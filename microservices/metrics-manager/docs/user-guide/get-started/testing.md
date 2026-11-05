<!--
SPDX-FileCopyrightText: (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# Testing Guide

This guide covers how to run tests for Metrics Manager.

## Prerequisites

- Service is running: `docker compose up -d`
- Or local setup with uvicorn and Telegraf running
- `curl` or similar HTTP client

---

## Running Tests via Docker

### Build Test Image

```bash
docker compose build
```

### Run All Tests

```bash
docker compose --profile test run --rm metrics-manager-test
```

Expected output: `179 passed in ~2s`

### Run with Coverage Report

```bash
docker compose --profile test run --rm metrics-manager-test \
  python -m pytest tests/ -v --cov=app --cov-report=term-missing
```

---

## Running Tests Locally

### Prerequisites

- **Python 3.10+** (`python3 --version`)
- **uv** (fast package manager): `pip install uv`
- **Telegraf** installed and running on the system

### Setup

1. **Create a virtual environment:**

   ```bash
   uv venv
   source .venv/bin/activate  # On Windows: .venv\Scripts\activate
   ```

2. **Install test dependencies:**

   ```bash
   uv sync --group test
   ```

3. **Configure the environment:**

   ```bash
   cp .env.example .env
   ```

   Edit `.env` to point to your local Telegraf:

   ```bash
   TELEGRAF_PORT=9273
   TELEGRAF_HTTP_ENDPOINT=http://localhost:8186/write
   PROMETHEUS_TELEGRAF_ENDPOINT=http://localhost:9273
   ```

4. **Start Telegraf on host:**

   ```bash
   telegraf --config telegraf.conf
   ```

### Run Tests

```bash
# All tests
pytest

# Verbose output
pytest -v

# With coverage report
pytest --cov=app --cov-report=html

# Run specific test file
pytest tests/test_models.py -v
```

---

## Telemetry selection regression tests

The selection tests check the actual bundled Telegraf examples. To also run
deterministic tests with the pinned Telegraf binary, use the existing project
Python test environment from the component directory:

```bash
TELEGRAF_TEST_BINARY=/usr/bin/telegraf \
  .venv/bin/python -m pytest tests/test_telemetry_selection.py tests/test_docker_collectors.py -q
bash native/tests/configure-metrics-test.sh
```

Use the Telegraf version pinned in `versions.env`. Without
`TELEGRAF_TEST_BINARY`, only the binary-dependent fixture tests are skipped;
the static selection and Docker configuration tests still run.
The fixtures use temporary procfs/sysfs/udev data and `telegraf --test`, not
real host disks, mounts or network traffic. They verify primary/virtual NIC
separation, exact custom mount/device selections, serial/WWID opt-in and IRQ
field filtering. IRQ filtering uses line-protocol fixtures because that input
has no procfs path override. No service, root privileges or hardware access
is required. These checks complement, not replace, hardware validation.

## Native memory-bandwidth validation

From the native module, run:

```bash
go test ./internal/cpu ./internal/membw
```

Fixtures cover nonzero representative CPUs, multiple dies/controllers, duplicate
PMU aliases, complete DDR subchannel pairs, aggregate-versus-subchannel selection,
exact byte rates, invalid CPU masks, permission/open failures and short reads.
Failed snapshots must not change any baseline or publish partial host totals;
counter resets re-prime the entire set.

The hardware smoke test also passed on a dual-socket Xeon 6972P with Linux 6.8:
six IMC PMU types, representatives `0,96`, and two DDR subchannels per PMU.
`strace -f -c -e trace=perf_event_open` confirmed **48 successful opens**:
6 PMUs * 2 sockets * 2 subchannels * 2 directions. The unprivileged run under
`perf_event_paranoid=4` failed visibly without publishing a measurement.
The privileged `-once` run emitted read/write/total in bytes/s.

For an independent counter check on that event layout:

```bash
sudo env LC_ALL=C perf stat -a --per-socket --no-big-num -x ';' \
  -e 'uncore_imc/cas_count_read_sch0/,uncore_imc/cas_count_read_sch1/,uncore_imc/cas_count_write_sch0/,uncore_imc/cas_count_write_sch1/' \
  -- sleep 2
```

Check `perf list` for the installed tool's event names. Perf 6.8 uses the
aggregate `uncore_imc/` selector for these PMUs. Both sockets reported both
directions/subchannels with 100% running time. Run perf and the collector
**sequentially**, not together: they compete for counter slots. Separate time
windows need not have identical rates; this smoke test establishes coverage
and error handling, not peak-bandwidth calibration or a workload benchmark.
The collector itself needs no external perf executable.
No host perf policy, service permissions or capabilities were changed.

## Test Categories

| Test File                      | Description                                                                |
| ------------------------------ | -------------------------------------------------------------------------- |
| `test_models.py`               | Pydantic models: Metric, SimpleMetric, MetricsBatch, InfluxDB/OTLP parsing |
| `test_store.py`                | MetricsStore CRUD, persistence, cleanup, eviction, statistics              |
| `test_routes.py`               | REST API endpoints: health, ingestion, queries, Prometheus                 |
| `test_settings.py`             | Pydantic Settings loading, environment variables overrides, validation     |
| `test_logging_config.py`       | Structured logging setup (JSON and text formats)                           |
| `test_main.py`                 | FastAPI middleware, lifespan, correlation ID handling                      |
| `test_metrics.py`              | Internal service metrics counters and uptime                               |
| `test_rate_limit.py`           | IP-based rate limiting middleware                                          |
| `test_sse.py`                  | SSE endpoint `/metrics/stream`: streaming, parsing, errors                 |
| `test_versions.py`             | Pinned third-party versions stay in sync between `versions.env` and `Dockerfile` |
| `test_telegraf_integration.py` | Telegraf configuration mounting and custom-metrics (integration)           |

The native collectors have their own Go test suite, run separately:

```bash
cd native && go test ./...
```

It also runs inside the image build, so `make build` fails if a collector regresses.

---

## Smoke Tests (Manual Testing)

After starting the service with `docker compose up`, verify all endpoints:

### Test 1: Simple JSON Metric

```bash
curl -X POST http://localhost:9090/api/v1/metrics/simple \
  -H "Content-Type: application/json" \
  -d '{"name": "test_metric", "value": 123.45}'

# Verify
curl -s http://localhost:9090/api/v1/metrics/latest | grep -i "test_metric"
curl -s http://localhost:9090/metrics | grep "test_metric"
```

### Test 2: JSON Batch with Tags

```bash
curl -X POST http://localhost:9090/api/v1/metrics \
  -H "Content-Type: application/json" \
  -d '{
    "metrics": [
      {"name": "cpu_test", "tags": {"host": "test-server"}, "fields": {"usage": 55.5}},
      {"name": "memory_test", "tags": {"host": "test-server"}, "fields": {"used_mb": 4096}}
    ]
  }'

# Verify
curl -s http://localhost:9090/api/v1/metrics/latest | jq '.metrics | keys'
```

### Test 3: InfluxDB Line Protocol

```bash
curl -X POST http://localhost:9090/api/v1/metrics/influx \
  -H "Content-Type: text/plain" \
  -d 'influx_test,host=myhost,env=test value=99.9'

# Verify
curl -s http://localhost:9090/api/v1/metrics/latest | grep -i "influx_test"
```

### Test 4: Direct to Telegraf HTTP Listener

```bash
curl -X POST http://localhost:8186/write \
  -H "Content-Type: text/plain" \
  -d 'telegraf_direct,source=curl temperature=25.5'

# Verify
curl -s http://localhost:9273/metrics | grep "telegraf_direct"
```

### Test 5: OpenTelemetry (OTLP)

```bash
curl -X POST http://localhost:9090/api/v1/metrics/otlp \
  -H "Content-Type: application/json" \
  -d '{
    "resourceMetrics": [{
      "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "test-service"}}]},
      "scopeMetrics": [{
        "metrics": [{
          "name": "otlp_test_metric",
          "gauge": {"dataPoints": [{"asDouble": 77.7, "attributes": []}]}
        }]
      }]
    }]
  }'

# Verify
curl -s http://localhost:9090/api/v1/metrics/latest | grep -i "otlp_test"
```

### Test 6: SSE Real-time Streaming

```bash
# Terminal 1: Connect to SSE stream
curl -N -H "Accept: text/event-stream" http://localhost:9090/metrics/stream

# Terminal 2: Push a metric
curl -X POST http://localhost:9090/api/v1/metrics/simple \
  -H "Content-Type: application/json" \
  -d '{"name": "realtime_test", "value": 42}'

# Terminal 1 should show the metric in the next SSE event (~500ms)
# Or open http://localhost:9090/metrics/stream in a browser
```

### Test 7: Full Health Check

```bash
echo "=== Health ===" && curl -s http://localhost:9090/health | jq .
echo "=== Detailed ===" && curl -s http://localhost:9090/api/health | jq .
echo "=== Stats ===" && curl -s http://localhost:9090/api/v1/stats | jq .
echo "=== Telegraf ===" && curl -s http://localhost:9273/metrics | head -20
```

---

## Expected Test Output

```
========================= test session starts ==========================
tests/test_settings.py::...              PASSED
tests/test_logging_config.py::...        PASSED
tests/test_main.py::...                  PASSED
tests/test_models.py::...                PASSED
tests/test_store.py::...                 PASSED
tests/test_routes.py::...                PASSED
tests/test_metrics.py::...               PASSED
tests/test_rate_limit.py::...            PASSED
tests/test_sse.py::...                   PASSED
tests/test_versions.py::...              PASSED
tests/test_telegraf_integration.py::...  SKIPPED (requires Docker)
========================= 179 passed, 1 skipped in 1.70s ==============
```

---

## Development Setup

For local development with hot-reload:

```bash
# Install dev dependencies
uv sync --group test --group dev

# Run with auto-reload
uvicorn app.main:app --reload --port 9090

# Check the native/Docker Python scope (Helm validation is separate)
.venv/bin/black --check --extend-exclude 'test_helm_chart\.py$' app tests docker_collectors.py
.venv/bin/ruff check --exclude test_helm_chart.py app tests docker_collectors.py
.venv/bin/mypy app docker_collectors.py
.venv/bin/python -m pytest --ignore=tests/test_helm_chart.py
```

Async fixtures use function-scoped event loops and close their stores/clients.
Resource leaks and unraisable exceptions fail the suite rather than being
silenced. Lifecycle regressions cover queued/active HTTP pushes, debounce
cancellation and cleanup when the application lifespan exits with an error.

---

## Troubleshooting Tests

| Issue                          | Solution                                                          |
| ------------------------------ | ----------------------------------------------------------------- |
| `Module not found: app`        | Ensure you are in `metrics-manager/` directory and ran `uv sync`  |
| `Connection refused on :9090`  | Telegraf not running. Start it: `telegraf --config telegraf.conf` |
| `Test fixtures failed`         | Clear cache: `pytest --cache-clear`                               |
| `telegraf_integration skipped` | Expected if not in Docker. Other tests still pass                 |

## Supporting Resources

- [Get Started Guide](../get-started.md)
- [System Requirements](./system-requirements.md)
- [Building from Source](./build-from-source.md)
- [Troubleshooting](../troubleshooting.md)

## License

Copyright (C) 2025-2026 Intel Corporation

SPDX-License-Identifier: Apache-2.0
