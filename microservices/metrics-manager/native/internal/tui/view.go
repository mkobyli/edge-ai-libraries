// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/open-edge-platform/edge-ai-libraries/microservices/metrics-manager/native/internal/source"
)

// defaultWidth is used until the terminal reports its size.
const defaultWidth = 80

const (
	// panelMinWidth leaves enough room for the widest hardware tables. Wider
	// terminals gain columns only when every panel can remain readable.
	panelMinWidth = 68
	panelGap      = 3
)

// staleAfter is how old the displayed numbers may get before the freshness
// indicator is highlighted. It is a multiple of the poll interval so a single
// missed refresh does not raise a false alarm, while a service that quietly
// stopped updating does not go unnoticed.
const staleAfter = 5 * source.DefaultInterval

// Styles are adaptive so the dashboard stays readable on both light and dark
// terminals rather than assuming a dark background.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	headingStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "27", Dark: "39"})
	labelStyle    = lipgloss.NewStyle().Faint(true)
	ruleStyle     = lipgloss.NewStyle().Faint(true)
	staleStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	footerStyle   = lipgloss.NewStyle().Faint(true)
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "42"})
	carefulStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "27", Dark: "45"})
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
	criticalStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
)

// View renders the dashboard.
//
// The body is clipped to the window and scrolled rather than left to overflow,
// because a terminal shorter than the content would otherwise drop the GPU and
// NPU panels off the bottom without saying so.
func (m Model) View() string {
	width := m.renderWidth()
	lines := m.bodyLines(width)
	rows := m.visibleRows()
	visible, offset := clip(lines, rows, m.offset)

	var b strings.Builder

	b.WriteString(strings.Join(visible, "\n"))
	b.WriteString("\n")
	b.WriteString(ruleStyle.Render(strings.Repeat("─", width)))
	b.WriteString("\n")
	b.WriteString(fitWidth(footerStyle.Render(footerHint(len(lines), rows, offset)), width))

	return b.String()
}

// renderWidth is the width to lay out at. Once Bubble Tea reports the terminal
// size, the dashboard honours it instead of rendering hidden columns beyond
// the right edge.
func (m Model) renderWidth() int {
	if m.width <= 0 {
		return defaultWidth
	}

	return m.width
}

// visibleRows is how many body lines fit above the footer, or 0 when the
// window size is unknown and everything should simply be rendered.
func (m Model) visibleRows() int {
	// The closing rule and the footer line are reserved.
	const reserved = 2

	if m.height <= reserved {
		return 0
	}

	return m.height - reserved
}

// bodyLines renders everything above the closing rule.
func (m Model) bodyLines(width int) []string {
	var b strings.Builder

	b.WriteString(m.header(width))
	b.WriteString("\n")
	if platform := m.platformLine(); platform != "" {
		b.WriteString(platform)
		b.WriteString("\n")
	}
	b.WriteString(m.tabLine())
	b.WriteString("\n")
	b.WriteString(ruleStyle.Render(strings.Repeat("─", width)))
	b.WriteString("\n\n")

	if m.activeTab == TrendsTab {
		b.WriteString(m.trendsGrid(width))
	} else {
		b.WriteString(m.panelGrid(width))
	}

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i := range lines {
		lines[i] = fitWidth(lines[i], width)
	}

	return lines
}

// panelGrid packs independent panels into as many columns as can fit without
// squeezing their tables. The source order remains the reading order: left to
// right, then top to bottom.
func (m Model) panelGrid(width int) string {
	var panels []string
	if len(m.alerts.Active()) > 0 {
		panels = append(panels, m.alertSection())
	}
	panels = append(panels, m.cpuSection(), m.memorySection())
	for _, gpu := range m.dash.GPUs {
		panels = append(panels, m.gpuSection(gpu))
	}

	// A machine without an NPU publishes no npu_ series at all, and a panel
	// of dashes would suggest a broken sensor rather than absent hardware.
	if m.dash.NPU.Present {
		panels = append(panels, m.npuSection())
	}

	// Processes go last because the hardware panels are what the dashboard
	// is for, but on a wide terminal they can use otherwise idle space.
	if len(m.dash.Processes) > 0 {
		panels = append(panels, m.processSection())
	}

	return packPanels(panels, width, panelMinWidth, 3)
}

