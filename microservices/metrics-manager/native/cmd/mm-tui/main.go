// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Command mm-tui renders Metrics Manager telemetry as a terminal dashboard.
//
// It reads the Prometheus exposition that Telegraf already serves, which is
// the same source the service's own /metrics/stream polls. Sharing that one
// source is deliberate: the terminal view and the REST interface cannot report
// different numbers for the same host, because there is only one collection
// path behind both.
//
//	mm-tui
//	mm-tui -endpoint http://127.0.0.1:9273/metrics
//
// The endpoint is read-only and no privileges are required beyond reaching it.
// A host with no Intel GPU or NPU simply shows those measurements as absent.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/open-edge-platform/edge-ai-libraries/microservices/metrics-manager/native/internal/source"
	"github.com/open-edge-platform/edge-ai-libraries/microservices/metrics-manager/native/internal/tui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

// defaultEndpoint is Telegraf's outputs.prometheus_client listener as
// configured by this component. Loopback is the default because the dashboard
// is a local diagnostic tool; pointing it at another host is possible but has
// to be asked for explicitly.
const defaultEndpoint = "http://127.0.0.1:9273/metrics"

func main() {
	endpoint := flag.String("endpoint", defaultEndpoint,
		"Prometheus exposition endpoint to read")
	interval := flag.Duration("interval", source.DefaultInterval,
		"how often to refresh; must be greater than zero")
	once := flag.Bool("once", false,
		"render a single frame to stdout and exit, for debugging and parity checks")
	width := flag.Int("width", 100,
		"column width to render at with -once")
	configPath := flag.String("config", tui.DefaultConfigPath,
		"dashboard configuration file; built-in defaults are used when absent")
	chartsConfigPath := flag.String("charts-config", tui.DefaultChartsConfigPath,
		"trend chart configuration file; built-in defaults are used when absent")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("mm-tui", version)
		return
	}

	config, err := tui.LoadDashboardConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mm-tui:", err)
		os.Exit(1)
	}
	chartsConfig, err := tui.LoadChartsConfig(*chartsConfigPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mm-tui:", err)
		os.Exit(1)
	}

	if *once {
		if err := renderOnce(*endpoint, *width, config, chartsConfig); err != nil {
			fmt.Fprintln(os.Stderr, "mm-tui:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*endpoint, *interval, config, chartsConfig); err != nil {
		fmt.Fprintln(os.Stderr, "mm-tui:", err)
		os.Exit(1)
	}
}

// renderOnce takes one reading and prints the frame it would display.
func renderOnce(
	endpoint string,
	width int,
	config tui.DashboardConfig,
	chartsConfig tui.ChartsConfig,
) error {
	poller, err := source.New(endpoint)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), source.DefaultTimeout)
	defer cancel()

	samples, err := poller.Poll(ctx)
	if err != nil {
		return err
	}

	fmt.Println(tui.RenderOnceWithConfigs(
		source.Snapshot{At: time.Now(), Samples: samples}, width, config, chartsConfig,
	))

	return nil
}

func run(
	endpoint string,
	interval time.Duration,
	config tui.DashboardConfig,
	chartsConfig tui.ChartsConfig,
) error {
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero, got %v", interval)
	}

	poller, err := source.New(endpoint)
	if err != nil {
		return err
	}
	poller.Interval = interval

	// Bubble Tea restores the terminal on its own way out, so the signal
	// handler only has to cancel the context and let it unwind.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return tui.RunWithConfigs(ctx, poller, config, chartsConfig)
}
