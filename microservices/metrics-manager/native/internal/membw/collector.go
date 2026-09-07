// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package membw reports system memory bandwidth from the integrated memory
// controller.
//
// This is not GPU bandwidth, and it is important not to read it as such.
//
// Neither Intel GPU driver publishes a per-device byte counter. On i915 this
// was checked on the machine: the PMU carries engine occupancy, frequencies
// and rc6 residency; DRM fdinfo carries engine time and resident memory sizes,
// which are sizes rather than transfers; the card's sysfs attributes are
// frequencies; and no OA metric sets are registered. On xe it was checked in
// the driver, which defines exactly five events -- gt-c6-residency,
// engine-active-ticks, engine-total-ticks, gt-actual-frequency and
// gt-requested-frequency -- and no byte counter among them.
//
// What the memory controller sees is every access from the whole package,
// attributable to nothing in particular. On a machine whose GPU has no
// dedicated memory that is still the closest available signal, because the GPU
// is competing for exactly this bandwidth. It is reported under its own name
// rather than folded into the GPU panel, so nobody mistakes it for a
// per-device figure. On a discrete card, whose traffic goes to its own memory,
// it says correspondingly less.
//
// Telegraf's intel_pmu input can read arbitrary PMU events, but only from
// per-CPU-model event definition files published separately by Intel. The
// free-running controller counters are described by sysfs itself, which is why
// this reads them directly and works on any part that exposes them.
package membw

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/open-edge-platform/edge-ai-libraries/microservices/metrics-manager/native/internal/lineproto"
)

// Measurement carries memory controller throughput.
const Measurement = "memory_bandwidth"

// pmuGlob matches every memory controller PMU.
//
// The pattern is deliberately wider than the counters actually used. A machine
// exposes several of these and only some carry events: this one has
// uncore_imc_free_running_0 and _1 with counters, alongside uncore_imc_0 and _1
// whose event directories are empty. Matching broadly and then looking for
// usable events is what keeps this working across parts, where the naming
// differs.
const pmuGlob = "/sys/bus/event_source/devices/uncore_imc*"

// eventPair is one controller's read and write counters, under whatever names
// that generation uses.
type eventPair struct {
	read  string
	write string
}

// eventPairs lists the spellings seen across Intel parts, most recent first.
//
// Which one a machine uses is a property of its generation, not of its GPU, so
// this is discovered rather than configured.
var eventPairs = []eventPair{
	// Free-running counters on current client parts.
	{read: "data_read", write: "data_write"},
	// Older client integrated memory controllers.
	{read: "data_reads", write: "data_writes"},
	// Server parts count column address strobes instead.
	{read: "cas_count_read", write: "cas_count_write"},
}

// expectedUnit is the unit every known counter publishes. It is checked rather
// than assumed: a counter measured in something else would be reported under a
// field name claiming mebibytes per second, and the number would look
// perfectly ordinary while being wrong.
const expectedUnit = "MiB"

// counter is one opened perf event.
type counter struct {
	// canonical is "read" or "write". It is kept apart from the sysfs event
	// name so the field names on the endpoint stay the same whichever
	// spelling the machine uses.
	canonical string
	event     string

	// scale converts a raw count into MiB, as published by sysfs alongside
	// the event. It is 1/16384 on current parts, one count per 64 bytes,
	// but reading it is what keeps this correct on a part that differs.
	scale float64
	fd    int

	previous uint64
	primed   bool
}

// Collector reads the memory controllers.
type Collector struct {
	hostname string
	counters []counter
	sampled  time.Time

	now func() time.Time
}

// eventNames are the canonical counters reported. "data_total", where a
// machine publishes it, is deliberately not read: it is the sum of the other
// two, and deriving it costs nothing while opening it costs another file
// descriptor.
var eventNames = []string{"read", "write"}