func packPanels(panels []string, width, minWidth, maxColumns int) string {
	columns := (width + panelGap) / (minWidth + panelGap)
	if columns < 1 {
		columns = 1
	}
	if columns > maxColumns {
		columns = maxColumns
	}
	if columns == 1 {
		for i := range panels {
			panels[i] = strings.TrimRight(panels[i], "\n")
		}

		return strings.Join(panels, "\n")
	}

	columnWidth := (width - panelGap*(columns-1)) / columns
	rows := make([]string, 0, (len(panels)+columns-1)/columns)
	for start := 0; start < len(panels); start += columns {
		end := start + columns
		if end > len(panels) {
			end = len(panels)
		}

		row := make([]string, 0, end-start)
		for _, content := range panels[start:end] {
			content = strings.TrimRight(content, "\n")
			row = append(row, lipgloss.NewStyle().
				Width(columnWidth).
				MaxWidth(columnWidth).
				Render(content))
		}

		rows = append(rows, lipgloss.JoinHorizontal(
			lipgloss.Top,
			joinWithGap(row, panelGap)...,
		))
	}

	return strings.Join(rows, "\n\n")
}

// joinWithGap inserts fixed-width separators between columns.
func joinWithGap(columns []string, gap int) []string {
	if len(columns) < 2 {
		return columns
	}

	out := make([]string, 0, len(columns)*2-1)
	for i, column := range columns {
		if i > 0 {
			out = append(out, strings.Repeat(" ", gap))
		}
		out = append(out, column)
	}

	return out
}

// fitWidth keeps every rendered line inside the terminal. Lip Gloss accounts
// for escape sequences and wide runes, unlike slicing the rendered string.
func fitWidth(line string, width int) string {
	if width <= 0 || lipgloss.Width(line) <= width {
		return line
	}

	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}

// clip selects the visible window of lines and reports the offset actually
// used, which may be lower than requested if the content shrank.
func clip(lines []string, rows, offset int) ([]string, int) {
	if rows <= 0 || len(lines) <= rows {
		return lines, 0
	}

	if max := len(lines) - rows; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}

	return lines[offset : offset+rows], offset
}

// footerHint spells out the keys, and says where the view is when the content
// does not fit on screen.
// footerHint names the keys that do something here.
//
// Paging is listed alongside the arrows because the dashboard is routinely
// twice the height of the window, and a reader who only learns about single
// line scrolling will scroll a screenful one line at a time. The short form is
// used when everything already fits, where scrolling keys would be noise.
func footerHint(total, rows, offset int) string {
	if rows <= 0 || total <= rows {
		return "1 overview · 2 trends · tab switch · q quit"
	}

	return fmt.Sprintf("1 overview · 2 trends · tab switch · q quit · ↑↓ pgup pgdn g G scroll · lines %d-%d of %d",
		offset+1, offset+rows, total)
}

func (m Model) tabLine() string {
	overview := "1 Overview"
	trends := "2 Trends"
	if m.activeTab == OverviewTab {
		overview = titleStyle.Render("[" + overview + "]")
		trends = labelStyle.Render(trends)
	} else {
		overview = labelStyle.Render(overview)
		trends = titleStyle.Render("[" + trends + "]")
	}

	return overview + "   " + trends
}

// platformLine names the machine under the title, and is omitted entirely on a
// host that publishes nothing about itself rather than showing a row of
// placeholders.
func (m Model) platformLine() string {
	platform := m.dash.Platform
	if platform.Model == "" {
		return ""
	}

	// Everything here arrives from the endpoint, so it is bounded and
	// escaped like any other remote string.
	line := sanitize(platform.Model, 64)
	if platform.LogicalCPUs.OK {
		line += fmt.Sprintf("   %d threads", int(platform.LogicalCPUs.Value))
	}
	if platform.Kernel != "" {
		line += "   kernel " + sanitize(platform.Kernel, 32)
	}

	return labelStyle.Render(line)
}

