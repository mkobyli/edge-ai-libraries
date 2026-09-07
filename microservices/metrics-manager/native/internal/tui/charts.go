// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const chartMinWidth = 52

func (m Model) trendsGrid(width int) string {
	columns := (width + panelGap) / (chartMinWidth + panelGap)
	if columns < 1 {
		columns = 1
	}
	if columns > 2 {
		columns = 2
	}
	columnWidth := (width - panelGap*(columns-1)) / columns

	var panels []string
	for _, spec := range m.chartsConfig.Charts {
		for _, series := range m.history.Series(spec.Metric) {
			panels = append(panels, m.renderChart(spec, series, columnWidth))
		}
	}
	if len(panels) == 0 {
		return labelStyle.Render("Collecting trend data…")
	}

	return packPanels(panels, width, chartMinWidth, 2)
}

func (m Model) renderChart(spec ChartSpec, series HistorySeries, width int) string {
	const axisWidth = 9
	plotWidth := width - axisWidth
	if plotWidth < 4 {
		plotWidth = 4
	}

	points := downsample(series.Points, plotWidth)
	minValue, maxValue := chartBounds(spec, points)
	height := m.chartsConfig.ChartHeight
	grid := make([][]rune, height)
	for row := range grid {
		grid[row] = []rune(strings.Repeat(" ", plotWidth))
	}
	for column, point := range points {
		position := int(math.Round((point.Value - minValue) / (maxValue - minValue) * float64(height-1)))
		position = max(0, min(height-1, position))
		grid[height-1-position][column] = '●'
	}

	var b strings.Builder
	title := sanitize(seriesTitle(spec, series), max(1, width-24))
	b.WriteString(headingStyle.Render(title))
	if len(points) > 0 {
		last, minimum, average, maximum := pointStats(series.Points)
		unit := sanitize(spec.Unit, 16)
		summary := fmt.Sprintf("  now %.1f%s  min %.1f  avg %.1f  max %.1f",
			last, unit, minimum, average, maximum)
		b.WriteString(labelStyle.Render(fitWidth(summary, max(1, width-lipgloss.Width(title)))))
	}
	b.WriteString("\n")

	for row := range grid {
		label := "       "
		if row == 0 {
			label = fmt.Sprintf("%6.1f ", maxValue)
		} else if row == height-1 {
			label = fmt.Sprintf("%6.1f ", minValue)
		}
		b.WriteString(labelStyle.Render(label + "│"))
		b.WriteString(string(grid[row]))
		b.WriteString("\n")
	}
	b.WriteString(labelStyle.Render(strings.Repeat(" ", axisWidth) + strings.Repeat("─", plotWidth)))
	b.WriteString("\n")
	window := m.chartsConfig.historyWindow()
	timeline := strings.Repeat(" ", axisWidth) + "-" + formatWindow(window)
	gap := width - lipgloss.Width(timeline) - len("now")
	if gap < 1 {
		gap = 1
	}
	b.WriteString(labelStyle.Render(fitWidth(timeline+strings.Repeat(" ", gap)+"now", width)))

	return b.String()
}

func downsample(points []HistoryPoint, width int) []HistoryPoint {
	if width <= 0 || len(points) == 0 {
		return nil
	}
	if len(points) <= width {
		return append([]HistoryPoint(nil), points...)
	}

	out := make([]HistoryPoint, 0, width)
	for bucket := 0; bucket < width; bucket++ {
		start := bucket * len(points) / width
		end := (bucket + 1) * len(points) / width
		peak := points[start]
		for _, point := range points[start+1 : end] {
			if point.Value > peak.Value {
				peak = point
			}
		}
		out = append(out, peak)
	}

	return out
}

func chartBounds(spec ChartSpec, points []HistoryPoint) (float64, float64) {
	if len(points) == 0 {
		return 0, 1
	}
	minValue, maxValue := points[0].Value, points[0].Value
	for _, point := range points[1:] {
		minValue = math.Min(minValue, point.Value)
		maxValue = math.Max(maxValue, point.Value)
	}
	if spec.Min != nil {
		minValue = *spec.Min
	}
	if spec.Max != nil {
		maxValue = *spec.Max
	}
	if minValue >= maxValue {
		switch {
		case spec.Min != nil && spec.Max == nil:
			maxValue = minValue + math.Max(1, math.Abs(minValue)*0.1)
		case spec.Min == nil && spec.Max != nil:
			minValue = maxValue - math.Max(1, math.Abs(maxValue)*0.1)
		default:
			padding := math.Max(1, math.Abs(minValue)*0.1)
			minValue -= padding
			maxValue += padding
		}
	}

	return minValue, maxValue
}

func pointStats(points []HistoryPoint) (last, minimum, average, maximum float64) {
	minimum, maximum = points[0].Value, points[0].Value
	for _, point := range points {
		average += point.Value
		minimum = math.Min(minimum, point.Value)
		maximum = math.Max(maximum, point.Value)
	}

	return points[len(points)-1].Value, minimum, average / float64(len(points)), maximum
}

func formatWindow(window time.Duration) string {
	if window%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(window.Hours()))
	}
	if window%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(window.Minutes()))
	}

	return fmt.Sprintf("%ds", int(window.Seconds()))
}
