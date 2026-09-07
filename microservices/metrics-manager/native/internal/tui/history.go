// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"sort"
	"time"
)

const maxHistorySeries = 128

type HistoryPoint struct {
	At    time.Time
	Value float64
}

type HistorySeries struct {
	Key    string
	Metric string
	Label  string
	Points []HistoryPoint
}

type History struct {
	window    time.Duration
	maxPoints int
	series    map[string]*HistorySeries
}

type chartObservation struct {
	key, metric, label string
	value              Reading
}

func NewHistory(config ChartsConfig) History {
	return History{
		window:    config.historyWindow(),
		maxPoints: config.MaxPoints,
		series:    make(map[string]*HistorySeries),
	}
}

func (h *History) Update(dashboard Dashboard, at time.Time) {
	for _, observation := range chartObservations(dashboard) {
		if !observation.value.OK ||
			math.IsNaN(observation.value.Value) ||
			math.IsInf(observation.value.Value, 0) {
			continue
		}

		series := h.series[observation.key]
		if series == nil {
			if len(h.series) >= maxHistorySeries {
				continue
			}
			series = &HistorySeries{
				Key: observation.key, Metric: observation.metric, Label: observation.label,
			}
			h.series[observation.key] = series
		}
		series.Points = append(series.Points, HistoryPoint{At: at, Value: observation.value.Value})
	}

	for key, series := range h.series {
		h.prune(series, at)
		if len(series.Points) == 0 {
			delete(h.series, key)
		}
	}
}

func (h *History) prune(series *HistorySeries, now time.Time) {
	cutoff := now.Add(-h.window)
	first := sort.Search(len(series.Points), func(i int) bool {
		return !series.Points[i].At.Before(cutoff)
	})
	if first > 0 {
		series.Points = append([]HistoryPoint(nil), series.Points[first:]...)
	}
	if overflow := len(series.Points) - h.maxPoints; overflow > 0 {
		series.Points = append([]HistoryPoint(nil), series.Points[overflow:]...)
	}
}

func (h History) Series(metric string) []HistorySeries {
	var series []HistorySeries
	for _, candidate := range h.series {
		if candidate.Metric == metric {
			series = append(series, *candidate)
		}
	}
	sort.Slice(series, func(i, j int) bool {
		return series[i].Key < series[j].Key
	})

	return series
}

func chartObservations(d Dashboard) []chartObservation {
	observations := []chartObservation{
		{key: "cpu.total", metric: "cpu.totalPercent", label: "CPU", value: totalCPUUsage(d.CPU)},
		{key: "cpu.temperature", metric: "cpu.temperatureC", label: "CPU", value: d.CPU.PackageTempC},
		{key: "memory.used", metric: "memory.usedPercent", label: "Memory", value: d.Memory.UsedPercent},
		{
			key: "memory.bandwidth", metric: "memory.bandwidthMiBps",
			label: "Memory", value: d.Memory.TotalMiBps,
		},
	}

	for _, gpu := range d.GPUs {
		observations = append(observations,
			chartObservation{
				key: "gpu." + gpu.ID + ".utilization", metric: "gpu.utilizationPercent",
				label: "GPU " + gpu.ID, value: maxEngineUsage(gpu.Engines),
			},
			chartObservation{
				key: "gpu." + gpu.ID + ".temperature", metric: "gpu.temperatureC",
				label: "GPU " + gpu.ID, value: gpu.TempC,
			},
		)
	}
	observations = append(observations,
		chartObservation{
			key: "npu.utilization", metric: "npu.utilizationPercent",
			label: "NPU", value: d.NPU.Utilization,
		},
		chartObservation{
			key: "npu.temperature", metric: "npu.temperatureC",
			label: "NPU", value: d.NPU.TempC,
		},
	)

	return observations
}

func maxEngineUsage(engines []GPUEngine) Reading {
	maximum := Reading{}
	for _, engine := range engines {
		if !engine.Usage.OK {
			continue
		}
		if !maximum.OK || engine.Usage.Value > maximum.Value {
			maximum = engine.Usage
		}
	}

	return maximum
}

func seriesTitle(spec ChartSpec, series HistorySeries) string {
	if series.Label == "" || series.Label == spec.Title {
		return spec.Title
	}

	return fmt.Sprintf("%s · %s", spec.Title, series.Label)
}
