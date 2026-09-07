// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package membw

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The perf file descriptors these counters need cannot be opened in a unit
// test, so what is covered here is everything around them: reading the event
// definitions sysfs publishes, and refusing the ones that cannot be trusted to
// mean what the caller assumes.

func TestReadConfigParsesAnEventDefinition(t *testing.T) {
	for definition, want := range map[string]uint64{
		// The layout the memory controller publishes: selector in the low
		// byte, unit mask in the next.
		"event=0xff,umask=0x20": 0x20ff,
		"event=0xff,umask=0x21": 0x21ff,
		"event=0x01":            0x01,
		// Trailing newline is how sysfs actually returns these.
		"event=0xff,umask=0x20\n": 0x20ff,
		"event=0xff, umask=0x20":  0x20ff,
	} {
		path := writeDefinition(t, definition)

		got, err := readConfig(path)
		if err != nil {
			t.Errorf("readConfig(%q) error = %v", definition, err)
			continue
		}
		if got != want {
			t.Errorf("readConfig(%q) = %#x, want %#x", definition, got, want)
		}
	}
}

// TestReadConfigRejectsDefinitionsItCannotHonour matters more than it looks.
// Silently ignoring a field would open a counter measuring something other
// than what the field asked for, and the reading would look perfectly
// plausible while being wrong.
func TestReadConfigRejectsDefinitionsItCannotHonour(t *testing.T) {
	for _, definition := range []string{
		"event=0xff,umask=0x20,edge=1",
		"event=0xff,inv=1",
		"config1=0x4043200000000",
		"",
		"event=",
		"event=zz",
	} {
		if _, err := readConfig(writeDefinition(t, definition)); err == nil {
			t.Errorf("readConfig(%q) succeeded, want an error", definition)
		}
	}
}

func TestReadConfigReportsAMissingFile(t *testing.T) {
	if _, err := readConfig(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("readConfig() accepted a missing file, want an error")
	}
}

// TestFieldNameCarriesTheUnit guards the names that end up on the endpoint.
func TestFieldNameCarriesTheUnit(t *testing.T) {
	for canonical, want := range map[string]string{
		"read":  "read_mibps",
		"write": "write_mibps",
	} {
		if got := fieldName(canonical); got != want {
			t.Errorf("fieldName(%q) = %q, want %q", canonical, got, want)
		}
	}
}

// TestDiscoverEventsHandlesEachSpelling covers the naming differences between
// part generations. Which spelling a machine uses is a property of its memory
// controller, so it has to be discovered rather than assumed.
func TestDiscoverEventsHandlesEachSpelling(t *testing.T) {
	for name, events := range map[string][]string{
		"current client": {"data_read", "data_write", "data_total"},
		"older client":   {"data_reads", "data_writes"},
		"server":         {"cas_count_read", "cas_count_write"},
	} {
		t.Run(name, func(t *testing.T) {
			device := fakeController(t, events...)

			pair, found := discoverEvents(device)
			if !found {
				t.Fatalf("discoverEvents() found nothing in %v", events)
			}
			if !slices.Contains(events, pair.read) || !slices.Contains(events, pair.write) {
				t.Errorf("discoverEvents() = %+v, want a pair from %v", pair, events)
			}
		})
	}
}

// TestDiscoverEventsSkipsUnusableControllers matters because a machine exposes
// several controllers and only some carry counters: this host publishes
// uncore_imc_0 with an empty event directory next to the free-running ones
// that work. Treating the empty one as a failure would report no bandwidth at
// all.
func TestDiscoverEventsSkipsUnusableControllers(t *testing.T) {
	for name, events := range map[string][]string{
		"no events":  {},
		"read only":  {"data_read"},
		"write only": {"data_write"},
		"unrelated":  {"clockticks"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, found := discoverEvents(fakeController(t, events...)); found {
				t.Errorf("discoverEvents() accepted %v, want it skipped", events)
			}
		})
	}
}

// TestNewCollectorReportsNoUsableCounters checks that a machine with
// controllers but no counters says so, rather than reporting silence.
func TestNewCollectorReportsNoUsableCounters(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"uncore_imc_0", "uncore_imc_1"} {
		if err := os.MkdirAll(filepath.Join(root, name, "events"), 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", name, err)
		}
	}

	_, err := NewCollector(filepath.Join(root, "uncore_imc*"), "test-host")
	if err == nil {
		t.Fatal("NewCollector() succeeded with no usable counters, want an error")
	}
	if !strings.Contains(err.Error(), "usable") {
		t.Errorf("NewCollector() error = %v, want it to say no usable pair was found", err)
	}
}

// TestCheckUnitRejectsSomethingElse guards against reporting a counter under a
// field name that claims mebibytes when it measures something else. The number
// would look perfectly ordinary while being wrong.
func TestCheckUnitRejectsSomethingElse(t *testing.T) {
	device := fakeController(t, "data_read")
	writeFile(t, filepath.Join(device, "events", "data_read.unit"), "ns\n")

	if err := checkUnit(device, "data_read"); err == nil {
		t.Error("checkUnit() accepted a counter measured in nanoseconds, want an error")
	}
}

func TestCheckUnitAcceptsTheExpectedUnitAndAMissingOne(t *testing.T) {
	device := fakeController(t, "data_read", "data_write")

	// Older parts omit the unit file; the scale still says how to convert.
	if err := checkUnit(device, "data_read"); err != nil {
		t.Errorf("checkUnit() rejected a counter with no published unit: %v", err)
	}

	writeFile(t, filepath.Join(device, "events", "data_write.unit"), "MiB\n")
	if err := checkUnit(device, "data_write"); err != nil {
		t.Errorf("checkUnit() rejected the expected unit: %v", err)
	}
}

func TestNewCollectorReportsAnAbsentController(t *testing.T) {
	_, err := NewCollector(filepath.Join(t.TempDir(), "uncore_imc*"), "test-host")
	if err == nil {
		t.Fatal("NewCollector() succeeded with no controllers, want an error")
	}
}

func TestReadFloatAndUint(t *testing.T) {
	dir := t.TempDir()

	scale := filepath.Join(dir, "scale")
	if err := os.WriteFile(scale, []byte("6.103515625e-5\n"), 0o644); err != nil {
		t.Fatalf("cannot write scale: %v", err)
	}
	// One count per 64 bytes, expressed in MiB, is the value current parts
	// publish. Getting this wrong scales every reading.
	if got, err := readFloat(scale); err != nil || got != 6.103515625e-5 {
		t.Errorf("readFloat() = %v, %v, want the published scale", got, err)
	}

	pmuType := filepath.Join(dir, "type")
	if err := os.WriteFile(pmuType, []byte("32\n"), 0o644); err != nil {
		t.Fatalf("cannot write type: %v", err)
	}
	if got, err := readUint(pmuType); err != nil || got != 32 {
		t.Errorf("readUint() = %v, %v, want 32", got, err)
	}
}

func writeDefinition(t *testing.T, definition string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "event")
	writeFile(t, path, definition)

	return path
}

// fakeController builds a controller directory publishing the named events.
func fakeController(t *testing.T, events ...string) string {
	t.Helper()

	device := t.TempDir()
	if err := os.MkdirAll(filepath.Join(device, "events"), 0o755); err != nil {
		t.Fatalf("cannot create event directory: %v", err)
	}
	writeFile(t, filepath.Join(device, "type"), "32\n")

	for _, event := range events {
		writeFile(t, filepath.Join(device, "events", event), "event=0xff,umask=0x20\n")
		writeFile(t, filepath.Join(device, "events", event+".scale"), "6.103515625e-5\n")
	}

	return device
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}
