// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDashboardConfigDefaultsWhenAbsent(t *testing.T) {
	config, err := LoadDashboardConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Processes.MaxDisplayed != 10 || config.Alerts.SamplesToRaise != 3 {
		t.Errorf("unexpected defaults: %+v", config)
	}
}

func TestLoadDashboardConfigOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"processes": {"maxDisplayed": 25},
		"alerts": {"samplesToRaise": 4, "samplesToClear": 2, "hysteresisPercent": 7},
		"thresholds": {
			"cpu.totalPercent": {"careful": 60, "warning": 75, "critical": 90}
		}
	}`)

	config, err := LoadDashboardConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Processes.MaxDisplayed != 25 {
		t.Errorf("maxDisplayed = %d, want 25", config.Processes.MaxDisplayed)
	}
	if config.Thresholds["memory.usedPercent"].Critical != 90 {
		t.Error("an omitted threshold did not keep its default")
	}
}

func TestLoadDashboardConfigRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"unknown field", `{"unknown": true}`, "unknown field"},
		{"multiple values", `{}` + "\n" + `{}`, "more than one"},
		{
			"unordered threshold",
			`{"thresholds":{"cpu.totalPercent":{"careful":80,"warning":70,"critical":90}}}`,
			"careful < warning",
		},
		{
			"unknown threshold",
			`{"thresholds":{"disk.usedPercent":{"careful":50,"warning":70,"critical":90}}}`,
			"not supported",
		},
		{
			"unbounded process count",
			`{"processes":{"maxDisplayed":1001}}`,
			"between 1 and 1000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadDashboardConfig(writeConfig(t, tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadDashboardConfigRejectsOversizedFile(t *testing.T) {
	_, err := LoadDashboardConfig(writeConfig(t, strings.Repeat(" ", maxConfigSize+1)))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want an oversized-file error", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
