// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type Severity int

const (
	SeverityOK Severity = iota
	SeverityCareful
	SeverityWarning
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityCareful:
		return "CAREFUL"
	case SeverityWarning:
		return "WARNING"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "OK"
	}
}

type Alert struct {
	Key       string
	Metric    string
	Device    string
	Severity  Severity
	Value     float64
	Threshold float64
	Since     time.Time
	Message   string
}

type observation struct {
	key, metric, device, message string
	value                        float64
}

type alertState struct {
	active       Severity
	pending      Severity
	pendingCount int
	missingCount int
	alert        Alert
}

type AlertTracker struct {
	config       AlertConfig
	processLimit int
	states       map[string]*alertState
}

func NewAlertTracker(config AlertConfig, processLimit int) AlertTracker {
	return AlertTracker{
		config:       config,
		processLimit: processLimit,
		states:       make(map[string]*alertState),
	}
}

func (t *AlertTracker) Update(dashboard Dashboard, thresholds map[string]Threshold, at time.Time) {
	seen := make(map[string]bool)
	for _, observation := range dashboardObservations(dashboard, t.processLimit) {
		threshold, enabled := thresholds[observation.metric]
		if !enabled || math.IsNaN(observation.value) || math.IsInf(observation.value, 0) {
			continue
		}

		seen[observation.key] = true
		state := t.states[observation.key]
		if state == nil {
			state = &alertState{}
			t.states[observation.key] = state
		}
		state.missingCount = 0

		target := classifyWithHysteresis(observation.value, threshold, state.active,
			t.config.HysteresisPercent)
		if target == state.active {
			state.pending = target
			state.pendingCount = 0
		} else {
			sameDirection := state.pendingCount > 0 &&
				(target > state.active) == (state.pending > state.active)
			if !sameDirection {
				state.pending = target
				state.pendingCount = 1
			} else {
				state.pendingCount++
				// Every sample in an upward streak supports at least its
				// lowest severity; every downward sample supports at most
				// its highest. This keeps noisy adjacent bands from
				// resetting a continuously unhealthy or recovering run.
				if target > state.active && target < state.pending {
					state.pending = target
				}
				if target < state.active && target > state.pending {
					state.pending = target
				}
			}
		}

		required := t.config.SamplesToRaise
		if state.pending < state.active {
			required = t.config.SamplesToClear
		}
		if state.pending != state.active && state.pendingCount >= required {
			state.active = state.pending
			state.pendingCount = 0
			if state.active == SeverityOK {
				state.alert = Alert{}
			} else {
				state.alert.Since = at
			}
		}
		if state.active != SeverityOK {
			state.alert.Key = observation.key
			state.alert.Metric = observation.metric
			state.alert.Device = observation.device
			state.alert.Severity = state.active
			state.alert.Value = observation.value
			state.alert.Threshold = thresholdFor(threshold, state.active)
			state.alert.Message = observation.message
		}
	}

	for key, state := range t.states {
		if seen[key] {
			continue
		}
		state.missingCount++
		if state.missingCount >= t.config.SamplesToClear {
			delete(t.states, key)
		}
	}
}

func (t *AlertTracker) Active() []Alert {
	alerts := make([]Alert, 0, len(t.states))
	for _, state := range t.states {
		if state.active != SeverityOK {
			alerts = append(alerts, state.alert)
		}
	}
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Severity != alerts[j].Severity {
			return alerts[i].Severity > alerts[j].Severity
		}
		return alerts[i].Key < alerts[j].Key
	})

	return alerts
}

func classify(value float64, threshold Threshold) Severity {
	switch {
	case value >= threshold.Critical:
		return SeverityCritical
	case value >= threshold.Warning:
		return SeverityWarning
	case value >= threshold.Careful:
		return SeverityCareful
	default:
		return SeverityOK
	}
}

func classifyWithHysteresis(value float64, threshold Threshold, active Severity, hysteresis float64) Severity {
	if active != SeverityOK {
		exit := thresholdFor(threshold, active) * (1 - hysteresis/100)
		if value >= exit {
			normal := classify(value, threshold)
			if normal < active {
				return active
			}
			return normal
		}
	}

	return classify(value, threshold)
}

func thresholdFor(threshold Threshold, severity Severity) float64 {
	switch severity {
	case SeverityCritical:
		return threshold.Critical
	case SeverityWarning:
		return threshold.Warning
	default:
		return threshold.Careful
	}
}

func dashboardObservations(d Dashboard, processLimit int) []observation {
	var observations []observation
	add := func(key, metric, device, message string, reading Reading) {
		if reading.OK {
			observations = append(observations, observation{
				key: key, metric: metric, device: device, message: message, value: reading.Value,
			})
		}
	}

	if total := totalCPUUsage(d.CPU); total.OK {
		add("cpu.total", "cpu.totalPercent", "", "Sustained CPU utilization is high.",
			total)
	}
	add("cpu.temperature", "cpu.temperatureC", "", "Check CPU load and cooling.",
		d.CPU.PackageTempC)
	add("memory.used", "memory.usedPercent", "", "Inspect memory-intensive processes.",
		d.Memory.UsedPercent)

	for _, gpu := range d.GPUs {
		for _, engine := range gpu.Engines {
			add(fmt.Sprintf("gpu.%s.engine.%s", gpu.ID, engine.Name), "gpu.utilizationPercent",
				"GPU "+gpu.ID, "GPU engine utilization is sustained.", engine.Usage)
		}
		add("gpu."+gpu.ID+".temperature", "gpu.temperatureC", "GPU "+gpu.ID,
			"Check GPU load and cooling.", gpu.TempC)
	}
	add("npu.utilization", "npu.utilizationPercent", "NPU",
		"NPU utilization is sustained.", d.NPU.Utilization)
	add("npu.temperature", "npu.temperatureC", "NPU",
		"Check NPU load and cooling.", d.NPU.TempC)

	processes := d.Processes
	if len(processes) > processLimit {
		processes = processes[:processLimit]
	}
	for _, process := range processes {
		add("process."+process.PID, "process.cpuPercent", process.Command,
			"Process CPU utilization is sustained.", process.CPUPercent)
	}

	return observations
}

func totalCPUUsage(cpu CPU) Reading {
	if !cpu.UsageIdle.OK || math.IsNaN(cpu.UsageIdle.Value) || math.IsInf(cpu.UsageIdle.Value, 0) {
		return Reading{}
	}

	return reading(math.Max(0, math.Min(100, 100-cpu.UsageIdle.Value)))
}