// header renders the title line and the freshness indicator.
func (m Model) header(width int) string {
	title := "metrics-manager"
	if host := m.dash.Host; host != "" {
		// The host label comes from the endpoint, so it is treated as
		// untrusted text like any other remote string.
		title += " · " + sanitize(host, 40)
	}
	left := titleStyle.Render(title)

	right := m.status(width)
	if lipgloss.Width(left)+1+lipgloss.Width(right) > width {
		if lipgloss.Width(left) <= width {
			return left
		}

		return fitWidth(left, width)
	}

	// Push the status to the right margin, accounting for the fact that
	// styled strings carry escape sequences that do not occupy columns.
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

// status describes how current the displayed numbers are.
func (m Model) status(width int) string {
	// Leave room for the title; a long error is truncated rather than
	// wrapped, which would break the single-line header.
	budget := width / 2

	if m.err != nil {
		text := "no contact: " + m.err.Error()
		if !m.updatedAt.IsZero() {
			text += " · last " + formatAge(m.now().Sub(m.updatedAt))
		}

		return errorStyle.Render(sanitize(text, budget))
	}

	if m.updatedAt.IsZero() {
		return labelStyle.Render("connecting…")
	}

	age := m.now().Sub(m.updatedAt)
	text := "updated " + formatAge(age)

	// Past a few refresh intervals the numbers are old enough that the
	// operator should be told, even though nothing has failed outright.
	if age > staleAfter {
		return staleStyle.Render(text)
	}

	return labelStyle.Render(text)
}

func (m Model) cpuSection() string {
	var b strings.Builder

	b.WriteString(headingStyle.Render("CPU"))
	b.WriteString("\n")

	cpu := m.dash.CPU
	total := totalCPUUsage(cpu)
	b.WriteString(field("usage", fmt.Sprintf("total %s   user %s   system %s   idle %s",
		m.thresholdPercent("cpu.totalPercent", total),
		formatPercent(cpu.UsageUser),
		formatPercent(cpu.UsageSystem),
		formatPercent(cpu.UsageIdle))))
	b.WriteString(field("frequency", formatFrequencyKHz(cpu.FrequencyKHz)))
	b.WriteString(field("package temp", m.thresholdTemperature("cpu.temperatureC", cpu.PackageTempC)))
	b.WriteString(field("package power", powerAgainstLimit(cpu.PackagePowerW, cpu.TDPW)))
	b.WriteString(field("freq limits", frequencyLimits(cpu)))

	if len(cpu.CStates) > 0 {
		b.WriteString(field("idle states", cStateSummary(cpu.CStates)))
	}

	if len(cpu.Classes) > 0 {
		b.WriteString("\n")
		b.WriteString(coreClassTable(cpu.Classes))
	}

	return b.String()
}

// powerAgainstLimit shows draw next to the thermal design power, because a
// wattage means little without the budget it is being spent from.
func powerAgainstLimit(power, tdp Reading) string {
	if !tdp.OK {
		return formatWatts(power)
	}

	return fmt.Sprintf("%s of %s TDP", formatWatts(power), formatWatts(tdp))
}

// frequencyLimits reports the base clock and the single-core turbo ceilings.
//
// On a hybrid part the two ceilings belong to the two core kinds, so they are
// labelled with the class names used in the table below rather than with
// powerstat's "primary" and "secondary".
func frequencyLimits(cpu CPU) string {
	parts := []string{"base " + formatFrequencyMHz(cpu.BaseFreqMHz)}

	if cpu.TurboPrimaryMHz.OK {
		parts = append(parts, "turbo P "+formatFrequencyMHz(cpu.TurboPrimaryMHz))
	}
	if cpu.TurboSecondaryMHz.OK {
		parts = append(parts, "turbo E "+formatFrequencyMHz(cpu.TurboSecondaryMHz))
	}
	if cpu.UncoreMHz.OK {
		parts = append(parts, "uncore "+formatFrequencyMHz(cpu.UncoreMHz))
	}

	return strings.Join(parts, "   ")
}

// cStateSummary renders idle-state residency on one line.
func cStateSummary(states []CState) string {
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, strings.ToUpper(state.Name)+" "+formatPercent(state.Residency))
	}

	return strings.Join(parts, "   ")
}

