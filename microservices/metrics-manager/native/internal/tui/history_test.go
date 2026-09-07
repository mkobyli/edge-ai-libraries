// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"testing"
	"time"
)

func TestHistoryKeepsConfiguredWindowAndPointLimit(t *testing.T) {
	config := DefaultChartsConfig()
	config.HistoryDuration = "10s"
	config.MaxPoints = 10
	history := NewHistory(config)

	for sample := 0; sample < 12; sample++ {
		history.Update(
			Dashboard{Memory: Memory{UsedPercent: reading(float64(sample))}},
			fixedNow.Add(time.Duration(sample)*time.Second),
		)
	}
	series := history.Series("memory.usedPercent")
	if len(series) != 1 {
		t.Fatalf("series = %+v, want one", series)
	}
	if len(series[0].Points) != 10 {
		t.Fatalf("points = %d, want the configured maximum 10", len(series[0].Points))
	}
	if got := series[0].Points[0].Value; got != 2 {
		t.Errorf("oldest value = %.1f, want 2", got)
	}
}

func TestHistoryRemovesExpiredSeries(t *testing.T) {
	config := DefaultChartsConfig()
	config.HistoryDuration = "10s"
	history := NewHistory(config)
	history.Update(
		Dashboard{CPU: CPU{PackageTempC: reading(80)}},
		fixedNow,
	)
	history.Update(Dashboard{}, fixedNow.Add(11*time.Second))

	if series := history.Series("cpu.temperatureC"); len(series) != 0 {
		t.Errorf("expired series remain: %+v", series)
	}
}

func TestHistorySeparatesMultipleGPUs(t *testing.T) {
	history := NewHistory(DefaultChartsConfig())
	history.Update(Dashboard{GPUs: []GPU{
		{ID: "0", Engines: []GPUEngine{{Name: "render", Usage: reading(20)}}},
		{ID: "1", Engines: []GPUEngine{{Name: "render", Usage: reading(30)}}},
	}}, fixedNow)

	series := history.Series("gpu.utilizationPercent")
	if len(series) != 2 || series[0].Label != "GPU 0" || series[1].Label != "GPU 1" {
		t.Errorf("GPU series = %+v, want stable per-device series", series)
	}
}

func TestDownsamplePreservesPeaks(t *testing.T) {
	points := []HistoryPoint{
		{Value: 1}, {Value: 9}, {Value: 2}, {Value: 8},
	}
	got := downsample(points, 2)
	if len(got) != 2 || got[0].Value != 9 || got[1].Value != 8 {
		t.Errorf("downsample = %+v, want bucket peaks 9 and 8", got)
	}
}

func TestChartBoundsStayOrderedWithOneSidedRange(t *testing.T) {
	minimum := 50.0
	minValue, maxValue := chartBounds(
		ChartSpec{Min: &minimum},
		[]HistoryPoint{{Value: 10}, {Value: 20}},
	)
	if minValue != 50 || maxValue <= minValue {
		t.Errorf("bounds = %.1f..%.1f, want an ordered range starting at 50", minValue, maxValue)
	}

	maximum := 50.0
	minValue, maxValue = chartBounds(
		ChartSpec{Max: &maximum},
		[]HistoryPoint{{Value: 80}, {Value: 90}},
	)
	if maxValue != 50 || minValue >= maxValue {
		t.Errorf("bounds = %.1f..%.1f, want an ordered range ending at 50", minValue, maxValue)
	}
}
