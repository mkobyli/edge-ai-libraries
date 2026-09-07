// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

module github.com/open-edge-platform/edge-ai-libraries/microservices/metrics-manager/native

// The build toolchain image is pinned in ../versions.env (GO_IMAGE).
//
// The language version below is not a free choice. It used to sit at 1.23 so
// the module also built with a distribution-provided Go; adding the TUI ended
// that. bubbletea raised the floor to 1.24, and golang.org/x/sys v0.44.0 --
// taken to clear GO-2026-5024 -- raised it to 1.25. Ubuntu 24.04 packages Go
// 1.22, so a distribution toolchain no longer suffices: build with the pinned
// image.
go 1.25.0

require (
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	golang.org/x/sys v0.44.0
)

require (
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/ansi v0.10.1 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13-0.20250311204145-2c3ea96c31dd // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/text v0.3.8 // indirect
)