// NewCollector opens the counters of every memory controller that publishes a
// usable pair, and takes a first reading so that the very first Collect
// already reports a real interval rather than nothing.
//
// A controller without events is skipped rather than treated as a failure:
// machines routinely expose several, only some of which carry counters. It is
// finding none at all that is an error, because a collector silently reporting
// nothing is worse than one that says why. The usual reason is a missing
// CAP_PERFMON, and that is worth an error message.
func NewCollector(sysfsGlob, hostname string) (*Collector, error) {
	devices, err := filepath.Glob(sysfsGlob)
	if err != nil {
		return nil, fmt.Errorf("bad device pattern %q: %w", sysfsGlob, err)
	}
	sort.Strings(devices)

	collector := &Collector{hostname: hostname, now: time.Now}

	for _, device := range devices {
		pair, found := discoverEvents(device)
		if !found {
			continue
		}

		pmuType, err := readUint(filepath.Join(device, "type"))
		if err != nil {
			collector.Close()
			return nil, err
		}

		for canonical, event := range map[string]string{"read": pair.read, "write": pair.write} {
			opened, err := openCounter(device, canonical, event, uint32(pmuType))
			if err != nil {
				collector.Close()
				return nil, err
			}

			collector.counters = append(collector.counters, opened)
		}
	}

	if len(collector.counters) == 0 {
		return nil, fmt.Errorf(
			"no memory controller at %s publishes a usable read/write counter pair", sysfsGlob)
	}

	for i := range collector.counters {
		if _, err := collector.counters[i].prime(); err != nil {
			collector.Close()
			return nil, err
		}
	}

	collector.sampled = collector.now()

	return collector, nil
}

// discoverEvents returns the first known event pair the controller publishes.
func discoverEvents(device string) (eventPair, bool) {
	for _, pair := range eventPairs {
		if exists(device, pair.read) && exists(device, pair.write) {
			return pair, true
		}
	}

	return eventPair{}, false
}

// exists reports whether the controller publishes an event by that name.
func exists(device, event string) bool {
	_, err := os.Stat(filepath.Join(device, "events", event))

	return err == nil
}

// openCounter opens one event of one controller.
func openCounter(device, canonical, event string, pmuType uint32) (counter, error) {
	config, err := readConfig(filepath.Join(device, "events", event))
	if err != nil {
		return counter{}, err
	}

	if err := checkUnit(device, event); err != nil {
		return counter{}, err
	}

	scale, err := readFloat(filepath.Join(device, "events", event+".scale"))
	if err != nil {
		return counter{}, err
	}

	attr := unix.PerfEventAttr{
		Type:   pmuType,
		Size:   uint32(unsafeSizeofPerfEventAttr),
		Config: config,
	}

	// An uncore event belongs to a package rather than to a task, so it is
	// opened for every process (pid -1) on one CPU of that package. CPU 0
	// is in the only package on the parts this runs on; a multi-socket
	// machine would need one open per socket.
	fd, err := unix.PerfEventOpen(&attr, -1, 0, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return counter{}, fmt.Errorf(
			"cannot open %s on %s: %w (reading uncore counters needs CAP_PERFMON)",
			event, filepath.Base(device), err)
	}

	return counter{
		canonical: canonical,
		event:     event,
		scale:     scale,
		fd:        fd,
	}, nil
}

// checkUnit refuses a counter that does not measure what the field names
// promise.
//
// Where sysfs publishes no unit at all the counter is accepted: older parts
// omit the file, and the scale still says how to convert it.
func checkUnit(device, event string) error {
	raw, err := os.ReadFile(filepath.Join(device, "events", event+".unit"))
	if err != nil {
		return nil
	}

	if unit := strings.TrimSpace(string(raw)); unit != expectedUnit {
		return fmt.Errorf("%s on %s is measured in %q, want %q",
			event, filepath.Base(device), unit, expectedUnit)
	}

	return nil
}

