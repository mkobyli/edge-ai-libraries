// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestPanelBordersHaveOneStyleDespiteNestedTitleColors(t *testing.T) {
	profile := lipgloss.ColorProfile()
	background := lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		lipgloss.SetHasDarkBackground(background)
	})
	for _, profile := range []termenv.Profile{termenv.ANSI, termenv.ANSI256} {
		lipgloss.SetColorProfile(profile)
		for _, dark := range []bool{true, false} {
			lipgloss.SetHasDarkBackground(dark)
			panel := framePanel(headingStyle.Render("GPU 0")+" "+criticalStyle.Render("device")+"\nvalue", 32)
			lines := strings.Split(panel, "\n")
			for _, row := range []int{0, len(lines) - 1} {
				plain := ansi.Strip(lines[row])
				if lines[row] != borderStyle.Render(plain) {
					t.Fatalf("profile %v dark=%v: header reset interrupted border color: %q", profile, dark, lines[row])
				}
			}
			if !strings.HasPrefix(lines[1], borderStyle.Render("│")) ||
				!strings.HasSuffix(lines[1], borderStyle.Render("│")) {
				t.Fatal("side borders differ from top/bottom border style")
			}
			for _, line := range lines {
				if lipgloss.Width(line) != 32 {
					t.Fatalf("border width = %d, want 32", lipgloss.Width(line))
				}
			}
		}
	}
}

func TestChartColorsFollowEveryThresholdAndKeepUnmonitoredMetricsNeutral(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := testModel(nil)
	for _, spec := range m.chartsConfig.Details.Metrics {
		threshold, monitored := m.config.Thresholds[thresholdMetric(spec.Metric)]
		if !monitored {
			spec.Min, spec.Max = floatPointer(0), floatPointer(100)
			text := m.renderChart(spec, HistorySeries{Available: true, Points: []HistoryPoint{{At: fixedNow, Value: 100}}}, 30)
			if !strings.Contains(text, chartStyle.Render("█")) {
				t.Errorf("%s has no fixed plot color", spec.Metric)
			}
			continue
		}
		for _, tc := range []struct {
			value    float64
			severity Severity
		}{
			{0, SeverityOK}, {threshold.Careful, SeverityCareful},
			{threshold.Warning, SeverityWarning}, {threshold.Critical, SeverityCritical},
		} {
			spec.Min, spec.Max = floatPointer(0), floatPointer(threshold.Critical)
			text := m.renderChart(spec, HistorySeries{Available: true, Points: []HistoryPoint{{At: fixedNow, Value: tc.value}}}, 30)
			found := false
			for _, cell := range "▁▂▃▄▅▆▇█" {
				if strings.Contains(text, severityStyle(tc.severity).Render(string(cell))) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s value %g lacks %v plot color", spec.Metric, tc.value, tc.severity)
			}
		}
	}
}

func TestChartModesPreserveMeasuredZeroGapsPeaksAndWidths(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, mode := range []string{"unicode", "ascii"} {
		m := testModel(nil)
		m.chartsConfig.GraphMode = mode
		m.chartsConfig.ChartHeight = 3
		spec := fixedChart("cpu.totalPercent", "CPU", "%", 0, 100)
		series := HistorySeries{Available: true, Points: []HistoryPoint{
			{At: fixedNow.Add(-5 * time.Minute), Value: 100},
			{At: fixedNow, Value: 0},
		}}
		text := ansi.Strip(m.renderChart(spec, series, 18))
		axis, peak, zero := " │", '█', '▁'
		if mode == "ascii" {
			axis, peak, zero = " |", '#', '_'
			for _, r := range text {
				if r > 127 {
					t.Fatalf("ASCII plot contains non-ASCII rune %q", r)
				}
			}
		}
		var grid [][]rune
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > 18 {
				t.Fatalf("%s plot exceeds width: %q", mode, line)
			}
			if label, plot, ok := strings.Cut(line, axis); ok && len(label) == 6 && strings.Trim(label, " 0123456789.-") == "" {
				grid = append(grid, []rune(plot))
			}
		}
		if len(grid) != 3 {
			t.Fatalf("%s plot rows = %d", mode, len(grid))
		}
		for _, row := range grid {
			if len(row) != 10 || row[0] != peak || strings.TrimSpace(string(row[1:9])) != "" {
				t.Fatalf("%s lost a peak/gap or misaligned cells: %q", mode, string(row))
			}
		}
		if grid[2][9] != zero || grid[0][9] != ' ' {
			t.Fatalf("%s turned zero into missing data or full-height data", mode)
		}
	}
}

func TestChartStylesAreBatchedAcrossAdjacentBuckets(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := testModel(nil)
	series := HistorySeries{Available: true}
	for i := 0; i <= 300; i++ {
		series.Points = append(series.Points, HistoryPoint{At: fixedNow.Add(time.Duration(i-300) * time.Second), Value: 100})
	}
	text := m.renderChart(fixedChart("cpu.totalPercent", "CPU", "%", 0, 100), series, 160)
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(ansi.Strip(line), " │") && strings.Count(line, "\x1b[") > 10 {
			t.Fatalf("plot emits style changes for each cell instead of each run: %q", line)
		}
	}
}

