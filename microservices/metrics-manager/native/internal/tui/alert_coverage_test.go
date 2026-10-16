// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestProcessCoverageIdentifiesReasonsAndUsesActualAlertCandidates(t *testing.T) {
	m := testModel(nil)
	m.updatedAt = fixedNow
	m.dash.Processes = []Process{
		{PID: "1", Command: "fresh", CPUPercent: reading(10), CPUAt: fixedNow},
		{PID: "2", Command: "missing", CPUPercent: reading(20)},
		{PID: "3", Command: "old", CPUPercent: reading(30), CPUAt: fixedNow.Add(-11 * time.Second)},
		{PID: "4", Command: "future", CPUPercent: reading(40), CPUAt: fixedNow.Add(time.Second)},
		{PID: "5", Command: "invalid", CPUPercent: reading(math.NaN()), CPUAt: fixedNow},
		{PID: "6", Command: "rewound", CPUPercent: reading(60), CPUAt: fixedNow.Add(-time.Second)},
		{PID: "7", Command: "conflict", CPUPercent: reading(70), CPUAt: fixedNow},
	}
	m.alerts.states["process.6"] = &alertState{observedAt: fixedNow, observedValue: 60}
	m.alerts.states["process.7"] = &alertState{observedAt: fixedNow, observedValue: 71}
	issues := m.processAlertIssues()
	wantReasons := []string{
		"measurement timestamp missing", "measurement is older than 10s", "measurement timestamp is in the future",
		"CPU measurement missing or non-finite", "out-of-order measurement timestamp", "conflicting CPU values at one timestamp",
	}
	if len(issues) != len(wantReasons) {
		t.Fatalf("issues = %+v", issues)
	}
	for i, reason := range wantReasons {
		if issues[i].reason != reason {
			t.Fatalf("issue %d = %+v, want %q", i, issues[i], reason)
		}
	}
	view := m.alertDetails(140)
	for _, want := range []string{
		"Process CPU alert coverage (6/7 limited)", "missing (PID 2): measurement timestamp missing",
		"source time", "checked", "export_timestamp=true",
		"this does not block process collection or display",
	} {
		wantContains(t, view, want)
	}
	m.config.Processes.SortBy = "pid"
	m.config.Processes.Descending = true
	for i, issue := range m.processAlertIssues() {
		if issue.reason != issues[i].reason || issue.process.PID != issues[i].process.PID {
			t.Fatal("display sorting changed the diagnostic population")
		}
	}
	m.alerts.processLimit = 1
	if m.sortedProcesses()[0].PID == "1" {
		t.Fatal("fixture must display different processes from those evaluated for alerts")
	}
	if len(m.processAlertIssues()) != 0 {
		t.Fatal("a process outside the alert population limited coverage")
	}
	m.alerts.processLimit = 2
	if got := m.processAlertIssues(); len(got) != 1 || got[0].process.PID != "2" {
		t.Fatalf("coverage did not respect the evaluation limit: %+v", got)
	}
	delete(m.config.Thresholds, "process.cpuPercent")
	if len(m.processAlertIssues()) != 0 {
		t.Fatal("unmonitored process CPU produced a coverage warning")
	}
}

func TestAlertShortcutsAndCoverageStayVisibleWithoutAddingFooterRows(t *testing.T) {
	m := testModel(nil)
	m.updatedAt = fixedNow
	m.dash.Processes = []Process{{PID: "42", Command: "worker", CPUPercent: reading(10)}}
	for _, width := range []int{40, 70, 80, 100, 140} {
		text := ansi.Strip(m.alertSummary(width))
		if !strings.Contains(text, "[4/a]") || !strings.Contains(text, "CPU") ||
			strings.Contains(text, "details") || lipgloss.Width(text) > width {
			t.Fatalf("width %d ambiguous or clipped status: %q", width, text)
		}
		if len(m.bottomAlertLines(width)) != 3 {
			t.Fatal("coverage expanded the fixed footer")
		}
		m.config.Alerts.ShowCareful = true
		if text := ansi.Strip(m.alertSummary(width)); lipgloss.Width(text) > width || !strings.Contains(text, "CPU") {
			t.Fatalf("width %d careful counts hid limited coverage: %q", width, text)
		}
		m.config.Alerts.ShowCareful = false
	}
	for _, err := range []error{nil, errors.New("test disconnect")} {
		m.err = err
		for _, at := range []time.Time{fixedNow, fixedNow.Add(-time.Minute), {}} {
			m.updatedAt = at
			text := m.alertSummary(140)
			if !strings.Contains(text, "[4/a] Open alerts") || strings.Contains(text, "a details") {
				t.Fatalf("connection state used ambiguous navigation: %q", text)
			}
		}
	}
	m.err, m.updatedAt = nil, fixedNow
	m.config.Alerts.BottomBar = false
	for _, width := range []int{70, 100, 140} {
		text := strings.Join(m.chromeLines(width), "\n")
		if !strings.Contains(text, "CPU") || !strings.Contains(text, "[4/a]") {
			t.Fatalf("width %d disabled bottom bar hid coverage or navigation: %q", width, text)
		}
	}
	m.dash.Processes[0].CPUAt = fixedNow
	if strings.Contains(m.alertSummary(140), "limited") || strings.Contains(m.alertDetails(140), "alert coverage") {
		t.Fatal("healthy timestamps retained a data-quality warning")
	}
}

func TestProcessCoverageDiagnosticsSanitizeUntrustedLabels(t *testing.T) {
	m := testModel(nil)
	m.dash.Processes = []Process{{PID: "4\x1b[2J2", Command: "worker\n\u009b2J", CPUPercent: reading(5)}}
	text := m.alertDetails(120)
	for _, unsafe := range []string{"\x1b[2J", "\u009b", "worker\n"} {
		if strings.Contains(text, unsafe) {
			t.Fatalf("diagnostic emitted terminal control %q", unsafe)
		}
	}
}