// coreClassColumns fixes the layout of the per-class table. Padding is applied
// to the plain text before any styling, because escape sequences would
// otherwise be counted as columns.
var coreClassColumns = []struct {
	title string
	width int
}{
	{"class", 5},
	{"cores", 6},
	{"frequency", 10},
	{"user", 8},
	{"system", 8},
	{"idle", 8},
	{"source", 10},
}

func coreClassTable(classes []CoreClass) string {
	var b strings.Builder

	var header strings.Builder
	header.WriteString("  ")
	for i, c := range coreClassColumns {
		if i == 0 {
			header.WriteString(fmt.Sprintf("%-*s", c.width, c.title))
			continue
		}
		header.WriteString(fmt.Sprintf("%*s", c.width, c.title))
	}
	b.WriteString(labelStyle.Render(header.String()))
	b.WriteString("\n")

	for _, class := range classes {
		cells := []string{
			formatCount(class.Cores),
			formatFrequencyKHz(class.FrequencyKHz),
			formatPercent(class.UsageUser),
			formatPercent(class.UsageSystem),
			formatPercent(class.UsageIdle),
			class.Source,
		}

		var row strings.Builder
		row.WriteString("  ")
		row.WriteString(fmt.Sprintf("%-*s", coreClassColumns[0].width, class.Name))
		for i, cell := range cells {
			width := coreClassColumns[i+1].width
			// The source column is a word, not a measurement, so it
			// reads better left-aligned against the numbers.
			if i == len(cells)-1 {
				row.WriteString("  " + cell)
				continue
			}
			row.WriteString(fmt.Sprintf("%*s", width, cell))
		}

		b.WriteString(row.String())
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) memorySection() string {
	var b strings.Builder

	b.WriteString(headingStyle.Render("Memory"))
	b.WriteString("\n")

	mem := m.dash.Memory
	used := m.thresholdPercent("memory.usedPercent", mem.UsedPercent)
	if mem.UsedBytes.OK && mem.TotalBytes.OK {
		used += fmt.Sprintf("   (%s of %s)", formatBytes(mem.UsedBytes), formatBytes(mem.TotalBytes))
	}

	b.WriteString(field("used", used))
	b.WriteString(field("available", formatPercent(mem.AvailablePercent)))

	// Bandwidth is omitted rather than dashed out when the collector is not
	// running, because on many machines it simply cannot be read.
	if mem.TotalMiBps.OK {
		b.WriteString(field("bandwidth", fmt.Sprintf("%s   read %s   write %s",
			formatBandwidthMiBps(mem.TotalMiBps),
			formatBandwidthMiBps(mem.ReadMiBps),
			formatBandwidthMiBps(mem.WriteMiBps))))
	}

	return b.String()
}

// fieldLabelWidth aligns the value column across every section.
const fieldLabelWidth = 14

func (m Model) gpuSection(g GPU) string {
	var b strings.Builder

	heading := "GPU"
	if g.ID != "" {
		// The id is a label from the endpoint, so it is sanitised like
		// any other remote string.
		heading += " " + sanitize(g.ID, 8)
	}
	// Naming the driver is what makes an unavailable counter explicable
	// rather than looking like a fault.
	if g.Driver != "" {
		heading += " · " + sanitize(g.Driver, 12)
	}
	b.WriteString(headingStyle.Render(heading))
	b.WriteString("\n")

	b.WriteString(field("power", fmt.Sprintf("graphics %s   package %s",
		formatWatts(g.PowerW), formatWatts(g.PackagePowerW))))
	b.WriteString(field("temperature", m.thresholdTemperature("gpu.temperatureC", g.TempC)))
	b.WriteString(field("shared mem", memoryPair(g.SharedUsedBytes, g.SharedTotalBytes)))

	// An integrated GPU reports a zero VRAM total rather than omitting the
	// field, and a "0 B of 0 B" row says nothing worth a line.
	if g.VRAMTotalBytes.OK && g.VRAMTotalBytes.Value > 0 {
		b.WriteString(field("vram", memoryPair(g.VRAMUsedBytes, g.VRAMTotalBytes)))
	}

	if len(g.Engines) > 0 {
		b.WriteString("\n")
		b.WriteString(m.engineTable(g.Engines))
	}
	if len(g.Tiles) > 0 {
		b.WriteString("\n")
		b.WriteString(tileTable(g.Tiles))
	}
	if len(g.Processes) > 0 {
		b.WriteString("\n")
		b.WriteString(gpuProcessTable(g.Processes))
	}

	return b.String()
}

// gpuProcessColumns fixes the layout of the per-process table.
var gpuProcessColumns = []column{
	{title: "pid", width: 8, left: true},
	{title: "process", width: 18, left: true},
	{title: "usage", width: 9},
	{title: "memory", width: 12},
}

func gpuProcessTable(processes []GPUProcess) string {
	rows := make([][]string, 0, len(processes))
	for _, p := range processes {
		rows = append(rows, []string{
			// Both come from the kernel by way of qmassa, so they are
			// bounded and escaped like every other external string.
			sanitize(p.PID, 8),
			sanitize(p.Command, 18),
			formatPercent(p.UsagePct),
			formatBytes(p.MemoryBytes),
		})
	}

	return table(gpuProcessColumns, rows)
}

// npuFrequency shows the current clock against the driver's ceiling, because
// an idle NPU reports zero and that reads very differently next to the number
// it could reach.
func npuFrequency(npu NPU) string {
	current := formatFrequencyHz(npu.FrequencyHz)
	if !npu.MaxFrequencyMHz.OK {
		return current
	}

	return current + " of " + formatFrequencyMHz(npu.MaxFrequencyMHz)
}

// processColumns fixes the layout of the process table.
var processColumns = []column{
	{title: "pid", width: 8, left: true},
	{title: "process", width: 20, left: true},
	{title: "cpu", width: 9},
	{title: "memory", width: 12},
}

func (m Model) processSection() string {
	var b strings.Builder

	b.WriteString(headingStyle.Render("Processes"))
	b.WriteString("\n")

	limit := m.config.Processes.MaxDisplayed
	processes := m.dash.Processes
	if len(processes) > limit {
		processes = processes[:limit]
	}
	rows := make([][]string, 0, len(processes))
	for _, p := range processes {
		rows = append(rows, []string{
			// A process names itself, so both of these are attacker
			// controlled and are bounded and escaped like every other
			// external string that reaches the terminal.
			sanitize(p.PID, 8),
			sanitize(p.Command, 20),
			m.thresholdPercent("process.cpuPercent", p.CPUPercent),
			formatBytes(p.MemoryBytes),
		})
	}

	b.WriteString(table(processColumns, rows))

	return b.String()
}

// memoryPair renders "used of total", still saying what it can when only one
// of the two is reported.
func memoryPair(used, total Reading) string {
	if total.OK {
		return fmt.Sprintf("%s of %s", formatBytes(used), formatBytes(total))
	}

	return formatBytes(used)
}

var engineColumns = []column{
	// Wide enough for the longest name either driver uses,
	// "video-enhance", so the table never truncates an engine.
	{"engine", 16, true},
	{"usage", 9, false},
}

func (m Model) engineTable(engines []GPUEngine) string {
	rows := make([][]string, 0, len(engines))
	for _, e := range engines {
		rows = append(rows, []string{
			sanitize(e.Name, engineColumns[0].width),
			m.thresholdPercent("gpu.utilizationPercent", e.Usage),
		})
	}

	return table(engineColumns, rows)
}

var tileColumns = []column{
	{"tile", 6, true},
	{"actual", 9, false},
	{"requested", 11, false},
	{"min", 10, false},
	{"max", 10, false},
	{"throttle", 12, true},
}

func tileTable(tiles []GPUTile) string {
	rows := make([][]string, 0, len(tiles))
	for _, t := range tiles {
		rows = append(rows, []string{
			sanitize(t.Name, tileColumns[0].width),
			formatFrequencyMHz(t.ActualMHz),
			formatFrequencyMHz(t.RequestedMHz),
			formatFrequencyMHz(t.MinMHz),
			formatFrequencyMHz(t.MaxMHz),
			throttleText(t),
		})
	}

	return table(tileColumns, rows)
}

// throttleText summarises a tile's throttle state.
//
// "no throttle series at all" and "nothing is throttling" are deliberately not
// collapsed: reporting the first as the second would be reassurance the data
// does not support.
func throttleText(t GPUTile) string {
	switch {
	case !t.ThrottleReported:
		return absent
	case len(t.ThrottleReasons) > 0:
		return strings.Join(t.ThrottleReasons, ",")
	case t.Throttled.OK && t.Throttled.Value != 0:
		// The summary flag is set while every named reason is clear, so
		// the cause is unknown rather than absent.
		return "yes"
	default:
		return "none"
	}
}

func (m Model) npuSection() string {
	var b strings.Builder

	b.WriteString(headingStyle.Render("NPU"))
	b.WriteString("\n")

	npu := m.dash.NPU
	b.WriteString(field("utilization", m.thresholdPercent("npu.utilizationPercent", npu.Utilization)))
	b.WriteString(field("frequency", npuFrequency(npu)))
	b.WriteString(field("power", formatWatts(npu.PowerW)))
	b.WriteString(field("temperature", m.thresholdTemperature("npu.temperatureC", npu.TempC)))
	b.WriteString(field("bandwidth", formatBandwidth(npu.BandwidthMBps)))
	b.WriteString(field("memory", formatMegabytes(npu.MemoryMB)))
	b.WriteString(field("tile config", formatCount(npu.TileConfig)))

	return b.String()
}

// column describes one table column. Padding is applied to the plain text
// before any styling, because escape sequences would otherwise be counted as
// columns.
type column struct {
	title string
	width int

	// left aligns the cell to the left, which suits names and words;
	// measurements read better right-aligned under each other.
	left bool
}

func table(columns []column, rows [][]string) string {
	var b strings.Builder

	b.WriteString(labelStyle.Render(tableRow(columns, titles(columns))))
	b.WriteString("\n")

	for _, cells := range rows {
		b.WriteString(tableRow(columns, cells))
		b.WriteString("\n")
	}

	return b.String()
}

func titles(columns []column) []string {
	out := make([]string, 0, len(columns))
	for _, c := range columns {
		out = append(out, c.title)
	}

	return out
}

func tableRow(columns []column, cells []string) string {
	var b strings.Builder

	b.WriteString("  ")
	for i, c := range columns {
		if i >= len(cells) {
			break
		}
		if c.left {
			// A right-aligned cell can fill its width exactly, so a
			// left-aligned column following one needs a separator of
			// its own or the two run together.
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(fmt.Sprintf("%-*s", c.width, cells[i]))
			continue
		}
		b.WriteString(fmt.Sprintf("%*s", c.width, cells[i]))
	}

	return strings.TrimRight(b.String(), " ")
}

// field renders one labelled value line.
func field(label, value string) string {
	padded := fmt.Sprintf("  %-*s", fieldLabelWidth, label)

	return labelStyle.Render(padded) + value + "\n"
}

func (m Model) thresholdPercent(metric string, value Reading) string {
	return m.thresholdValue(metric, value, formatPercent)
}

func (m Model) thresholdTemperature(metric string, value Reading) string {
	return m.thresholdValue(metric, value, formatTemperature)
}

func (m Model) thresholdValue(metric string, value Reading, format func(Reading) string) string {
	text := format(value)
	threshold, enabled := m.config.Thresholds[metric]
	if !enabled || !value.OK || math.IsNaN(value.Value) || math.IsInf(value.Value, 0) {
		return text
	}

	return severityStyle(classify(value.Value, threshold)).Render(text)
}

func severityStyle(severity Severity) lipgloss.Style {
	switch severity {
	case SeverityCareful:
		return carefulStyle
	case SeverityWarning:
		return warningStyle
	case SeverityCritical:
		return criticalStyle
	default:
		return okStyle
	}
}

func (m Model) alertSection() string {
	var b strings.Builder
	alerts := m.alerts.Active()
	b.WriteString(criticalStyle.Render(fmt.Sprintf("Warnings (%d)", len(alerts))))
	b.WriteString("\n")
	for _, alert := range alerts {
		label := alert.Metric
		if alert.Device != "" {
			label = alert.Device + " · " + label
		}
		line := fmt.Sprintf("%-8s %s: current %.1f · threshold %.1f", alert.Severity, label,
			alert.Value, alert.Threshold)
		b.WriteString(severityStyle(alert.Severity).Render(line))
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("  " + alert.Message))
		b.WriteString("\n")
	}

	return b.String()
}
