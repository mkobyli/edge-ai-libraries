// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
)

func (m *Model) toggleProcessCPUMode() {
	if m.config.Processes.CPUMode == "total" {
		m.config.Processes.CPUMode = "per-core"
	} else {
		m.config.Processes.CPUMode = "total"
	}
}

func usableCPUCount(count Reading) bool {
	return finiteReading(count) && count.Value >= 1 && math.Trunc(count.Value) == count.Value
}

func (m Model) processCPULabel() string {
	if m.config.Processes.CPUMode != "total" {
		return "CPU: per-core | c toggle (100% = 1 CPU)"
	}
	count := m.dash.Platform.LogicalCPUs
	if !usableCPUCount(count) {
		return "CPU: total | c toggle\nCPU total unavailable: logical CPU count missing or invalid."
	}
	return fmt.Sprintf("CPU: total | c toggle (100%% = %.0f CPUs)\nColors/alerts: per-core (h help)", count.Value)
}

func (m Model) processCPUPercent(value Reading) string {
	if m.config.Processes.CPUMode != "total" {
		return m.thresholdPercent("process.cpuPercent", value)
	}
	count := m.dash.Platform.LogicalCPUs
	if !usableCPUCount(count) {
		return unavailable
	}
	// Only the displayed number changes scale; colors and alerts share the
	// original per-core thresholds, even when a busy thread is a small host share.
	return m.thresholdValue("process.cpuPercent", value, func(r Reading) string {
		r.Value /= count.Value
		return formatPercent(r)
	})
}
