// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) alertSummary(width int) string {
	var counts [4]int
	for _, alert := range m.alerts.Visible() {
		counts[alert.Severity]++
	}
	var summary string
	if width < 70 {
		summary = fmt.Sprintf("Alerts C:%d W:%d", counts[SeverityCritical], counts[SeverityWarning])
		if m.config.Alerts.ShowCareful {
			summary += fmt.Sprintf(" I:%d", counts[SeverityCareful])
		}
	} else {
		summary = fmt.Sprintf("Alerts: %d critical | %d warning",
			counts[SeverityCritical], counts[SeverityWarning])
		if m.config.Alerts.ShowCareful {
			summary += fmt.Sprintf(" | %d careful", counts[SeverityCareful])
		}
	}
	summary += " | 4/a details"
	switch {
	case m.err != nil:
		summary = "Alerts: no contact | a details"
	case m.updatedAt.IsZero():
		summary = "Alerts: waiting for data"
	case m.now().Sub(m.updatedAt) > m.staleAfter():
		summary = "Alerts: stale data | a details"
	case counts[SeverityCritical] > 0:
		return criticalStyle.Render(summary)
	case counts[SeverityWarning] > 0:
		return warningStyle.Render(summary)
	}
	return labelStyle.Render(summary)
}

func (m Model) alertDetails(width int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Alerts") + "\n")
	b.WriteString("High utilization may be expected during inference; thresholds are diagnostic, not safety limits.\n\n")
	b.WriteString("History shows ended episodes, not current load. Process CPU uses one logical CPU as 100%; host charts show aggregate CPU.\n\n")
	if m.err != nil || (!m.updatedAt.IsZero() && m.now().Sub(m.updatedAt) > m.staleAfter()) {
		b.WriteString("Data is stale: these are last known events, not confirmed current conditions.\n\n")
	}

	alerts := m.alerts.Visible()
	b.WriteString(headingStyle.Render(fmt.Sprintf("Active (%d)", len(alerts))) + "\n")
	if len(alerts) == 0 {
		b.WriteString("No active events recorded.\n")
	}
	for _, alert := range alerts {
		b.WriteString(renderAlert(alert, m.updatedAt, "active"))
	}
	b.WriteString("\n" + headingStyle.Render(fmt.Sprintf("Recent history (%d/%d)",
		len(m.alerts.events), m.config.Alerts.MaxHistory)) + "\n")
	if len(m.alerts.events) == 0 {
		b.WriteString("No ended events recorded in this session.\n")
	}
	for _, event := range m.alerts.events {
		b.WriteString(renderAlert(event.Alert, event.EndedAt, event.Outcome))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func (m Model) bottomBarRows() int {
	if !m.config.Alerts.BottomBar || (m.height > 0 && m.height < len(m.chromeLines(m.renderWidth()))+6) {
		return 0
	}
	return 3
}

func (m Model) bottomAlertLines(width int) []string {
	if m.bottomBarRows() == 0 {
		return nil
	}
	detail := "No active events recorded."
	alerts := m.alerts.Visible()
	if len(alerts) > 0 {
		alert := alerts[0]
		unit := thresholdUnit(alert.Metric)
		status := ""
		if alert.Unavailable || m.err != nil || m.now().Sub(m.updatedAt) > m.staleAfter() {
			status = " [last observed]"
		}
		detail = severityStyle(alert.Severity).Render(fmt.Sprintf("%s %s %s %.1f%s%s",
			alert.Severity, sanitize(alert.Device, 60), sanitize(alert.Metric, 60), alert.Value, unit, status))
	}
	return []string{
		borderStyle.Render(strings.Repeat("─", width)),
		fitWidth(m.alertSummary(width), width), fitWidth(detail, width),
	}
}

func renderAlert(alert Alert, end time.Time, outcome string) string {
	label := alert.Metric
	if alert.Device != "" {
		label = alert.Device + " / " + label
	}
	unit := thresholdUnit(alert.Metric)
	valueLabel := "last observed"
	if outcome == "active" {
		valueLabel = "current"
		if alert.Unavailable {
			outcome = "waiting for data"
			valueLabel = "last observed"
		}
	}
	duration := max(time.Duration(0), end.Sub(alert.Since)).Truncate(time.Second)
	evidence := ""
	if alert.TriggerValue.OK {
		evidence += fmt.Sprintf("  activated at %.1f%s | activation threshold %.1f%s\n",
			alert.TriggerValue.Value, unit, alert.TriggerThreshold, unit)
	}
	if alert.PeakValue.OK {
		evidence += fmt.Sprintf("  peak since activation %.1f%s at %s\n",
			alert.PeakValue.Value, unit, alert.PeakAt.Format("2006-01-02 15:04:05"))
	}
	if alert.RecoveryValue.OK {
		evidence += fmt.Sprintf("  confirmed recovery at %.1f%s\n", alert.RecoveryValue.Value, unit)
	}
	if outcome == "active" && alert.Value < alert.Threshold {
		evidence += "  Value is below the event threshold; recovery is not yet confirmed.\n"
	}
	return severityStyle(alert.Severity).Render(
		fmt.Sprintf("%s %s [%s]", alert.Severity, sanitize(label, 180), outcome)) + "\n" +
		fmt.Sprintf("  %s %.1f%s | threshold %.1f%s\n", valueLabel, alert.Value, unit, alert.Threshold, unit) +
		evidence +
		fmt.Sprintf("  since %s | through %s | duration %s\n",
			alert.Since.Format("2006-01-02 15:04:05"), end.Format("15:04:05"), duration) +
		"  " + sanitize(alert.Message, 200) + "\n\n"
}