func TestNetworkDetailCellsKeepMetricNamesAndFullDeviceIdentityInHeader(t *testing.T) {
	m := testModel(nil)
	m.width, m.height, m.activeTab = 160, 60, DetailsTab
	var tracker networkTracker
	samples := networkSamples(fixedNow, 0, "enp165s0", "long-hostname.example.intel.com")
	m.dash.Network, _ = tracker.update(samples, fixedNow, m.config.Network)
	var titles []string
	for _, item := range m.detailItems() {
		if !strings.HasPrefix(item.spec.Metric, "network.") {
			continue
		}
		if detailCellTitle(item) != item.spec.Title {
			t.Fatalf("Network cell repeats device identity: %q", detailCellTitle(item))
		}
		titles = append(titles, detailCellTitle(item))
		m.detailSelection = item.choice
	}
	if len(titles) != 10 {
		t.Fatalf("network metric count = %d", len(titles))
	}
	text := ansi.Strip(m.detailsBody(m.width))
	for _, title := range titles {
		wantContains(t, text, title)
	}
	frame := m.detailsLayout(m.width)
	wantContains(t, ansi.Strip(frame.chart[0]), "Network enp165s0 @ long-hostname.example.intel.com")
	for _, row := range strings.Split(text, "\n") {
		if strings.HasPrefix(row, "│") && strings.Contains(row, "long-hostname") {
			t.Fatalf("device name still occupies a metric cell: %q", row)
		}
	}
}

func TestIntervalActiveTabsAndBottomSeparatorAcrossViews(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := acceleratorModel(t, 30)
	m.width = 140
	for tab := OverviewTab; tab < tabCount; tab++ {
		m.switchTab(tab)
		view := strings.Split(m.View(), "\n")
		if len(view) != m.height {
			t.Fatalf("tab %d rows = %d, want %d", tab, len(view), m.height)
		}
		wantContains(t, m.tabLine(), activeTabStyle.Render("["+m.tabLabels()[tab]+"]"))
		wantContains(t, m.View(), headingStyle.Render("interval: 500ms"))
		if view[len(view)-5] != borderStyle.Render(strings.Repeat("─", m.width)) {
			t.Fatalf("tab %d does not separate the body from the alert footer", tab)
		}
		for _, line := range view {
			if lipgloss.Width(line) > m.width {
				t.Fatalf("tab %d line exceeds terminal width", tab)
			}
		}
	}
	m, _ = update(t, m, key("h"))
	view := strings.Split(m.View(), "\n")
	if view[len(view)-5] != borderStyle.Render(strings.Repeat("─", m.width)) {
		t.Fatal("help lost the fixed bottom separator")
	}
}

func TestGraphModeValidationInheritanceAndPackagedDefault(t *testing.T) {
	for _, mode := range []string{"unicode", "ascii"} {
		config, err := LoadChartsConfig(writeConfig(t, `{"graphMode":"`+mode+`"}`))
		if err != nil || config.GraphMode != mode {
			t.Fatalf("mode %s: %+v, %v", mode, config, err)
		}
	}
	if _, err := LoadChartsConfig(writeConfig(t, `{"graphMode":"braille"}`)); err == nil {
		t.Fatal("unsupported graph mode was accepted")
	}
	old, err := LoadChartsConfig(writeConfig(t, `{}`))
	if err != nil || old.GraphMode != "unicode" {
		t.Fatalf("old configuration lost the Unicode default: %+v, %v", old, err)
	}
	packaged, err := LoadChartsConfig("../../packaging/etc/tui-charts.json")
	if err != nil || packaged.GraphMode != DefaultChartsConfig().GraphMode {
		t.Fatalf("packaged graph mode differs: %+v, %v", packaged, err)
	}
}

func TestDetailsUsesTheConfiguredASCIIPlotMode(t *testing.T) {
	m := detailsModel(t, 140, 40)
	m.chartsConfig.GraphMode = "ascii"
	frame := m.detailsLayout(140)
	rows := 0
	for _, line := range frame.chart {
		plain := ansi.Strip(line)
		if strings.ContainsAny(plain, "▁▂▃▄▅▆▇█") {
			t.Fatalf("Details ignored ASCII plot mode: %q", plain)
		}
		if strings.HasPrefix(plain, "│") && len(plain) > 1 {
			label, _, found := strings.Cut(strings.TrimPrefix(plain, "│"), " |")
			if found && len(label) == 6 && strings.Trim(label, " 0123456789.-") == "" {
				rows++
			}
		}
	}
	if rows != m.chartsConfig.Details.ChartHeight {
		t.Fatalf("Details has %d ASCII axis rows, want %d", rows, m.chartsConfig.Details.ChartHeight)
	}
}
