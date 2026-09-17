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
		return labelStyle.Render("No samples in the current window. Waiting for trend data.")
	}

	grid := packPanels(panels, width, chartMinWidth, 2)
	if m.history.limited {
		return labelStyle.Render(fmt.Sprintf("History series limit (%d) reached; additional series omitted.",
			maxHistorySeries)) + "\n" + grid
	}
	return grid
}

func (m Model) renderChart(spec ChartSpec, series HistorySeries, width int) string {
	end := m.now()
	window := m.chartsConfig.historyWindow()
	start := end.Add(-window)
	points := windowPoints(series.Points, start, end)
	minValue, maxValue := chartBounds(spec, points)

	var b strings.Builder
	b.WriteString(headingStyle.Render(sanitize(seriesTitle(spec, series), width)) + "\n")
	if len(points) > 0 {
		last, minimum, average, maximum := pointStats(points)
		unit := sanitize(spec.Unit, 16)
		latest := points[len(points)-1]
		b.WriteString(fmt.Sprintf("last %.1f%s | %s\n", last, unit, formatAge(end.Sub(latest.At))))
		b.WriteString(fmt.Sprintf("min %.1f | sample avg %.1f | max %.1f\n", minimum, average, maximum))
		if !series.Available || m.err != nil {
			b.WriteString("No current measurement; showing retained samples.\n")
		}
	} else {
		b.WriteString("No samples in this time window.\n")
	}

	labelWidth := max(6, len(fmt.Sprintf("%.1f", minValue)), len(fmt.Sprintf("%.1f", maxValue)))
	axisWidth := labelWidth + 2
	plotWidth := width - axisWidth
	if plotWidth < 4 {
		b.WriteString("Widen terminal for plot.\n")
		return lipgloss.NewStyle().Width(max(1, width)).Render(b.String())
	}
	buckets := timeBuckets(points, plotWidth, start, end)
	height := m.chartsConfig.ChartHeight
	grid := make([][]rune, height)
	for row := range grid {
		grid[row] = []rune(strings.Repeat(" ", plotWidth))
	}
	for column, point := range buckets {
		if !point.OK {
			continue
		}
		position := int(math.Round((point.Value - minValue) / (maxValue - minValue) * float64(height-1)))
		position = max(0, min(height-1, position))
		grid[height-1-position][column] = '●'
	}

	for row := range grid {
		label := strings.Repeat(" ", labelWidth)
		if row == 0 {
			label = fmt.Sprintf("%*.1f", labelWidth, maxValue)
		} else if row == height-1 {
			label = fmt.Sprintf("%*.1f", labelWidth, minValue)
		}
		b.WriteString(labelStyle.Render(label + " │"))
		b.WriteString(string(grid[row]))
		b.WriteString("\n")
	}
	b.WriteString(labelStyle.Render(strings.Repeat(" ", axisWidth) + strings.Repeat("─", plotWidth)))
	b.WriteString("\n")
	left := "-" + formatWindow(window)
	timeline := "now"
	if plotWidth >= len(left)+4 {
		timeline = left + strings.Repeat(" ", plotWidth-len(left)-3) + "now"
	}
	b.WriteString(labelStyle.Render(strings.Repeat(" ", axisWidth) + timeline))

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func windowPoints(points []HistoryPoint, start, end time.Time) []HistoryPoint {
	var visible []HistoryPoint
	for _, point := range points {
		if !point.At.Before(start) && !point.At.After(end) &&
			!math.IsNaN(point.Value) && !math.IsInf(point.Value, 0) {
			visible = append(visible, point)
		}
	}
	return visible
}

// Each column represents an equal time interval, not an equal sample count.
// Empty columns stay empty: neither missing data nor startup becomes zero.
func timeBuckets(points []HistoryPoint, width int, start, end time.Time) []Reading {
	if width <= 0 || !end.After(start) {
		return nil
	}
	buckets := make([]Reading, width)
	for _, point := range points {
		if point.At.Before(start) || point.At.After(end) ||
			math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
			continue
		}
		column := min(width-1, int(float64(point.At.Sub(start))/float64(end.Sub(start))*float64(width)))
		if !buckets[column].OK || point.Value > buckets[column].Value {
			buckets[column] = reading(point.Value)
		}
	}
	return buckets
}

func chartBounds(spec ChartSpec, points []HistoryPoint) (float64, float64) {
	minValue, maxValue := 0.0, 1.0
	if len(points) > 0 {
		minValue, maxValue = points[0].Value, points[0].Value
	}
	for _, point := range points {
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