// unsafeSizeofPerfEventAttr is the size the kernel expects in attr.Size. The
// constant provided by the unix package is the authority on it.
const unsafeSizeofPerfEventAttr = unix.PERF_ATTR_SIZE_VER1

// Collect returns read and write throughput since the previous call.
func (c *Collector) Collect() ([]lineproto.Point, error) {
	now := c.now()
	elapsed := now.Sub(c.sampled).Seconds()
	c.sampled = now

	// Controllers are summed: they serve one address space, and reporting
	// them apart would invite adding up numbers that are already a total.
	totals := make(map[string]float64, len(eventNames))
	primed := make(map[string]bool, len(eventNames))

	for i := range c.counters {
		counter := &c.counters[i]

		raw, err := counter.read()
		if err != nil {
			return nil, err
		}

		if !counter.primed || elapsed <= 0 {
			counter.previous, counter.primed = raw, true
			continue
		}
		if raw < counter.previous {
			// A free-running counter wraps rather than resetting. One
			// interval of data is not worth reporting a negative rate
			// for.
			counter.previous = raw
			continue
		}

		totals[counter.canonical] += float64(raw-counter.previous) * counter.scale / elapsed
		primed[counter.canonical] = true
		counter.previous = raw
	}

	if len(primed) == 0 {
		// The first call has nothing to compare against.
		return nil, nil
	}

	fields := make([]lineproto.Field, 0, len(eventNames)+1)
	total := 0.0
	for _, event := range eventNames {
		if !primed[event] {
			continue
		}
		fields = append(fields, lineproto.FixedFloatField(fieldName(event), totals[event], 2))
		total += totals[event]
	}
	fields = append(fields, lineproto.FixedFloatField("total_mibps", total, 2))

	return []lineproto.Point{{
		Measurement: Measurement,
		Tags:        []lineproto.Tag{{Key: "host", Value: c.hostname}},
		Fields:      fields,
		Timestamp:   now,
	}}, nil
}

// fieldName turns a canonical counter name into a field name carrying its
// unit. It is built from the canonical name rather than the sysfs one, so the
// endpoint looks the same whichever spelling the machine uses.
func fieldName(canonical string) string {
	return canonical + "_mibps"
}

// prime takes the first reading, which later deltas are measured against.
func (c *counter) prime() (uint64, error) {
	raw, err := c.read()
	if err != nil {
		return 0, err
	}

	c.previous, c.primed = raw, true

	return raw, nil
}

// read returns the current value of the counter.
func (c *counter) read() (uint64, error) {
	var buf [8]byte

	if _, err := unix.Read(c.fd, buf[:]); err != nil {
		return 0, fmt.Errorf("reading %s: %w", c.event, err)
	}

	return uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
		uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56, nil
}

// Close releases the perf file descriptors.
func (c *Collector) Close() error {
	for _, counter := range c.counters {
		if counter.fd > 0 {
			_ = unix.Close(counter.fd)
		}
	}
	c.counters = nil

	return nil
}

// readConfig parses an event definition such as "event=0xff,umask=0x20" into
// the config word perf_event_open expects.
//
// The layout is the PMU's own: the event selector occupies the low byte and
// the unit mask the next one. Both are what sysfs publishes, so nothing here
// is hard-coded per model.
func readConfig(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading event definition: %w", err)
	}

	var config uint64
	for _, part := range strings.Split(strings.TrimSpace(string(raw)), ",") {
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}

		number, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(value), "0x"), 16, 64)
		if err != nil {
			return 0, fmt.Errorf("event definition %q: %w", part, err)
		}

		switch strings.TrimSpace(key) {
		case "event":
			config |= number
		case "umask":
			config |= number << 8
		default:
			return 0, fmt.Errorf("event definition %q: unsupported field %q", raw, key)
		}
	}

	if config == 0 {
		return 0, fmt.Errorf("event definition %q names no event", raw)
	}

	return config, nil
}

func readUint(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}

	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
}

func readFloat(path string) (float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}

	return strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
}
