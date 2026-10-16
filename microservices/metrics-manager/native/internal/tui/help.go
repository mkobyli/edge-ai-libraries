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
		"  1 / 2 / 3 / 4: Overview / Trends / Details / Alerts.\n" +
		"  Tab / Shift+Tab or click a tab: change tab.\n\n" +
		"Processes - Overview (CPU and GPU lists)\n" +
		"  CPU columns: PID, Process, Threads, User, Memory (RSS), CPU.\n" +
		"  Threads counts OS threads, not busy CPUs or Go goroutines; no health threshold.\n" +
		"  User is the real UID's name on the monitored host, or the numeric UID if unresolved.\n" +
		"  Older collectors may omit Threads/User; missing values are not zero.\n" +
		"  s: cycle sort key: usage, memory, PID.\n" +
		"  r: reverse sort direction. Current sorting is shown above each list.\n" +
		"  c: toggle process CPU per-core / total; does not affect GPU usage.\n" +
		"  per-core: 100% = one logical CPU; multithreaded processes can exceed 100%.\n" +
		"  total: divide by the monitored host's reported logical CPU count.\n" +
		"  Missing/invalid CPU count: total is n/a, never guessed from the TUI machine.\n" +
		"  Colors and alerts always use per-core thresholds, not the displayed total.\n" +
		"  Set processes.cpuMode in the dashboard config to persist the startup mode.\n" +
		"  Sorting only covers processes published by the collectors.\n\n" +
		"Scrolling - Overview, Trends, alerts and help\n" +
		"  Up / Down or k / j: scroll one line.\n" +
		"  PgUp / PgDn: previous / next page. Space: next page.\n" +
		"  Home / End or g / G: first / last line.\n\n" +
		"  Highlighted viewport edges say more metrics/charts/alert details above or below.\n" +
		"  Indicators disappear at that edge when no more content is hidden.\n\n" +
		"Details - select the metric plotted above the list\n" +
		"  Arrow keys: select immediately. j / k / l: down / up / right.\n" +
		"  Use Left Arrow to move left; h opens help in every view.\n" +
		"  Enter: confirm the selected metric.\n" +
		"  PgUp / PgDn: previous / next page of metrics. Space: next page.\n" +
		"  Home / End or g / G: first / last metric.\n" +
		"  Click: select a metric. Mouse wheel: move up / down.\n" +
		"  Mouse capture enables tab clicks and scrolling; help releases it.\n" +
		"  Set mouse=false in the dashboard config to disable all capture.\n" +
		"  details.mouse=false disables only metric click/wheel selection.\n" +
		"  CPU, Memory, individual GPUs, NPU and network interfaces have separate sections.\n\n" +
		"Reading the display\n" +
		"  Overview: current readings. Trends: multiple history charts.\n" +
		"  Details: one selected chart; both use 5 minutes by default.\n" +
		"  Filled plots preserve blank gaps where measurements are missing.\n" +
		"  Time buckets are anchored: closed columns move together, not regrouped per poll.\n" +
		"  Auto scales expand in steps; halving the range needs 30s before shrinking.\n" +
		"  Plots fade from dark blue at the baseline to light blue at the top of the axis.\n" +
		"  Shades depend on height, not health or trend direction; values keep threshold colors.\n" +
		"  Nonnegative metrics default to a zero baseline, including all-zero samples.\n" +
		"  graphMode=\"braille\" (default) uses 2x4 dots per cell; requires font support.\n" +
		"  Use graphMode=\"solid\" for background spaces if dotted glyphs render poorly.\n" +
		"  Existing explicit graphMode settings are preserved; choose braille to switch.\n" +
		"  interval shows the configured TUI interval, not the collector cadence.\n" +
		"  Set -interval at startup to change it (for example -interval 1s).\n" +
		"  The fixed bottom alert preview can be disabled with alerts.bottomBar=false.\n" +
		"  Alert colors indicate configured diagnostic thresholds,\n" +
		"  not universal hardware safety limits. Press a for events.\n" +
		"  Frequency, power and bandwidth stay neutral without a configured limit.\n" +
		"  recovered: readings confirm the alert ended.\n" +
		"  no longer observed: observations stopped, not confirmed recovery.\n" +
		"  Alert evidence: activation value, peak since activation and confirmed recovery.\n" +
		"  A low last-observed value may be from the recovery-confirmation period.\n" +
		"  Process CPU alert evidence: 100% means one logical CPU, regardless of display mode.\n" +
		"  Process CPU alerts need distinct timestamps no older than 10s.\n" +
		"  [4/a] Open alerts shows events and per-process data-quality diagnostics.\n" +
		"  Process CPU alerts: limited (CPU! on narrow screens) means incomplete evaluation,\n" +
		"  not stopped collection. The process table remains available.\n" +
		"  Cached process samples older than 10s are excluded before ranking.\n" +
		"  Their retention alone is informational; loss of all current process data is flagged.\n" +
		"  Collection and history continue while help is open.\n\n" +
		"Network - optional Telegraf net input\n" +
		"  RX/TX rates need two distinct, timestamped counter measurements.\n" +
		"  Cached samples do not advance Network charts or alert streaks.\n" +
		"  Unknown link speed means no utilization percentage or utilization alert.\n" +
		"  network.interfaces filters exact names; network.speedMbps supplies known limits.\n" +
		"  Errors/drops use events/s, not lifetime totals; rate colors share alert thresholds.\n" +
		"  -once shows totals but cannot calculate a rate from one snapshot.\n" +
		"  See the native package guide to enable the optional input.\n\n" +
		"Startup reference - defaults, not the current session settings\n\n" +
		CommandLineHelp()
	return lipgloss.NewStyle().Width(width).Render(content)
}

func (m *Model) toggleHelp() {
	if m.showHelp {
		m.helpOffset = m.offset
		m.showHelp = false
		m.offset = m.scrollTo(m.tabOffsets[m.activeTab])
	} else {
		m.setOffset(m.offset)
		m.showHelp = true
		m.offset = m.scrollTo(m.helpOffset)
	}
}
