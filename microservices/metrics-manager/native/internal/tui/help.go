// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import "github.com/charmbracelet/lipgloss"

func (m Model) helpBody(width int) string {
	content := "Help - mm-tui\n\n" +
		"Navigation\n" +
		"  h: open/close help and return to the previous view.\n" +
		"  Esc: close help; otherwise close alerts or quit from a tab.\n" +
		"  q / Ctrl+C: quit, including from help or alerts.\n" +
		"  a: open/close alerts; from help, go to alerts.\n" +
		"  1 / 2 / 3: Overview / Trends / Details.\n" +
		"  Tab / Shift+Tab: next / previous tab.\n\n" +
		"Scrolling - Overview, Trends, alerts and help\n" +
		"  Up / Down or k / j: scroll one line.\n" +
		"  PgUp / PgDn: previous / next page. Space: next page.\n" +
		"  Home / End or g / G: first / last line.\n\n" +
		"Details - select the metric plotted above the list\n" +
		"  Arrow keys: select immediately. j / k / l: down / up / right.\n" +
		"  Use Left Arrow to move left; h opens help in every view.\n" +
		"  Enter: confirm the selected metric.\n" +
		"  PgUp / PgDn: previous / next page of metrics. Space: next page.\n" +
		"  Home / End or g / G: first / last metric.\n" +
		"  Click: select a metric. Mouse wheel: move up / down.\n" +
		"  Mouse capture applies only in Details when details.mouse=true;\n" +
		"  it is disabled while help or alerts are open.\n" +
		"  CPU, Memory, individual GPUs and NPU have separate sections.\n\n" +
		"Reading the display\n" +
		"  Overview: current readings. Trends: multiple history charts.\n" +
		"  Details: one selected chart; both use 5 minutes by default.\n" +
		"  Alert colors indicate configured diagnostic thresholds,\n" +
		"  not universal hardware safety limits. Press a for events.\n" +
		"  recovered: readings confirm the alert ended.\n" +
		"  no longer observed: observations stopped, not confirmed recovery.\n" +
		"  Collection and history continue while help is open.\n\n" +
		"Startup reference - defaults, not the current session settings\n\n" +
		CommandLineHelp()
	return lipgloss.NewStyle().Width(width).Render(content)
}

func (m *Model) toggleHelp() {
	if m.showHelp {
		m.helpOffset = m.offset
		m.showHelp = false
		if m.showAlerts {
			m.offset = m.scrollTo(m.alertOffset)
		} else {
			m.offset = m.scrollTo(m.tabOffsets[m.activeTab])
		}
	} else {
		m.setOffset(m.offset)
		m.showHelp = true
		m.offset = m.scrollTo(m.helpOffset)
	}
}
