<!--
SPDX-FileCopyrightText: (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# Release Notes: Metrics Manager

## Unreleased

This upcoming release improves the **responsive native TUI**, introduces
**explicit base-unit telemetry** and **optional Docker collectors with host-NIC
networking**, and includes runtime fixes and documentation improvements.

Target version: **2026.3.0**. This section is not a publication announcement.
Historical release entries and Helm metadata remain unchanged.

**New**

- **Host-NIC deployment**: adds the opt-in `host-nic-telemetry` Compose profile,
  sharing the default native image and collectors. Its network collector
  defaults on; an explicit false setting takes precedence. Listener ports,
  relay/scrape URLs and healthchecks follow the configured host ports. Bridge
  networking remains the default. Select one service explicitly: enabling a
  profile does not stop the default service, and host networking exposes
  listeners directly on host interfaces.
- **Optional Docker collectors**: adds explicit collector flags while keeping
  additions disabled in the default bridge deployment. Mounted Telegraf
  drop-ins are loaded from a private startup snapshot; review previously
  inactive files before upgrading. Conflicting automatic/custom inputs fail
  startup instead of duplicating measurements. Optional Turbostat has a bounded
  executable/MSR preflight and is skipped with a warning when unavailable.
  Recreate containers to apply configuration changes.

**Improved**

- **Native TUI**: provides responsive Overview, Trends, Details and Alerts
  views, configurable thresholds and process limits, and five-minute chart
  history by default. Network and storage views distinguish unavailable or
  stale readings from measured zeros.
- **Native metric units**: migrates CPU/GPU clocks, GPU device/client memory
  and DRAM throughput to explicit bytes, bytes/second, Hz, W and Celsius fields,
  without old-name aliases. NPU retains `npu_power`, `npu_frequency`,
  `npu_temperature`, `npu_bandwidth`, `npu_memory_mb` and `npu_frequency_max_mhz`
  with W, actual Hz, Celsius, decimal MB/s, binary MiB and MHz respectively.
  DRAM display/charts use
  decimal GB/s. Standard Telegraf fields, percentage semantics and existing
  alert thresholds are unchanged. Upgrade collectors, TUI and custom queries
  together using the [breaking-change migration guide](./get-started/native-metric-migration.md),
  also included in the bare-metal package.
- **Plugin-first delivery**: uses the shared, SHA-256-verified official
  Telegraf 1.40.3 archive and native Go collector implementation across Docker
  and the bare-metal package. The digest-pinned native Go toolchain is 1.27.3.
  The official Telegraf binary also embeds Go 1.27.3; it is not rebuilt locally.
- **Python dependencies**: aligns runtime and development dependencies with
  upstream, including FastAPI 0.143.1 and Uvicorn 0.54.0. OpenVINO remains an
  optional build extra. Resolution follows the existing manifest-based build;
  this change does not introduce a Python lockfile.
- **Security updates**: uses a shared, digest-pinned Debian 13 Python base and
  applies available Debian package updates. Build proxy values are no longer
  retained in image history; explicit runtime proxy configuration remains
  supported. Available updates do not guarantee that remaining Debian
  vulnerabilities have fixes. See the [release validation gate](./get-started/testing.md#release-validation-scope).
- **RC identity**: separates the default `2026.3.0-rc1` and extended
  `2026.3.0-extended-rc1` images. API/log identity and native binary versions
  follow the build tag, while the project and Debian package stay at 2026.3.0.
  A runtime service-version override affects API/log identity only.

**Fixed**

- **Native NPU measurements**: corrects PLL decoding for MTL/ARL/LNL/PTL and
  bandwidth rates using actual elapsed time with single-wrap correction.
  **Breaking unit correction:** the unchanged `npu_frequency` contains real Hz, not the
  legacy MHz-scale number. Divide by `1e6` in queries that need MHz. Unsupported
  utilization/memory readings are omitted instead of publishing artificial
  zero/negative values. Presence-based memory detection, busy-counter reset
  handling, power state and the frequency ceiling are retained.
- **NPU diagnostics and recovery**: invalid intervals drop the sample without
  changing counter baselines. Every busy-time or memory read/parse failure is
  reported to stderr; a missing busy-time attribute is reported once at startup,
  while a missing memory node remains silent. Stdout stays Line Protocol only.
  TUI tests cover measured zero, unavailable data and recovery. ViPPET UI and
  benchmark ingestion use the planned NPU names with explicit display conversions;
  report column units remain unchanged.
- **Stale NPU series**: container, bare-metal and ViPPET Prometheus exports
  expire inactive series after 15 seconds. Other fields do not refresh omitted
  fields' timestamps; SSE forwards the reduced snapshot after expiration.
  Allow flush/poll latency. Expiration applies to all series; custom inputs
  slower than this window need a longer `expiration_interval`.
- **OTLP histogram ingestion**: stores count and available finite summary
  statistics, aggregation temporality and typed attributes; parses integer
  strings without float conversion. Invalid numeric points are reported and
  skipped independently. Histogram buckets are not stored. Tagged series remain
  distinct through the API and Prometheus export.
- **GPU capability registry**: adds the upstream Xe3/Xe3p platform entries and
  corrects Battlemage, Lunar Lake and Arrow Lake IDs. These are inferred
  capabilities; a listed minimum kernel does not guarantee that a device is
  enabled or supported by the installed runtime.

- **Multisocket memory bandwidth**: honors each IMC PMU's CPU affinity instead
  of assuming a single CPU can access counters on every socket.
- **Asynchronous resource cleanup**: tracks and cancels pending HTTP pushes
  before closing the forwarding session, including exceptional shutdown paths.
  Timestamp handling preserves epoch zero and structured timestamp precision.
- **CORS validation**: rejects malformed JSON arrays and non-string array
  entries rather than silently treating them as another configuration format.
- **Debian package dependencies**: declares the account-management, systemd,
  module-loading and runtime-library prerequisites needed for installation.

---

## Version 2026.2.0

**Release Date:** September 9, 2026

**New:**

- **Platform & Device Capabilities API**: New `GET /api/v1/capabilities` endpoint returns a structured snapshot of the host platform and its compute devices (CPU, GPU, NPU, memory, storage, media codecs). Two detail profiles are available via `?profile=`: `minimal` (default) for a high-level summary and `expanded` for a full technical inventory. Enables workload placement without shelling out to platform-specific tools.
- **Software Capabilities Export**: The capabilities snapshot reports the software stack on each detected compute device (drivers, runtimes, supported precisions, media codecs), enabling capability-aware model and pipeline placement across heterogeneous edge fleets.
- **Hardware Discovery Tooling Bundled**: The runtime image ships `dmidecode`, `pciutils` (`lspci`), `util-linux` (`lsblk`), and `hwinfo` for out-of-the-box capability detection with no extra host packages or sidecar containers.
- **Optional OpenVINO™ Build Variant**: New `openvino` extra in `pyproject.toml` and `INSTALL_OPENVINO` Docker build argument. When enabled, adds `vainfo` and `intel-media-va-driver` for VAAPI codec detection, while keeping the default image lean.

**Improved:**

- **Native Go Telemetry Plugins**: The CPU, Intel® GPU and Intel® NPU collectors are now single static Go binaries (`mm-plugin-cpu`, `mm-plugin-gpu`, `mm-plugin-npu`) in `/usr/libexec/metrics-manager/`, replacing the previous shell and Python readers. Output is unchanged for existing series, so dashboards and Prometheus queries keep working. The same binaries are shipped by the bare-metal package, so a container and a native install report identical metrics from one codebase.
  - **New GPU series**: per-tile frequency and throttle reasons, GPU memory, and GPU temperature. Multi-tile parts such as Meteor Lake now report every graphics tile instead of only the first, so `gpu_frequency` carries an additional `tile` label.
  - **New CPU series**: `cpu_core_class` breaks frequency and utilisation down by P, E and low-power-E cores.
  - **Opt-in per-process GPU usage**: `mm-plugin-gpu -clients` emits a `gpu_client` measurement tagged by PID. It is excluded from the Prometheus endpoint by default, because one series per process is unbounded cardinality.
- **Telegraf 1.39.3**: The official upstream release archive is used and verified against a pinned SHA-256 digest before extraction, so a tampered or truncated download fails the build.
- **Pinned Third-Party Versions**: All third-party component versions live in a single `versions.env`, mirrored into the `Dockerfile` and enforced by tests, so the container and the bare-metal package cannot drift apart.
- **qmassa 2.1.0 and qmmd 0.2.0**: Installed with `cargo install --locked --git … --tag …` from upstream instead of crates.io, preventing build failures from yanked versions and bringing the latest Intel® GPU telemetry improvements.
- **Security Hardening**: Several attack-surface reductions in the production image: `supervisor` installed via `pip` (4.3.0), `perl-base` purged, `curl` removed (healthcheck replaced with a Python one-liner), and supervisord's Unix socket configured with an explicit user/password placeholder.
- **Build and Developer Loop**: Go module and build caches added to the `Dockerfile`, with leaner service startup/shutdown in `Makefile` and `compose.yaml` for shorter `make test` and `docker compose up/down` turnaround.
- **Documentation**: Capabilities endpoint documented in the API reference, updated system requirements, corrected links and release-branch references, improved cross-linking, and an expanded troubleshooting section.
- **Test Coverage**: New tests for the capabilities endpoint and response model, plus additional route and application wiring tests.

---

## Version 2026.1.0

**Release Date:** May 11, 2026

**New:**

- **Server-Sent Events (SSE) Streaming** — New `/metrics/stream` endpoint streams all metrics (system + custom) in real-time via SSE. Each connected client polls the Telegraf Prometheus endpoint independently. Browser requests receive a live HTML table; SSE clients receive raw event stream
  - **Impact**: Dashboards can now consume metrics without polling, enabling real-time visualizations. Replaces the old WebSocket relay model

- **Multiple Input Formats** — Accept custom metrics in four formats without code changes:
  - JSON Batch (`POST /api/v1/metrics`) — multiple metrics with multiple fields
  - Simple JSON (`POST /api/v1/metrics/simple`) — single metric, simplest format
  - InfluxDB Line Protocol (`POST /api/v1/metrics/influx`) — standard Influx format
  - OpenTelemetry (OTLP) (`POST /api/v1/metrics/otlp`) — CNCF standard format
  - **Impact**: Any monitoring system can integrate without client library or format conversion

- **Intel NPU Telemetry** — Bundled `npu_reader.py` script collects metrics from Intel NPU:
  - Power draw (watts)
  - Frequency, temperature, utilization
  - Memory usage, bandwidth, tile configuration
  - Supported on Meteor Lake (MTL), Arrow Lake (ARL), Lunar Lake (LNL), Panther Lake (PTL)
  - **Impact**: Full hardware observability for edge AI applications on Intel platforms

- **Custom Metrics Scripts** — Drop executable scripts into `/app/custom-metrics/` (shell or Python). Telegraf runs them every 10 seconds and publishes metrics automatically
  - **Impact**: No REST API calls needed for custom metrics. Simplest way to add application-specific metrics

- **Docker Compose & Kubernetes** — Production-ready `compose.yaml` and Helm chart included
  - Docker Compose for standalone deployments
  - Helm chart published to OCI registry (`oci://registry-1.docker.io/intel/metrics-manager:2026.1.0-helm`)
  - **Impact**: Deploy in 3 commands on Docker or Kubernetes

- **Comprehensive API Reference** — Health checks, statistics, detailed endpoint documentation
  - Basic health (`GET /health`)
  - Detailed health with store statistics (`GET /api/health`)
  - Service statistics (`GET /api/v1/stats`)
  - **Impact**: Easy to integrate into monitoring systems and dashboards

**Improved:**

- **Debounced Persistence** — Custom metrics are persisted to Telegraf with configurable debounce (default 100ms). Prevents HTTP bottleneck when ingesting high-frequency metrics
  - **Impact**: Support for 1000+ metrics per second without API latency spikes

- **Memory Protection** — Automatic eviction of oldest metrics when in-memory limit is reached (default 100k metrics)
  - **Impact**: Metrics service can not run out of memory even under sustained high ingestion

- **Structured Logging** — JSON-formatted logs by default (with human-readable text option for development)
  - **Impact**: Easy integration with log aggregators (ELK, Datadog, etc.)

- **Rate Limiting** — Per-IP token bucket rate limiting (default 1000 requests/minute per IP)
  - **Impact**: Protection against accidental/malicious floods; health and statistics endpoints exempt

- **Correlation IDs** — Every request gets a correlation ID (auto-generated UUID or from `X-Correlation-ID` header) for distributed tracing
  - **Impact**: Easy to track requests through logs and multiple services

- **GZIP Compression** — Automatic compression for HTTP responses >1 KB
  - **Impact**: Reduced bandwidth for metrics streaming (especially important for SSE)

**Known Issues:**

None at this release. See GitHub issues for feature requests and discussions.

---

## Version 2026.0.1

**Release Date:** May 5, 2026

**Fixed:**

- **SSE Stream HTML UI** — Browser requests to `/metrics/stream` now receive an HTML page with an in-place updated metrics table (previously showed raw SSE stream)
  - **Impact**: Live dashboard experience in browser without client-side framework

- **Content Negotiation** — `/metrics/stream` auto-detects client type:
  - Browser (`Accept: text/html`) → HTML page
  - SSE client (`Accept: text/event-stream`) → raw stream
  - **Impact**: Single endpoint works for both browsers and programmatic clients

---

## Version 2026.0.0

**Release Date:** April 1, 2026

**New:**

- **Initial Release** — Metrics Manager with:
  - Telegraf-based system metrics collection (CPU, RAM, temperature, GPU, NPU)
  - FastAPI REST interface for custom metrics ingestion
  - In-memory metrics store with configurable retention
  - Prometheus-compatible output (`GET /metrics`, `:9273/metrics`)
  - Real-time SSE streaming (`GET /metrics/stream`)

## Versions and Dependencies

### Current Image

- Intel® Metrics Manager **2026.3.0** (unreleased target)
- Telegraf **1.40.3** (official system metrics agent)
- qmassa **2.1.0** (Intel® GPU telemetry via named pipe)
- qmmd **0.2.0** _(optional)_ — Lightweight Prometheus GPU exporter (bundled but **not started by default**; use only if you need a separate GPU metrics port)
- Native Go collectors `mm-plugin-cpu`, `mm-plugin-gpu`, `mm-plugin-npu`,
  `mm-plugin-proc`, `mm-plugin-membw` and opt-in `mm-plugin-rapl` in `/usr/libexec/metrics-manager/`
- Python **3.12** runtime + FastAPI service
- supervisord process supervisor

> [!NOTE]
> The default Metrics Manager already collects GPU metrics via qmassa,
> `mm-plugin-gpu` and Telegraf. Enable qmmd only if you need a standalone
> Prometheus exporter on a separate port. See
> [Environment Variables](./get-started/environment-variables.md#optional-components).

## Support Matrix

| Component    | Version | Support                                |
| ------------ | ------- | -------------------------------------- |
| Docker       | 24.0+   | Required                               |
| Kubernetes   | 1.25+   | Via Helm chart                         |
| Python       | 3.12    | Included in image                      |
| Linux Kernel | 5.4+    | Required for system metrics collection |

## Migration Guide

### From Version 2026.0.x to 2026.1.0

No breaking changes. All existing endpoints remain compatible.

**What changed:**

- SSE stream now returns flat metric format instead of measurement/field split
- Content negotiation added for `/metrics/stream` (HTML vs SSE)

**Migration steps:**

1. Pull new image: `docker pull intel/metrics-manager:2026.1.0`
2. Update `docker-compose.yaml` or Helm values if pinning version
3. Restart: `docker compose up -d`
4. No data migration needed (in-memory store)

## Supporting Resources

- [Get Started Guide](./get-started.md)
- [API Reference](./api-reference.md)
- [Environment Variables](./get-started/environment-variables.md)
- [How It Works](./how-it-works.md)
- [Troubleshooting](./troubleshooting.md)

## Disclaimer

This container image is intended for demo and evaluation purposes and is not
hardened for production deployments out of the box. Its base layer uses Debian;
follow [Debian security advisories](https://www.debian.org/security/) and rebuild
and rescan the final images when updates are available. Ubuntu Pro coverage
does not apply to this Debian image. Functional validation does not establish
that all vulnerabilities are fixed or that a deployment is production-ready.

## License

Copyright (C) 2025-2026 Intel Corporation

SPDX-License-Identifier: Apache-2.0

The Metrics Manager source code is licensed under the [Apache License, Version 2.0](https://www.apache.org/licenses/LICENSE-2.0).

### Third-Party Software

- **Telegraf** — Open source project developed by InfluxData and licensed under the MIT license. See <https://github.com/influxdata/telegraf/blob/master/LICENSE>
- **qmassa** — Open source Intel® GPU monitoring tool licensed under the MIT license. See <https://github.com/ulissesf/qmassa>
- **qmmd** — Prometheus exporter for Intel® GPUs, published on crates.io under the same project
- **Prometheus & OpenTelemetry** — CNCF projects. See <https://prometheus.io/> and <https://opentelemetry.io/> for license terms

You are solely responsible for determining if your use of these tools requires any additional licenses.

## Legal Information

Intel technologies' features and benefits depend on system configuration and may require enabled hardware, software, or service activation. Learn more at intel.com, or from the OEM or retailer.

No computer system can be absolutely secure. Intel does not assume any liability for lost or stolen data or systems or any damages resulting from such losses.

You may not use or facilitate the use of this document in connection with any infringement or other legal analysis concerning Intel products described herein. You agree to grant Intel a non-exclusive, royalty-free license to any patent claim thereafter drafted which includes subject matter disclosed herein.

No license (express or implied, by estoppel or otherwise) to any intellectual property rights is granted by this document.

Intel disclaims all express and implied warranties, including without limitation, the implied warranties of merchantability, fitness for a particular purpose, and non-infringement, as well as any warranty arising from course of performance, course of dealing, or usage in trade.

This document contains information on products, services and/or processes in development. All information provided here is subject to change without notice. Contact your Intel representative to obtain the latest forecast, schedule, specifications and roadmaps.

The products and services described may contain defects or errors which may cause deviations from published specifications. Current characterized errata are available on request.

Intel, the Intel logo, and Xeon are trademarks of Intel Corporation in the U.S. and/or other countries.

\*Other names and brands may be claimed as the property of others.

© 2025-2026 Intel Corporation
