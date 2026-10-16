// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestBlueGradientUsesRowShadesInEveryChartModeAndTheme(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		lipgloss.SetHasDarkBackground(dark)
	})
	for _, dark := range []bool{false, true} {
		lipgloss.SetHasDarkBackground(dark)
		for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI} {
			lipgloss.SetColorProfile(profile)
			colors := []string{"#2d6eb4", "#1e518c", "#0f3464"}
			if dark {
				colors = []string{"#9bd7ff", "#5a9bd7", "#195faf"}
			}
			if profile == termenv.ANSI {
				colors = []string{"12", "12", "4"}
			}
			for _, mode := range []string{"solid", "unicode", "ascii", "braille"} {
				m := testModel(nil)
				m.chartsConfig.GraphMode, m.chartsConfig.ChartHeight = mode, 3
				series := HistorySeries{Available: true}
				for second := -300; second <= 0; second++ {
					series.Points = append(series.Points, HistoryPoint{At: fixedNow.Add(time.Duration(second) * time.Second), Value: 100})
				}
				text := m.renderChart(fixedChart("cpu.totalPercent", "CPU", "%", 0, 100), series, 18)
				axis, cell := " │", "█"
				switch mode {
				case "solid":
					cell = " "
				case "braille":
					cell = "⣿"
				case "ascii":
					axis, cell = " |", "#"
				}
				row := 0
				for _, line := range strings.Split(text, "\n") {
					label, _, ok := strings.Cut(ansi.Strip(line), axis)
					if !ok || strings.Trim(label, " 0123456789.-") != "" {
						continue
					}
					if row >= len(colors) {
						t.Fatal("unexpected additional plot row")
					}
					color := lipgloss.Color(colors[row])
					style := lipgloss.NewStyle().Foreground(color)
					if mode == "solid" {
						style = lipgloss.NewStyle().Background(color)
					}
					if !strings.Contains(line, style.Render(strings.Repeat(cell, 10))) {
						t.Fatalf("mode=%s profile=%v dark=%v row=%d does not use shade %s: %q",
							mode, profile, dark, row, colors[row], line)
					}
					if lipgloss.Width(line) != 18 {
						t.Fatal("gradient changed the chart width")
					}
					row++
				}
				if row != 3 {
					t.Fatalf("%s produced %d plot rows", mode, row)
				}
			}
		}
	}
}

func TestBlueGradientBrightensMonotonicallyAcrossSupportedHeights(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		lipgloss.SetHasDarkBackground(dark)
	})
	color := regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)
	for _, dark := range []bool{false, true} {
		lipgloss.SetHasDarkBackground(dark)
		for height := 3; height <= 20; height++ {
			previous := [3]int{}
			for row := height - 1; row >= 0; row-- {
				match := color.FindStringSubmatch(chartRowStyle(row, height).Render("x"))
				if len(match) != 4 {
					t.Fatal("true-color gradient did not render an RGB shade")
				}
				var rgb [3]int
				for channel := range rgb {
					var err error
					rgb[channel], err = strconv.Atoi(match[channel+1])
					if err != nil {
						t.Fatal(err)
					}
					if rgb[channel] < previous[channel] {
						t.Fatalf("height=%d row=%d did not brighten: %v after %v", height, row, rgb, previous)
					}
				}
				if rgb == previous {
					t.Fatalf("height=%d row=%d repeated the previous shade %v", height, row, rgb)
				}
				if rgb[2] <= rgb[1] || rgb[1] <= rgb[0] {
					t.Fatalf("shade left the blue palette: %v", rgb)
				}
				previous = rgb
			}
		}
	}
}

func TestGradientRetainsMonochromeFallbackAndBlankGaps(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := testModel(nil)
	m.chartsConfig.ChartHeight = 3
	spec := fixedChart("cpu.totalPercent", "CPU", "%", 0, 100)
	series := HistorySeries{Available: true, Points: []HistoryPoint{{At: fixedNow, Value: 100}}}
	text := m.renderChart(spec, series, 18)
	rows := plotRaster(text, "solid")
	if len(rows) != 3 {
		t.Fatal("missing monochrome plot")
	}
	for _, row := range rows {
		if string(row) != strings.Repeat(" ", 9)+"#" {
			t.Fatalf("gradient hid data or filled gaps: %q", string(row))
		}
	}
}
