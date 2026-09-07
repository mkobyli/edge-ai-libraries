// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"testing"
	"time"
)

func TestAlertTrackerRaisesAfterConsecutiveSamples(t *testing.T) {
	config := DefaultDashboardConfig()
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	dashboard := Dashboard{Memory: Memory{UsedPercent: reading(75)}}

	for sample := 1; sample <= 2; sample++ {
		tracker.Update(dashboard, config.Thresholds, fixedNow.Add(time.Duration(sample)*time.Second))
		if alerts := tracker.Active(); len(alerts) != 0 {
			t.Fatalf("sample %d raised alerts: %+v", sample, alerts)
		}
	}
	tracker.Update(dashboard, config.Thresholds, fixedNow.Add(3*time.Second))

	alerts := tracker.Active()
	if len(alerts) != 1 || alerts[0].Severity != SeverityWarning {
		t.Fatalf("alerts = %+v, want one warning", alerts)
	}
}

func TestAlertTrackerUsesHysteresisAndConsecutiveClearSamples(t *testing.T) {
	config := DefaultDashboardConfig()
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	updateMemory := func(percent float64) {
		tracker.Update(
			Dashboard{Memory: Memory{UsedPercent: reading(percent)}},
			config.Thresholds,
			fixedNow,
		)
	}

	for range 3 {
		updateMemory(55)
	}
	if len(tracker.Active()) != 1 {
		t.Fatal("careful alert was not raised")
	}

	// Five percent hysteresis puts the careful exit at 47.5%.
	for range 3 {
		updateMemory(48)
	}
	if len(tracker.Active()) != 1 {
		t.Fatal("alert cleared inside the hysteresis band")
	}
	for range 2 {
		updateMemory(47)
	}
	if len(tracker.Active()) != 1 {
		t.Fatal("alert cleared before three consecutive samples")
	}
	updateMemory(47)
	if len(tracker.Active()) != 0 {
		t.Fatal("alert did not clear after three samples below the exit threshold")
	}
}

func TestAlertTrackerDropsMissingMeasurements(t *testing.T) {
	config := DefaultDashboardConfig()
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	for range 3 {
		tracker.Update(
			Dashboard{CPU: CPU{PackageTempC: reading(100)}},
			config.Thresholds,
			fixedNow,
		)
	}
	for range 2 {
		tracker.Update(Dashboard{}, config.Thresholds, fixedNow)
	}
	if len(tracker.Active()) != 1 {
		t.Fatal("alert disappeared after a transient missing sample")
	}
	tracker.Update(Dashboard{}, config.Thresholds, fixedNow)
	if len(tracker.Active()) != 0 {
		t.Fatal("alert remained after the measurement disappeared")
	}
}

func TestAlertTrackerSortsBySeverity(t *testing.T) {
	config := DefaultDashboardConfig()
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	dashboard := Dashboard{
		CPU:    CPU{PackageTempC: reading(100)},
		Memory: Memory{UsedPercent: reading(55)},
	}
	for range 3 {
		tracker.Update(dashboard, config.Thresholds, fixedNow)
	}

	alerts := tracker.Active()
	if len(alerts) != 2 {
		t.Fatalf("alerts = %+v, want two", alerts)
	}
	if alerts[0].Severity != SeverityCritical || alerts[1].Severity != SeverityCareful {
		t.Errorf("alerts are not ordered by severity: %+v", alerts)
	}
}

func TestTotalCPUUsageRejectsInvalidAndClampsRange(t *testing.T) {
	tests := []struct {
		name string
		idle Reading
		want Reading
	}{
		{"missing", Reading{}, Reading{}},
		{"negative idle", reading(-10), reading(100)},
		{"idle over one hundred", reading(110), reading(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := totalCPUUsage(CPU{UsageIdle: tt.idle}); got != tt.want {
				t.Errorf("totalCPUUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAlertTrackerCountsSeverityChangesInSameDirection(t *testing.T) {
	config := DefaultDashboardConfig()
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	for _, percent := range []float64{80, 90, 80} {
		tracker.Update(
			Dashboard{Memory: Memory{UsedPercent: reading(percent)}},
			config.Thresholds,
			fixedNow,
		)
	}

	alerts := tracker.Active()
	if len(alerts) != 1 || alerts[0].Severity != SeverityWarning {
		t.Fatalf("alerts = %+v, want one warning after a warning/critical/warning run", alerts)
	}
}

func TestAlertTrackerDowngradesAcrossVaryingLowerSeverities(t *testing.T) {
	config := DefaultDashboardConfig()
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	update := func(percent float64) {
		tracker.Update(
			Dashboard{Memory: Memory{UsedPercent: reading(percent)}},
			config.Thresholds,
			fixedNow,
		)
	}
	for range 3 {
		update(95)
	}
	for _, percent := range []float64{60, 40, 60} {
		update(percent)
	}

	alerts := tracker.Active()
	if len(alerts) != 1 || alerts[0].Severity != SeverityCareful {
		t.Fatalf("alerts = %+v, want a downgrade to careful", alerts)
	}
}

func TestAlertTrackerOnlyChecksDisplayedProcesses(t *testing.T) {
	config := DefaultDashboardConfig()
	config.Processes.MaxDisplayed = 1
	tracker := NewAlertTracker(config.Alerts, config.Processes.MaxDisplayed)
	dashboard := Dashboard{Processes: []Process{
		{PID: "1", Command: "visible", CPUPercent: reading(1)},
		{PID: "2", Command: "hidden", CPUPercent: reading(99)},
	}}
	for range 3 {
		tracker.Update(dashboard, config.Thresholds, fixedNow)
	}
	if alerts := tracker.Active(); len(alerts) != 0 {
		t.Fatalf("hidden process raised an alert: %+v", alerts)
	}
}
