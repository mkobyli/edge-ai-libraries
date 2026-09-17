// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type detailChoice struct {
	key, metric, label string
}

type detailItem struct {
	choice detailChoice
	spec   ChartSpec
	series HistorySeries
	value  Reading
}

type detailsFrame struct {
	items                 []detailItem
	chart                 []string
	selected              int
	columns, cellWidth    int
	firstRow, visibleRows int
}

func detailTitle(item detailItem) string {
	if item.series.Label != "" && !strings.HasPrefix(item.spec.Title, item.series.Label+" ") {
		return item.series.Label + " / " + item.spec.Title
	}
	return item.spec.Title
}

func (m Model) detailItems() []detailItem {
	observations := chartObservations(m.dash)
	var items []detailItem
	for _, spec := range m.chartsConfig.Details.Metrics {
		instances := make(map[string]detailItem)
		for _, series := range m.history.Series(spec.Metric) {
			instances[series.Key] = detailItem{
				choice: detailChoice{series.Key, series.Metric, series.Label},
				spec:   spec, series: series,
			}
		}
		for _, observation := range observations {
			if observation.metric != spec.Metric {
				continue
			}
			item := instances[observation.key]
			item.choice = detailChoice{observation.key, spec.Metric, observation.label}
			item.spec = spec
			item.value = observation.value
			if m.err != nil {
				item.value = Reading{}
			}
			if item.series.Key == "" {
				item.series = HistorySeries{Key: observation.key, Metric: spec.Metric, Label: observation.label}
			}
			instances[observation.key] = item
		}
		// Keep a selected device visible after its history expires instead of
		// silently switching the plot to another device.
		if m.detailSelection.metric == spec.Metric && m.detailSelection.key != "" {
			if _, exists := instances[m.detailSelection.key]; !exists {
				instances[m.detailSelection.key] = detailItem{
					choice: m.detailSelection, spec: spec,
					series: HistorySeries{Key: m.detailSelection.key, Metric: spec.Metric, Label: m.detailSelection.label},
				}
			}
		}
		keys := make([]string, 0, len(instances))
		for key := range instances {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if spec.Metric == "cpu.classPercent" {
			sort.SliceStable(keys, func(i, j int) bool {
				a, aKnown := classOrder[instances[keys[i]].series.Label]
				b, bKnown := classOrder[instances[keys[j]].series.Label]
				if aKnown != bKnown {
					return aKnown
				}
				return aKnown && a < b
			})
		}
		for _, key := range keys {
			items = append(items, instances[key])
		}
		if len(keys) == 0 {
			items = append(items, detailItem{
				choice: detailChoice{metric: spec.Metric}, spec: spec,
				series: HistorySeries{Metric: spec.Metric},
			})
		}
	}
	return items
}

func (m Model) detailIndex(items []detailItem) int {
	for i, item := range items {
		if m.detailSelection.key != "" {
			if item.choice.key == m.detailSelection.key && item.choice.metric == m.detailSelection.metric {
				return i
			}
		} else if item.spec.Metric == m.detailSelection.metric {
			return i
		}
	}
	return 0
}

func (m Model) detailsLayout(width int) detailsFrame {
	frame := detailsFrame{items: m.detailItems()}
	frame.selected = m.detailIndex(frame.items)
	frame.columns = max(1, min(3, (width+panelGap)/(36+panelGap)))
	frame.cellWidth = (width - panelGap*(frame.columns-1)) / frame.columns
	item := frame.items[frame.selected]
	plotSpec, plotSeries := item.spec, item.series
	plotSpec.Title = detailTitle(item)
	plotSeries.Label = ""
	chartModel := m
	chartModel.chartsConfig.ChartHeight = m.chartsConfig.Details.ChartHeight
	render := func() []string {
		return strings.Split(strings.TrimRight(chartModel.renderChart(plotSpec, plotSeries, width), "\n"), "\n")
	}
	frame.chart = render()
	available := m.visibleRows()
	if m.height > 0 {
		for len(frame.chart)+3 > available && chartModel.chartsConfig.ChartHeight > 3 {
			chartModel.chartsConfig.ChartHeight--
			frame.chart = render()
		}
		if len(frame.chart)+2 > available {
			frame.chart = []string{
				fitWidth(headingStyle.Render(sanitize(detailTitle(item), width)), width),
				"Taller terminal needed for plot.",
			}
			frame.chart = frame.chart[:min(len(frame.chart), max(0, available-2))]
		}
	}
	totalRows := (len(frame.items) + frame.columns - 1) / frame.columns
	frame.visibleRows = totalRows
	if m.height > 0 {
		frame.visibleRows = min(totalRows, max(0, available-len(frame.chart)-1))
	}
	frame.firstRow = min(m.detailTop, max(0, totalRows-frame.visibleRows))
	selectedRow := frame.selected / frame.columns
	if selectedRow < frame.firstRow {
		frame.firstRow = selectedRow
	} else if frame.visibleRows > 0 && selectedRow >= frame.firstRow+frame.visibleRows {
		frame.firstRow = selectedRow - frame.visibleRows + 1
	}
	return frame
}

func (m Model) detailsBody(width int) string {
	frame := m.detailsLayout(width)
	lines := append([]string(nil), frame.chart...)
	status := fmt.Sprintf("Select metric (%d/%d)", frame.selected+1, len(frame.items))
	if m.history.limited {
		status += " | history series limit reached"
	}
	lines = append(lines, headingStyle.Render(status))
	for row := frame.firstRow; row < frame.firstRow+frame.visibleRows; row++ {
		var cells []string
		for col := 0; col < frame.columns; col++ {
			i := row*frame.columns + col
			if i >= len(frame.items) {
				break
			}
			item := frame.items[i]
			value := m.thresholdValue(item.spec.Metric, item.value, func(value Reading) string {
				if text, missing := placeholder(value); missing {
					return text
				}
				return fmt.Sprintf("%.1f%s", value.Value, sanitize(item.spec.Unit, 16))
			})
			label := sanitize(detailTitle(item), 180)
			marker := "  "
			if i == frame.selected {
				marker = "> "
			}
			labelWidth := max(0, frame.cellWidth-lipgloss.Width(value)-3)
			cell := marker + fitWidth(label, labelWidth)
			// fitWidth intentionally leaves width=0 unchanged.
			if labelWidth == 0 {
				cell = marker
			}
			cell += strings.Repeat(" ", max(1, frame.cellWidth-lipgloss.Width(cell)-lipgloss.Width(value))) + value
			cell = fitWidth(cell, frame.cellWidth)
			if i == frame.selected {
				cell = titleStyle.Render(cell)
			}
			cells = append(cells, cell)
		}
		lines = append(lines, strings.Join(cells, strings.Repeat(" ", panelGap)))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) selectDetail(index int) {
	items := m.detailItems()
	index = max(0, min(len(items)-1, index))
	m.detailSelection = items[index].choice
	m.detailTop = m.detailsLayout(m.renderWidth()).firstRow
}

func (m *Model) detailKey(key string) bool {
	frame := m.detailsLayout(m.renderWidth())
	index := frame.selected
	switch key {
	case "up", "k":
		index -= frame.columns
	case "down", "j":
		index += frame.columns
	case "left", "h":
		index--
	case "right", "l":
		index++
	case "pgup":
		index -= max(1, frame.visibleRows) * frame.columns
	case "pgdown", " ":
		index += max(1, frame.visibleRows) * frame.columns
	case "home", "g":
		index = 0
	case "end", "G":
		index = len(frame.items) - 1
	case "enter":
	default:
		return false
	}
	m.selectDetail(index)
	return true
}

func (m Model) detailsMouseEnabled() bool {
	return m.activeTab == DetailsTab && !m.showAlerts && m.chartsConfig.Details.Mouse
}

func (m *Model) detailMouse(msg tea.MouseMsg) {
	if !m.detailsMouseEnabled() || msg.Action != tea.MouseActionPress {
		return
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.detailKey("up")
	case tea.MouseButtonWheelDown:
		m.detailKey("down")
	case tea.MouseButtonLeft:
		width := m.renderWidth()
		frame := m.detailsLayout(width)
		row := msg.Y - len(m.chromeLines(width)) - len(frame.chart) - 1
		if row < 0 || row >= frame.visibleRows || msg.X < 0 || msg.X >= width {
			return
		}
		col := msg.X / (frame.cellWidth + panelGap)
		if col >= frame.columns || msg.X%(frame.cellWidth+panelGap) >= frame.cellWidth {
			return
		}
		index := (frame.firstRow+row)*frame.columns + col
		if index < len(frame.items) {
			m.selectDetail(index)
		}
	}
}
