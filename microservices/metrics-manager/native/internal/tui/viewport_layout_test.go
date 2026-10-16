// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestOverviewColumnsDoNotWaitForAdjacentTallPanels(t *testing.T) {
	rowOf := func(text, marker string) int {
		t.Helper()
		for i, line := range strings.Split(text, "\n") {
			if strings.Contains(line, marker) {
				return i
			}
		}
		t.Fatalf("missing panel %s", marker)
		return -1
	}
	for _, height := range []int{6, 20} {
		panels := []string{
			"CPU\n" + strings.Repeat("cpu row\n", height),
			"Memory\nmemory row",
			"GPU\ngpu row",
			"NPU\nnpu row",
		}
		view := packPanelColumns(panels, 139, panelMinWidth, 3)
		if rowOf(view, "NPU") != 3 || rowOf(view, "GPU") != height+2 {
			t.Fatalf("independent columns contain row-grid whitespace:\n%s", view)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) != 139 {
				t.Fatalf("column alignment changed: %d", lipgloss.Width(line))
			}
		}
	}
}

func TestDetailsClosesVisibleGroupAfterEverySelectionAndResize(t *testing.T) {
	for _, width := range []int{40, 80, 140, 190} {
		for _, height := range []int{24, 30, 40} {
			m := groupedDetailsModel(t, width, height)
			var tracker networkTracker
			m.dash.Network, _ = tracker.update(networkSamples(fixedNow, 0, "eth0", "host"), fixedNow, m.config.Network)
			for i := range m.detailItems() {
				m.selectDetail(i)
				frame := m.detailsLayout(width)
				if frame.visibleRows == 0 {
					continue
				}
				selectedRow := frame.itemRows[frame.selected]
				if selectedRow < frame.firstRow || selectedRow >= frame.firstRow+frame.visibleRows {
					t.Fatalf("%dx%d selection %d hidden by closing border", width, height, i)
				}
				body := strings.Split(ansi.Strip(m.detailsBody(width)), "\n")
				last := body[len(body)-1]
				if !strings.HasPrefix(last, "└") || !strings.HasSuffix(last, "┘") {
					t.Fatalf("%dx%d selection %d has no closing edge: %q", width, height, i, last)
				}
				if len(body) > m.visibleRows() {
					t.Fatalf("%dx%d closing edge was drawn outside viewport", width, height)
				}
				choice := m.detailSelection
				y := len(m.chromeLines(width)) + len(body) - 1
				m, _ = update(t, m, tea.MouseMsg{X: 2, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				if m.detailSelection != choice {
					t.Fatal("click on closing edge selected a hidden metric")
				}
			}
		}
	}
}

func TestSolidChartsUseOnlyBackgroundSpacesForPeaksAndZeros(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, profile := range []termenv.Profile{termenv.ANSI, termenv.ANSI256} {
		lipgloss.SetColorProfile(profile)
		m := testModel(nil)
		m.chartsConfig.GraphMode = "solid"
		m.chartsConfig.ChartHeight = 3
		spec := fixedChart("cpu.totalPercent", "CPU", "%", 0, 100)
		text := m.renderChart(spec, HistorySeries{Available: true, Points: []HistoryPoint{
			{At: fixedNow.Add(-5 * time.Minute), Value: 100},
			{At: fixedNow, Value: 0},
		}}, 18)
		if strings.ContainsAny(text, "▁▂▃▄▅▆▇█#+_") {
			t.Fatalf("solid mode emitted glyph-based bars: %q", text)
		}
		row := 0
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(ansi.Strip(line), " │") {
				continue
			}
			fill := lipgloss.NewStyle().Background(chartRowStyle(row, 3).GetForeground()).Render(" ")
			wantRuns := 1
			if row == 2 {
				wantRuns = 2
			}
			if strings.Count(line, fill) != wantRuns {
				t.Fatalf("row %d confused measured zero with gap: %q", row, line)
			}
			_, plot, _ := strings.Cut(ansi.Strip(line), " │")
			if plot != strings.Repeat(" ", 10) {
				t.Fatalf("solid plot contains non-space text or wrong width: %q", plot)
			}
			row++
		}
		if row != 3 {
			t.Fatalf("plot rows = %d", row)
		}
	}
}

func TestSolidChartsRemainVisibleWithoutColorSupport(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := testModel(nil)
	text := m.renderChart(fixedChart("cpu.totalPercent", "CPU", "%", 0, 100),
		HistorySeries{Available: true, Points: []HistoryPoint{{At: fixedNow, Value: 100}}}, 30)
	if !strings.Contains(text, "#") || strings.ContainsAny(text, "▁▂▃▄▅▆▇█") {
		t.Fatal("monochrome output hid the solid chart")
	}
}
