// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	DefaultConfigPath = "/etc/metrics-manager/tui-dashboard.json"
	maxConfigSize     = 1 << 20
)

type DashboardConfig struct {
	SPDXFileCopyrightText string               `json:"SPDX-FileCopyrightText,omitempty"`
	SPDXLicenseIdentifier string               `json:"SPDX-License-Identifier,omitempty"`
	Processes             ProcessConfig        `json:"processes"`
	Alerts                AlertConfig          `json:"alerts"`
	Thresholds            map[string]Threshold `json:"thresholds"`
}

type ProcessConfig struct {
	MaxDisplayed int `json:"maxDisplayed"`
}

type AlertConfig struct {
	SamplesToRaise    int     `json:"samplesToRaise"`
	SamplesToClear    int     `json:"samplesToClear"`
	HysteresisPercent float64 `json:"hysteresisPercent"`
}

type Threshold struct {
	Careful  float64 `json:"careful"`
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
}

var supportedThresholds = map[string]bool{
	"cpu.totalPercent":       true,
	"cpu.temperatureC":       true,
	"memory.usedPercent":     true,
	"gpu.utilizationPercent": true,
	"gpu.temperatureC":       true,
	"npu.utilizationPercent": true,
	"npu.temperatureC":       true,
	"process.cpuPercent":     true,
}

func DefaultDashboardConfig() DashboardConfig {
	return DashboardConfig{
		Processes: ProcessConfig{MaxDisplayed: 10},
		Alerts: AlertConfig{
			SamplesToRaise:    3,
			SamplesToClear:    3,
			HysteresisPercent: 5,
		},
		Thresholds: map[string]Threshold{
			"cpu.totalPercent":       {Careful: 65, Warning: 75, Critical: 85},
			"cpu.temperatureC":       {Careful: 70, Warning: 85, Critical: 95},
			"memory.usedPercent":     {Careful: 50, Warning: 70, Critical: 90},
			"gpu.utilizationPercent": {Careful: 50, Warning: 70, Critical: 90},
			"gpu.temperatureC":       {Careful: 60, Warning: 70, Critical: 80},
			"npu.utilizationPercent": {Careful: 50, Warning: 70, Critical: 90},
			"npu.temperatureC":       {Careful: 70, Warning: 85, Critical: 95},
			"process.cpuPercent":     {Careful: 50, Warning: 70, Critical: 90},
		},
	}
}

// LoadDashboardConfig returns built-in defaults when the standard configuration
// file is absent. An existing but invalid file is always reported.
func LoadDashboardConfig(path string) (DashboardConfig, error) {
	config := DefaultDashboardConfig()
	config, err := decodeConfig(path, "dashboard", config)
	if err != nil {
		return DashboardConfig{}, err
	}
	if err := config.Validate(); err != nil {
		return DashboardConfig{}, fmt.Errorf("validate dashboard config %q: %w", path, err)
	}

	return config, nil
}

func decodeConfig[T any](path, kind string, config T) (T, error) {
	var zero T
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return zero, fmt.Errorf("open %s config %q: %w", kind, path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return zero, fmt.Errorf("stat %s config %q: %w", kind, path, err)
	}
	if info.Size() > maxConfigSize {
		return zero, fmt.Errorf("%s config %q exceeds %d bytes", kind, path, maxConfigSize)
	}

	decoder := json.NewDecoder(io.LimitReader(file, maxConfigSize))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return zero, fmt.Errorf("decode %s config %q: %w", kind, path, err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return zero, fmt.Errorf("decode %s config %q: %w", kind, path, err)
	}

	return config, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("configuration contains more than one JSON value")
		}
		return err
	}

	return nil
}

func (c DashboardConfig) Validate() error {
	if c.Processes.MaxDisplayed <= 0 || c.Processes.MaxDisplayed > 1000 {
		return fmt.Errorf("processes.maxDisplayed must be between 1 and 1000")
	}
	if c.Alerts.SamplesToRaise <= 0 || c.Alerts.SamplesToRaise > 1000 {
		return fmt.Errorf("alerts.samplesToRaise must be between 1 and 1000")
	}
	if c.Alerts.SamplesToClear <= 0 || c.Alerts.SamplesToClear > 1000 {
		return fmt.Errorf("alerts.samplesToClear must be between 1 and 1000")
	}
	if c.Alerts.HysteresisPercent < 0 || c.Alerts.HysteresisPercent >= 100 {
		return fmt.Errorf("alerts.hysteresisPercent must be at least 0 and less than 100")
	}
	for name, threshold := range c.Thresholds {
		if !supportedThresholds[name] {
			return fmt.Errorf("threshold %q is not supported", name)
		}
		if threshold.Careful < 0 ||
			threshold.Careful >= threshold.Warning ||
			threshold.Warning >= threshold.Critical {
			return fmt.Errorf("threshold %q must satisfy 0 <= careful < warning < critical", name)
		}
		if isPercentThreshold(name) && threshold.Critical > 100 {
			return fmt.Errorf("threshold %q critical value cannot exceed 100", name)
		}
	}

	return nil
}

func isPercentThreshold(name string) bool {
	return name != "cpu.temperatureC" &&
		name != "gpu.temperatureC" &&
		name != "npu.temperatureC"
}
