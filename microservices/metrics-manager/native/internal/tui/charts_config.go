// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"time"
)

const DefaultChartsConfigPath = "/etc/metrics-manager/tui-charts.json"

type ChartsConfig struct {
	SPDXFileCopyrightText string      `json:"SPDX-FileCopyrightText,omitempty"`
	SPDXLicenseIdentifier string      `json:"SPDX-License-Identifier,omitempty"`
	HistoryDuration       string      `json:"historyDuration"`
	MaxPoints             int         `json:"maxPoints"`
	ChartHeight           int         `json:"chartHeight"`
	Charts                []ChartSpec `json:"charts"`
}

type ChartSpec struct {
	Metric string   `json:"metric"`
	Title  string   `json:"title"`
	Unit   string   `json:"unit"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
}

var supportedCharts = map[string]bool{
	"cpu.totalPercent":       true,
	"cpu.temperatureC":       true,
	"memory.usedPercent":     true,
	"memory.bandwidthMiBps":  true,
	"gpu.utilizationPercent": true,
	"gpu.temperatureC":       true,
	"npu.utilizationPercent": true,
	"npu.temperatureC":       true,
}

func DefaultChartsConfig() ChartsConfig {
	return ChartsConfig{
		HistoryDuration: "5m",
		MaxPoints:       600,
		ChartHeight:     6,
		Charts: []ChartSpec{
			fixedChart("cpu.totalPercent", "CPU utilization", "%", 0, 100),
			fixedChart("memory.usedPercent", "Memory used", "%", 0, 100),
			fixedChart("cpu.temperatureC", "CPU temperature", "°C", 0, 110),
			fixedChart("gpu.utilizationPercent", "GPU utilization", "%", 0, 100),
			fixedChart("gpu.temperatureC", "GPU temperature", "°C", 0, 110),
			fixedChart("npu.utilizationPercent", "NPU utilization", "%", 0, 100),
		},
	}
}

func fixedChart(metric, title, unit string, minValue, maxValue float64) ChartSpec {
	return ChartSpec{
		Metric: metric,
		Title:  title,
		Unit:   unit,
		Min:    &minValue,
		Max:    &maxValue,
	}
}

func LoadChartsConfig(path string) (ChartsConfig, error) {
	config, err := decodeConfig(path, "charts", DefaultChartsConfig())
	if err != nil {
		return ChartsConfig{}, err
	}
	if err := config.Validate(); err != nil {
		return ChartsConfig{}, fmt.Errorf("validate charts config %q: %w", path, err)
	}

	return config, nil
}

func (c ChartsConfig) Validate() error {
	duration, err := time.ParseDuration(c.HistoryDuration)
	if err != nil {
		return fmt.Errorf("historyDuration: %w", err)
	}
	if duration < 10*time.Second || duration > 24*time.Hour {
		return fmt.Errorf("historyDuration must be between 10s and 24h")
	}
	if c.MaxPoints < 10 || c.MaxPoints > 3600 {
		return fmt.Errorf("maxPoints must be between 10 and 3600")
	}
	if c.ChartHeight < 3 || c.ChartHeight > 20 {
		return fmt.Errorf("chartHeight must be between 3 and 20")
	}
	if len(c.Charts) == 0 || len(c.Charts) > 32 {
		return fmt.Errorf("charts must contain between 1 and 32 entries")
	}

	seen := make(map[string]bool, len(c.Charts))
	for i, chart := range c.Charts {
		if !supportedCharts[chart.Metric] {
			return fmt.Errorf("charts[%d].metric %q is not supported", i, chart.Metric)
		}
		if seen[chart.Metric] {
			return fmt.Errorf("charts[%d].metric %q is duplicated", i, chart.Metric)
		}
		seen[chart.Metric] = true
		if chart.Title == "" || len([]rune(chart.Title)) > 80 {
			return fmt.Errorf("charts[%d].title must contain 1 to 80 characters", i)
		}
		if len([]rune(chart.Unit)) > 16 {
			return fmt.Errorf("charts[%d].unit cannot exceed 16 characters", i)
		}
		if chart.Min != nil && (math.IsNaN(*chart.Min) || math.IsInf(*chart.Min, 0)) {
			return fmt.Errorf("charts[%d].min must be finite", i)
		}
		if chart.Max != nil && (math.IsNaN(*chart.Max) || math.IsInf(*chart.Max, 0)) {
			return fmt.Errorf("charts[%d].max must be finite", i)
		}
		if chart.Min != nil && chart.Max != nil && *chart.Min >= *chart.Max {
			return fmt.Errorf("charts[%d] must satisfy min < max", i)
		}
	}

	return nil
}

func (c ChartsConfig) historyWindow() time.Duration {
	duration, err := time.ParseDuration(c.HistoryDuration)
	if err != nil {
		return 5 * time.Minute
	}

	return duration
}
