// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func panelEdge(title string, width int, bottom bool) string {
	if width < 2 {
		return ""
	}
	left, right := "╭", "╮"
	if bottom {
		left, right = "╰", "╯"
	}
	text := ""
	if title != "" {
		text = fitWidth(" "+title+" ", width-2)
		if width == 2 {
			text = ""
		}
	}
	return headingStyle.Render(left + text + strings.Repeat("─", max(0, width-2-lipgloss.Width(text))) + right)
}

func panelRow(content string, width int) string {
	if width < 3 {
		return fitWidth(content, width)
	}
	content = fitWidth(content, width-2)
	return headingStyle.Render("│") + content +
		strings.Repeat(" ", max(0, width-2-lipgloss.Width(content))) + headingStyle.Render("│")
}

func framePanel(content string, width int) string {
	if width < 3 {
		return fitWidth(content, width)
	}
	title, body, _ := strings.Cut(strings.TrimRight(content, "\n"), "\n")
	title = strings.TrimSpace(title)
	lines := []string{panelEdge(title, width, false)}
	body = lipgloss.NewStyle().Width(width - 2).Render(body)
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, panelRow(line, width))
	}
	return strings.Join(append(lines, panelEdge("", width, true)), "\n")
}
