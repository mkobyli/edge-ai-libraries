// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"
	"testing"
)

func TestLoadChartsConfigDefaultsWhenAbsent(t *testing.T) {
	config, err := LoadChartsConfig(t.TempDir() + "/missing.json")
	if err != nil {
		t.Fatal(err)
	}
	if config.HistoryDuration != "5m" || config.MaxPoints != 600 || len(config.Charts) != 6 {
		t.Errorf("unexpected chart defaults: %+v", config)
	}
}

func TestLoadChartsConfigOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"historyDuration":"10m",
		"maxPoints":300,
		"chartHeight":8,
		"charts":[{
			"metric":"memory.bandwidthMiBps",
			"title":"Memory bandwidth",
			"unit":"MiB/s"
		}]
	}`)
	config, err := LoadChartsConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.HistoryDuration != "10m" || config.ChartHeight != 8 {
		t.Errorf("config = %+v, want overridden duration and height", config)
	}
}

func TestLoadChartsConfigRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"bad duration", `{"historyDuration":"later"}`, "historyDuration"},
		{"excessive points", `{"maxPoints":3601}`, "maxPoints"},
		{"short chart", `{"chartHeight":2}`, "chartHeight"},
		{"no charts", `{"charts":[]}`, "charts must contain"},
		{
			"unknown metric",
			`{"charts":[{"metric":"disk.io","title":"Disk","unit":"B/s"}]}`,
			"not supported",
		},
		{
			"duplicate metric",
			`{"charts":[
				{"metric":"cpu.totalPercent","title":"One","unit":"%"},
				{"metric":"cpu.totalPercent","title":"Two","unit":"%"}
			]}`,
			"duplicated",
		},
		{
			"reversed range",
			`{"charts":[{"metric":"cpu.totalPercent","title":"CPU","unit":"%","min":100,"max":0}]}`,
			"min < max",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadChartsConfig(writeConfig(t, tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
