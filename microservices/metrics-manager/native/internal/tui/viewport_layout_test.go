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

func TestOverviewGroupsDoNotInterleave(t *testing.T) {
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
	m := testModel(nil)
	m.dash.GPUs = []GPU{{ID: "0", Processes: []GPUProcess{{PID: "300", Command: "gpu-worker"}}}, {ID: "1"}}
	m.dash.NPU.Present = true
	m.dash.Processes = []Process{{PID: "200", Command: "cpu-worker"}}
	var network networkTracker
	m.dash.Network, _ = network.update(networkSamples(fixedNow, 0, "eth0", "host"), fixedNow, m.config.Network)
	var storage storageTracker
	m.dash.Storage, _ = storage.update(storageSamples(fixedNow, 0, 50), fixedNow, m.config.Storage)
	for _, width := range []int{100, 160, 240} {
		m.width = width
		view := ansi.Strip(m.panelGrid(width))
		previous := -1
		for _, marker := range []string{"┌ CPU", "┌ GPU 0 ", "┌ Network eth0", "┌ Filesystem /data", "┌ Processes", "┌ GPU 0 Processes"} {
			row := rowOf(view, marker)
			if row <= previous {
				t.Fatalf("groups interleaved at width %d:\n%s", width, view)
			}
			previous = row
		}
		wantContains(t, view, "cpu-worker")
		wantContains(t, view, "gpu-worker")
		if strings.Count(view, "gpu-worker") != 1 {
			t.Fatal("GPU process list was duplicated")
		}
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "┌ Processes") || strings.Contains(line, "┌ GPU 0 Processes") {
				if lipgloss.Width(line) != width {
					t.Fatalf("process frame is not full width: %q", line)
				}
			}
		}
		if width == 240 && !lineContainsAll(strings.Split(view, "\n"), "GPU 0", "GPU 1", "NPU") {
			t.Fatal("wide accelerator group did not use three columns")
		}
		if strings.Contains(m.gpuSection(m.dash.GPUs[0]), "gpu-worker") {
			t.Fatal("processes remained mixed into GPU hardware panel")
		}
	}
	m.width = 160
	for _, height := range []int{6, 20} {
		view := m.overviewGroup(160, []func(Model) string{
			func(Model) string { return "CPU\n" + strings.Repeat("cpu row\n", height) },
			func(Model) string { return "Memory\nmemory row" },
			func(Model) string { return "GPU\ngpu row" },
		})
		if rowOf(view, "GPU") <= rowOf(view, "cpu row")+height-1 {
			t.Fatal("next row started before preceding row completed")
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
	m.chartsConfig.GraphMode = "solid"
	text := m.renderChart(fixedChart("cpu.totalPercent", "CPU", "%", 0, 100),
		HistorySeries{Available: true, Points: []HistoryPoint{{At: fixedNow, Value: 100}}}, 30)
	if !strings.Contains(text, "#") || strings.ContainsAny(text, "▁▂▃▄▅▆▇█") {
		t.Fatal("monochrome output hid the solid chart")
	}
}
