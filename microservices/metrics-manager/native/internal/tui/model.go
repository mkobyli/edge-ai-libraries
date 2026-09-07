// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package tui renders the metrics-manager telemetry as a terminal dashboard.
//
// It consumes the same Prometheus exposition that the service's own
// /metrics/stream polls, so the two views cannot report different numbers for
// the same host.
package tui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/open-edge-platform/edge-ai-libraries/microservices/metrics-manager/native/internal/source"
)

// snapshotMsg delivers one poll result into the update loop.
type snapshotMsg source.Snapshot

// Model is the dashboard state.
type Model struct {
	snapshots <-chan source.Snapshot

	// dash is the most recent successfully parsed snapshot. It is kept
	// across failures so a brief loss of contact leaves the last known
	// numbers on screen, marked stale, rather than blanking the display.
	dash Dashboard

	// updatedAt is when dash was collected, zero until the first success.
	updatedAt time.Time

	// err is the reason the most recent poll failed, or nil.
	err error

	width  int
	height int

	// offset is the first body line shown, non-zero only when the content
	// is taller than the window.
	offset int

	// now is injected so the staleness readout can be tested without
	// depending on the wall clock.
	now func() time.Time
}

// NewModel returns a Model that renders snapshots as they arrive on ch.
func NewModel(ch <-chan source.Snapshot) Model {
	return Model{snapshots: ch, now: time.Now}
}

// Init starts waiting for the first snapshot.
func (m Model) Init() tea.Cmd {
	return waitForSnapshot(m.snapshots)
}

// Update folds one message into the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.offset = m.scrollTo(m.offset - 1)
		case "down", "j":
			m.offset = m.scrollTo(m.offset + 1)
		case "pgup":
			m.offset = m.scrollTo(m.offset - m.visibleRows())
		case "pgdown", " ":
			m.offset = m.scrollTo(m.offset + m.visibleRows())
		case "home", "g":
			m.offset = 0
		case "end", "G":
			m.offset = m.maxOffset()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// A window that just grew can leave the view scrolled past the
		// end, which would show a screen of blank lines.
		m.offset = m.scrollTo(m.offset)

	case snapshotMsg:
		m.err = msg.Err
		if msg.Err == nil {
			m.dash = BuildDashboard(msg.Samples)
			m.updatedAt = msg.At
		}
		// Re-arm immediately: the poller paces itself, so the update
		// loop should never be the thing that throttles refreshes.
		return m, waitForSnapshot(m.snapshots)

	case streamClosedMsg:
		// The poller stopped, which only happens when its context was
		// cancelled, so there is nothing further to display.
		return m, tea.Quit
	}

	return m, nil
}

// streamClosedMsg reports that the snapshot channel was closed.
type streamClosedMsg struct{}

// scrollTo clamps a requested scroll position to the content.
func (m Model) scrollTo(offset int) int {
	if offset < 0 {
		return 0
	}
	if max := m.maxOffset(); offset > max {
		return max
	}

	return offset
}

// maxOffset is the furthest the body can scroll, which is zero whenever it
// already fits on screen.
func (m Model) maxOffset() int {
	rows := m.visibleRows()
	if rows <= 0 {
		return 0
	}

	if overflow := len(m.bodyLines(m.renderWidth())) - rows; overflow > 0 {
		return overflow
	}

	return 0
}

// waitForSnapshot blocks in a command goroutine until the next snapshot.
func waitForSnapshot(ch <-chan source.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}

		return snapshotMsg(s)
	}
}

// Run drives a Bubble Tea program that renders the endpoint until the operator
// quits or ctx is cancelled.
func Run(ctx context.Context, poller *source.Poller) error {
	// The caller's context is kept so shutdownErr can tell an operator's
	// signal apart from the cancel below, which always fires.
	parent := ctx

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// A single slot is enough: the display only ever wants the newest
	// snapshot, and buffering more would only let it fall behind.
	ch := make(chan source.Snapshot, 1)

	polled := make(chan struct{})
	go func() {
		defer close(polled)
		defer close(ch)
		poller.Run(ctx, func(s source.Snapshot) { send(ch, s) })
	}()

	program := tea.NewProgram(NewModel(ch), tea.WithContext(ctx), tea.WithAltScreen())
	_, err := program.Run()

	// Stop the poller and wait for it, so the process does not exit while
	// a request is still in flight.
	cancel()
	<-polled

	return shutdownErr(err, parent)
}

// shutdownErr suppresses the error Bubble Tea reports when it was stopped by a
// cancelled context.
//
// A signal reaches the program by cancelling that context, so treating the
// resulting ErrProgramKilled as a failure would make every ordinary SIGTERM
// exit non-zero and have a service manager record a clean stop as a crash.
func shutdownErr(err error, parent context.Context) error {
	if errors.Is(err, tea.ErrProgramKilled) && parent.Err() != nil {
		return nil
	}

	return err
}

// RenderOnce renders a single frame for one snapshot and returns it.
//
// It exists so the dashboard can be inspected without a terminal: a parity
// check against the REST output, or a quick look on a host where running an
// interactive program is inconvenient.
func RenderOnce(s source.Snapshot, width int) string {
	m := NewModel(nil)
	m.width = width

	next, _ := m.Update(snapshotMsg(s))

	return next.View()
}

// send delivers s to ch, discarding a snapshot that is still queued.
//
// Dropping is correct here: an unread snapshot is already out of date, and
// blocking instead would stall the poller behind a slow terminal.
func send(ch chan source.Snapshot, s source.Snapshot) {
	for {
		select {
		case ch <- s:
			return
		default:
			select {
			case <-ch:
			default:
			}
		}
	}
}
