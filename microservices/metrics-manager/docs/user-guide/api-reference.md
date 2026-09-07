# API Reference

**Version: 2026.1.0**

This document describes all REST API endpoints, request/response formats, and examples.

---

## Health Checks

### Basic Health Check

```bash
curl http://localhost:9090/health
```

```json
{
  "status": "healthy",
  "version": "2026.1.0",
  "uptime_seconds": 3600.5,
  "checks": { "store": true }
}
```

### Detailed Health Check

```bash
curl http://localhost:9090/api/health
```

```json
{
  "status": "healthy",
  "version": "2026.1.0",
  "uptime_seconds": 3600.5,
  "checks": { "store": true },
  "metrics_store": {
    "total_metrics": 42,
    "metric_names": ["fps", "cpu"],
    "retention_seconds": 300,
    "max_metrics": 100000,
    "telegraf_endpoint": "http://localhost:8186/write"
  },
  "sse_subscribers": 2
}
```

### Service Statistics

```bash
curl http://localhost:9090/api/v1/stats
```

```json
{
  "requests_total": 1523,
  "errors_total": 5,
  "metrics_received_total": 45000,
  "sse_events_sent": 3120,
  "uptime_seconds": 3600.5
}
```

---

## Platform and Device Capabilities

### Get Capabilities (Minimal Profile)

```bash
curl -s "http://localhost:9090/api/v1/capabilities?profile=minimal" | jq
```

Use `minimal` for a compact platform and device summary suitable for quick validation.

### Get Capabilities (Expanded Profile)

```bash
curl -s "http://localhost:9090/api/v1/capabilities?profile=expanded" | jq
```

Use `expanded` for full technical inventory.

### Endpoint and Query Parameter

- Endpoint: `GET /api/v1/capabilities`
- Query parameter: `profile`
  - `minimal` (default)
  - `expanded`

### Example Response Shape

```json
{
  "generated_at": 1782833792,
  "profile": "minimal",
  "categories": {},
  "platform": {
    "hostname": "example-host",
    "system_memory": {
      "installed_gib": 30.91,
      "type": "DDR5"
    },
    "system_storage": {
      "total_capacity_gib": 931.51,
      "available_gib": 225.14
    },
    "device_summary": {
      "cpu": 1,
      "igpu": 1,
      "dgpu": 1,
      "npu": 0
    }
  },
  "devices": []
}
```

### Notes

- Hardware-enriched values are best-effort and depend on host visibility of `/sys`, `/proc`, and `/dev/dri` (when GPU devices are present).
- PCI branding/model naming uses `lspci` (`pciutils`) when available.

---

## Push Metrics

Four input formats are supported. All return `{"accepted": N, "message": "..."}`.

### A. Simple JSON - `POST /api/v1/metrics/simple`

The simplest format for single metrics.

```bash
# Single metric
curl -X POST http://localhost:9090/api/v1/metrics/simple \
  -H "Content-Type: application/json" \
  -d '{"name": "my_metric", "value": 42.5}'

# With tags
curl -X POST http://localhost:9090/api/v1/metrics/simple \
  -H "Content-Type: application/json" \
  -d '{
    "name": "fps",
    "value": 29.97,
    "tags": {"source": "camera1", "pipeline": "detection"}
  }'

# With explicit timestamp (optional)
curl -X POST http://localhost:9090/api/v1/metrics/simple \
  -H "Content-Type: application/json" \
  -d '{
    "name": "fps",
    "value": 29.97,
    "timestamp": 1776947971
  }'
```

**Fields:**

| Field       | Type         | Required | Description                                                                                                                           |
| ----------- | ------------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `name`      | string       | yes      | Metric name (1–256 chars)                                                                                                             |
| `value`     | int \| float | yes      | Numeric value                                                                                                                         |
| `tags`      | object       | no       | Key-value labels (e.g. `{"source": "camera1"}`)                                                                                       |
| `timestamp` | int \| float | no       | Unix timestamp — seconds (`< 1e12`), milliseconds (`< 1e15`), or nanoseconds. Auto-detected. Defaults to current UTC time if omitted. |

---

### B. JSON Batch - `POST /api/v1/metrics`

Multiple metrics at once, with multiple fields per metric.

```bash
curl -X POST http://localhost:9090/api/v1/metrics \
  -H "Content-Type: application/json" \
  -d '{
    "metrics": [
      {
        "name": "cpu",
        "fields": {"usage_user": 45.2, "usage_system": 12.1},
        "tags": {"host": "server1"},
        "timestamp": 1704067200000000000
      },
      {
        "name": "inference",
        "fields": {"latency_ms": 23.5, "throughput": 42},
        "tags": {"model": "yolov8"},
        "metric_type": "gauge"
      }
    ]
  }'
```

**Response:**

```json
{ "accepted": 2, "message": "Accepted 2 metrics" }
```

---

### C. InfluxDB Line Protocol - `POST /api/v1/metrics/influx`

Standard InfluxDB text format, one metric per line.

```bash
curl -X POST http://localhost:9090/api/v1/metrics/influx \
  -H "Content-Type: text/plain" \
  -d 'cpu_usage,host=server1,cpu=cpu0 usage=45.2 1704067200000000000
memory,host=server1 used_percent=67.5 1704067200000000000
fps,pipeline=detection value=29.97'
```

**Format:**

```
measurement[,tag1=val1,tag2=val2] field1=val1[,field2=val2] [timestamp]
```

**Alternative endpoint (InfluxDB-compatible):**

```bash
curl -X POST http://localhost:9090/write \
  -H "Content-Type: text/plain" \
  -d 'cpu,host=server1 usage=45.2'
```

Returns `204 No Content`.

**Direct to Telegraf HTTP listener (bypasses FastAPI):**

```bash
curl -X POST http://localhost:8186/write \
  -H "Content-Type: text/plain" \
  -d 'cpu,host=server1 usage=45.2'
```

---

### D. OpenTelemetry (OTLP) - `POST /api/v1/metrics/otlp`

OpenTelemetry metrics format (protocol buffer or JSON).

```bash
curl -X POST http://localhost:9090/api/v1/metrics/otlp \
  -H "Content-Type: application/json" \
  -d '{
    "resourceMetrics": [{
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "my-service"}}
        ]
      },
      "scopeMetrics": [{
        "metrics": [{
          "name": "custom_metric",
          "gauge": {
            "dataPoints": [{
              "asDouble": 42.5,
              "attributes": [
                {"key": "host", "value": {"stringValue": "server1"}}
              ]
            }]
          }
        }]
      }]
    }]
  }'
```

---

## Query Metrics

### Get All Custom Metrics (JSON)

```bash
curl http://localhost:9090/api/v1/metrics
```

```json
{
  "metrics": {
    "fps": {
      "name": "fps",
      "fields": { "value": 29.97 },
      "tags": { "source": "camera1" },
      "timestamp": 1704067200
    },
    "cpu": {
      "name": "cpu",
      "fields": { "usage": 45.2 },
      "tags": { "host": "server1" },
      "timestamp": 1704067200
    }
  }
}
```

### Filter by Metric Name

```bash
curl "http://localhost:9090/api/v1/metrics?name=fps"
```

### Get Latest Value for Each Metric

```bash
curl http://localhost:9090/api/v1/metrics/latest
```

### Get Metric Names List

```bash
curl http://localhost:9090/api/v1/metrics/names
```

```json
{ "names": ["fps", "cpu", "mem"], "count": 3 }
```

### Prometheus Format (Custom Metrics Only)

```bash
curl http://localhost:9090/metrics
```

```
fps{source="camera1"} 29.97
cpu_usage{host="server1"} 45.2
```

### Telegraf Prometheus Endpoint (System + Custom Metrics)

```bash
curl http://localhost:9273/metrics
```

Returns all system metrics (CPU, memory, temperature, GPU, NPU) plus persisted custom metrics in Prometheus text format.

---

## Delete Metrics

### Clear All Metrics

```bash
curl -X DELETE http://localhost:9090/api/v1/metrics
```

```json
{ "cleared": 5, "message": "Cleared 5 metrics" }
```

### Clear a Specific Metric by Name

```bash
curl -X DELETE "http://localhost:9090/api/v1/metrics?name=my_metric"
```

---

## SSE Streaming

### Connect as Client (Python)

```python
import httpx

with httpx.stream("GET", "http://localhost:9090/metrics/stream",
                  headers={"Accept": "text/event-stream"}) as r:
    for line in r.iter_lines():
        if line.startswith("data:"):
            import json
            event = json.loads(line[5:])
            print(event)
```

### Connect as Client (JavaScript)

```javascript
const es = new EventSource("http://localhost:9090/metrics/stream");
es.onmessage = (event) => {
  const { metrics } = JSON.parse(event.data);
  console.log(metrics);
};
```

### Event Format

```json
{
  "timestamp": 1777461975860,
  "metrics": [
    {
      "name": "cpu_usage_user",
      "labels": { "cpu": "cpu-total", "host": "myhost" },
      "value": 0.14,
      "timestamp": 1777463430000
    },
    {
      "name": "memory_used_percent",
      "labels": { "host": "myhost" },
      "value": 67.5,
      "timestamp": 1777463430000
    }
  ]
}
```

Each event contains all metrics available at that moment (system + custom). The stream polls Telegraf every `PROMETHEUS_POLLER_INTERVAL_MS` milliseconds (default 500 ms).

### Browser / Live UI

Opening `http://localhost:9090/metrics/stream` in a browser serves an HTML page with an in-place updated table. Direct SSE access:

```bash
curl -N -H "Accept: text/event-stream" http://localhost:9090/metrics/stream
```

---

## Metric Types

Supported `metric_type` values in JSON Batch format (default: `gauge`):

| Type        | Description         | Example                         |
| ----------- | ------------------- | ------------------------------- |
| `gauge`     | Instantaneous value | temperature, FPS, CPU usage     |
| `counter`   | Monotonic counter   | request count, processed frames |
| `histogram` | Value distribution  | request latency                 |
| `summary`   | Statistical summary | response time percentiles       |

---

## System Metrics (Telegraf)

Collected every 1 second, available at `:9273/metrics` (Prometheus format).

### CPU (`cpu`)

| Field          | Description                     |
| -------------- | ------------------------------- |
| `usage_user`   | % CPU usage by user processes   |
| `usage_system` | % CPU usage by system processes |
| `usage_idle`   | % CPU in idle state             |

### RAM (`mem`)

| Field               | Description          |
| ------------------- | -------------------- |
| `used_percent`      | % memory used        |
| `available_percent` | % memory available   |
| `total`             | Total memory (bytes) |
| `used`              | Used memory (bytes)  |

### CPU Frequency (`cpu_frequency_avg`, `cpu_core_class`)

Collected by `mm-plugin-cpu` in InfluxDB Line Protocol format.

| Measurement          | Tags             | Fields                                                                  |
| -------------------- | ---------------- | ----------------------------------------------------------------------- |
| `cpu_frequency_avg`  | —                | `frequency` — mean current frequency across all CPUs, in kHz            |
| `cpu_core_class`     | `class`, `source`| `cores`, `frequency_avg`, `usage_user`, `usage_system`, `usage_idle`    |

`class` is `P`, `E` or `LPE`. `source` records how the class was identified
(`pmu` when the kernel exposes a per-class PMU, `heuristic` when it was inferred
from cache and frequency evidence), so a consumer can tell a measured split from
an inferred one.

### Temperature (`temp`)

Filtered to `coretemp_package_id_*` (CPU package temperature). Tag: `sensor`.
Field: `temp`, in degrees Celsius.

Note that the Prometheus endpoint joins the measurement and the field names, so
this series appears there as `temp_temp`, not `temp_temperature`.

### Intel GPU (via `mm-plugin-gpu`)

Parsed from the `qmassa` JSON stream. All measurements carry a `gpu_id` tag and,
when `qmassa` reports one, a `driver` tag naming the kernel driver bound to the
device (`i915` or `xe`).

| Measurement        | Extra tags       | Fields                                                             |
| ------------------ | ---------------- | ------------------------------------------------------------------ |
| `gpu_engine_usage` | `engine`, `type` | `usage` — % busy for compute / render / copy / video / video-enhance |
| `gpu_frequency`    | `tile`, `type`   | `value` (backward-compatible), `cur_freq`, `act_freq`, `min_freq`, `max_freq` |
| `gpu_power`        | `type`           | `value` — `gpu_cur_power` and `pkg_cur_power`, in watts            |
| `gpu_memory`       | —                | `smem_total`, `smem_used`, `vram_total`, `vram_used`                |
| `gpu_temperature`  | `sensor`         | `value` — °C                                                       |
| `gpu_throttle`     | `tile`           | `status`, `pl1`, `pl2`, `pl4`, `prochot`, `ratl`, `thermal`, `vr_tdc`, `vr_thermalert` |

On a multi-tile part such as Meteor Lake, `gpu_frequency` and `gpu_throttle` are
reported once per graphics tile, distinguished by the `tile` tag (`gt0`, `gt1`).

#### Counters that depend on the driver

The `driver` tag exists because `i915` and `xe` do not populate the same
counters, and without it a consumer cannot tell a value the driver never fills
in from one the hardware actually measured.

- **`gpu_memory.smem_used` is always `0` on `i915`.** That driver does not
  expose per-device shared memory usage the way `xe` does. The raw zero is
  published rather than suppressed, so nothing is lost for a driver that does
  report it, but a dashboard should treat `smem_used` as unavailable — not as
  "no memory in use" — while `driver="i915"`. For example:

  ```promql
  gpu_memory_smem_used{driver!="i915"}
  ```

  The bundled TUI applies exactly this rule and renders the value as `n/a`.

- **`vram_total` and `vram_used` are `0` on an integrated GPU**, which has no
  dedicated memory. `qmassa` reports zero rather than omitting the fields.

**Per-process GPU usage (opt-in):** running the plugin with `-clients` adds a
`gpu_client` measurement tagged by `pid` and `comm`, with per-process CPU,
per-engine usage and memory. It is excluded from the Prometheus endpoint by
default (`namedrop` in `telegraf.conf`) because one series per process is
unbounded cardinality. The process command line is never read, so arguments
containing credentials cannot leak into metrics.

### Intel NPU (`npu`) via `mm-plugin-npu`

| Prometheus Name   | Field         | Description                                                                     |
| ----------------- | ------------- | ------------------------------------------------------------------------------- |
| `npu_power`       | `power`       | NPU power draw in watts (derived from `VPU_ENERGY` delta)                       |
| `npu_frequency`   | `frequency`   | NPU display frequency in Hz                                                     |
| `npu_temperature` | `temperature` | NPU SoC temperature in °C (integer)                                             |
| `npu_bandwidth`   | `bandwidth`   | NoC memory bandwidth delta in MB/s                                              |
| `npu_tile_config` | `tile_config` | Active tile configuration                                                       |
| `npu_utilization` | `utilization` | % NPU utilization over the last interval (0–100)                                |
| `npu_memory_mb`   | `memory_mb`   | NPU memory usage in MB, from the driver's `npu_memory_utilization` attribute; `-1` where the driver does not expose it |
| `npu_frequency_max_mhz` | `frequency_max_mhz` | Frequency ceiling the driver reports, omitted where it is not published |

**Requirements:**

- Intel NPU present and the `intel_vpu` driver loaded (`ls /sys/bus/pci/drivers/intel_vpu/`)
- `/sys/class/intel_pmt/` accessible inside the container (provided by `privileged: true` + `/sys:/sys:ro`)
- Supported generations: Meteor Lake (MTL), Arrow Lake (ARL/ARL-H/ARL-S), Lunar Lake (LNL), Panther Lake (PTL)

`npu_memory_mb` and `npu_frequency_max_mhz` come from the `intel_vpu` driver's
sysfs attributes rather than from PMT, and those are world-readable, so they
need no privilege. Both were measured present on Meteor Lake and Panther Lake
alike; an earlier version of this plugin assumed memory usage arrived only
from Panther Lake onwards and reported `-1` on Meteor Lake while the attribute
sat there with a perfectly good value.

**Per-process NPU usage is not available.** The NPU registers an accelerator
device at `/dev/accel/accel0`, but on kernel 6.17 `intel_vpu` publishes no
`drm-*` fields in `fdinfo`, so the kernel does no per-client accounting to read.
There is no NPU PMU either. This is a missing implementation rather than an
architectural limit, so a later kernel may change it; the equivalent GPU
accounting works because `i915` does implement those fields.

---

### Platform power and idle states (`powerstat_*`) via `inputs.intel_powerstat`

| Prometheus Name                                    | Labels                     | Description                                        |
| -------------------------------------------------- | -------------------------- | -------------------------------------------------- |
| `powerstat_package_current_power_consumption_watts` | `package_id`               | Package power draw from RAPL                        |
| `powerstat_package_thermal_design_power_watts`      | `package_id`               | Thermal design power, the budget the draw comes from |
| `powerstat_package_cpu_base_frequency_mhz`          | `package_id`               | Base clock                                          |
| `powerstat_package_max_turbo_frequency_mhz`         | `active_cores`, `hybrid`   | Turbo ceiling per number of active cores; `hybrid` is `primary` for P cores and `secondary` for E cores |
| `powerstat_package_uncore_frequency_mhz_cur`        | `die`, `type`              | Current uncore frequency                            |
| `powerstat_core_cpu_c0_state_residency_percent`     | `cpu_id`, `core_id`        | Time in the active state, per logical CPU           |
| `powerstat_core_cpu_c1_state_residency_percent`     | `cpu_id`, `core_id`        | Shallow idle residency                              |
| `powerstat_core_cpu_c6_state_residency_percent`     | `cpu_id`, `core_id`        | Deep idle residency                                 |
| `powerstat_core_cpu_c7_state_residency_percent`     | `cpu_id`, `core_id`        | Deepest idle residency                              |

**Requirements:**

- The `msr` kernel module. Without it these series are absent rather than reported as failed
- Root in the container; on bare metal a dedicated service with `CAP_SYS_RAWIO` and `CAP_DAC_READ_SEARCH`
- States the part does not implement are detected and skipped with one warning. `current_dram_power_consumption` is *not* enabled by default: where the DRAM RAPL domain is missing it errors on every interval instead

---

### Memory bandwidth (`memory_bandwidth`) via `mm-plugin-membw`

| Prometheus Name                | Field         | Description                                    |
| ------------------------------ | ------------- | ---------------------------------------------- |
| `memory_bandwidth_read_mibps`  | `read_mibps`  | Read throughput at the memory controller       |
| `memory_bandwidth_write_mibps` | `write_mibps` | Write throughput                               |
| `memory_bandwidth_total_mibps` | `total_mibps` | Sum of the two                                 |

This is **whole-system DRAM throughput, not per-device GPU bandwidth**.

Neither Intel GPU driver publishes a per-device byte counter. On `i915` this
was checked on hardware: the PMU exposes engine occupancy, frequencies, rc6
residency and interrupts; DRM `fdinfo` reports engine time in nanoseconds and
*resident* memory sizes rather than transfers; the card's sysfs attributes are
frequencies; and no OA metric sets are registered. On `xe` it was checked in
the driver source, which defines five events in total -- `gt-c6-residency`,
`engine-active-ticks`, `engine-total-ticks`, `gt-actual-frequency` and
`gt-requested-frequency` -- with no byte counter among them. qmassa's own
device state carries no bandwidth field either.

On a machine whose GPU has no dedicated memory the controller figure is still
the closest available signal, because the GPU competes for exactly this
bandwidth. On a discrete card, whose traffic goes to its own memory, it says
correspondingly less.

The controller and its event names are discovered rather than assumed. Current
client parts publish `data_read` and `data_write` on free-running counters,
older ones `data_reads` and `data_writes`, and server parts
`cas_count_read` and `cas_count_write`; the published scale and unit are read
alongside them. Controllers exposing no counters are skipped, which is normal:
a machine typically has several and only some carry events.

**Requirements:**

- A memory controller exposing free-running counters under
  `/sys/bus/event_source/devices/uncore_imc_free_running_*`
- `CAP_PERFMON`, because the counters are read through `perf_event_open`. Where
  it is missing the collector says so and idles rather than reporting zeros

---

### Processes (`process`) via `mm-plugin-proc`

| Prometheus Name            | Field              | Description                             |
| -------------------------- | ------------------ | --------------------------------------- |
| `process_cpu_percent`      | `cpu_percent`      | CPU used over the interval              |
| `process_memory_rss_bytes` | `memory_rss_bytes` | Resident set size                       |

Tagged with `pid` and `process`. Only the process name is collected; command
lines are deliberately never read, because arguments routinely carry
credentials.

Two rankings are published and combined: the busiest by CPU and the largest by
resident memory. Ranking on CPU alone would hide a process leaking memory while
using none. The limit applies to each ranking, so the default of 10 publishes up
to twenty processes.

**Cardinality:** each process is a time series tagged with its process id, and
ids are not reused predictably, so a long-running Prometheus scrape accumulates
series as processes come and go. The limit is what bounds this; raising it, or
setting it to 0 for every process, raises the rate.

---

### Platform (`platform`) via `mm-plugin-cpu`

| Prometheus Name         | Labels                    | Description                        |
| ----------------------- | ------------------------- | ---------------------------------- |
| `platform_logical_cpus` | `model`, `kernel`, `arch` | Describes the machine; the value is the number of logical CPUs and the facts are in the labels |

---

### Per-process GPU usage (`gpu_client`) via `mm-plugin-gpu`

| Prometheus Name           | Field           | Description                                 |
| ------------------------- | --------------- | ------------------------------------------- |
| `gpu_client_engine_<name>` | `engine_*`     | Engine occupancy attributed to the process  |
| `gpu_client_cpu`          | `cpu`           | CPU usage qmassa reports alongside it       |
| `gpu_client_smem_used`    | `smem_used`     | Shared memory held by the process           |
| `gpu_client_vram_used`    | `vram_used`     | Dedicated memory held by the process        |
| `gpu_client_active`       | `active`        | Whether the process is currently submitting  |

Tagged with `pid`, `comm` and `gpu_id`. Enabled with `-clients` and capped by
`-clients-limit`, which defaults to the ten busiest per device. The cap carries
the same cardinality reasoning as the process metrics above, and the ranking is
by engine occupancy so a small cap still shows whoever is actually using the
GPU.

---

## Endpoint Summary

### Input (POST)

| Format                 | Endpoint                      |
| ---------------------- | ----------------------------- |
| JSON Batch             | `POST /api/v1/metrics`        |
| Simple                 | `POST /api/v1/metrics/simple` |
| InfluxDB Line Protocol | `POST /api/v1/metrics/influx` |
| InfluxDB-compatible    | `POST /write`                 |
| OpenTelemetry (OTLP)   | `POST /api/v1/metrics/otlp`   |

### Output (GET)

| Format               | Endpoint                     |
| -------------------- | ---------------------------- |
| JSON metrics list    | `GET /api/v1/metrics`        |
| JSON latest per name | `GET /api/v1/metrics/latest` |
| Metric names list    | `GET /api/v1/metrics/names`  |
| Prometheus text      | `GET /metrics`               |
| Basic health         | `GET /health`                |
| Detailed health      | `GET /api/health`            |
| Service statistics   | `GET /api/v1/stats`          |

### Delete

| Action        | Endpoint                        |
| ------------- | ------------------------------- |
| Clear all     | `DELETE /api/v1/metrics`        |
| Clear by name | `DELETE /api/v1/metrics?name=X` |

### SSE

| Endpoint              | Description                                                             |
| --------------------- | ----------------------------------------------------------------------- |
| `GET /metrics/stream` | SSE stream (system + custom metrics, auto-negotiates HTML for browsers) |

---

## Response Models

| Endpoint                     | Response Model                 |
| ---------------------------- | ------------------------------ |
| `GET /health`                | `HealthResponse`               |
| `GET /api/health`            | `DetailedHealthResponse`       |
| `POST /api/v1/metrics*`      | `MetricsAcceptedResponse`      |
| `GET /api/v1/metrics`        | `MetricsListResponse`          |
| `GET /api/v1/metrics/latest` | `MetricsLatestResponse`        |
| `GET /api/v1/metrics/names`  | `MetricNamesResponse`          |
| `DELETE /api/v1/metrics`     | `MetricsClearedResponse`       |
| `GET /metrics`               | `str` (Prometheus text format) |

---

## HTTP Status Codes

| Code                        | Scenario                                                                |
| --------------------------- | ----------------------------------------------------------------------- |
| `200 OK`                    | Request successful                                                      |
| `201 Created`               | Metric created (if applicable)                                          |
| `204 No Content`            | Request successful, no body (e.g., `/write` endpoint)                   |
| `400 Bad Request`           | Invalid request format or missing required fields                       |
| `422 Unprocessable Entity`  | Validation error (Pydantic) — invalid metric type, malformed JSON, etc. |
| `429 Too Many Requests`     | Rate limit exceeded                                                     |
| `500 Internal Server Error` | Server error (unexpected exception)                                     |
| `503 Service Unavailable`   | Telegraf endpoint unreachable (for some operations)                     |

---

## Rate Limiting

Rate limiting is applied per client IP (unless `TRUST_FORWARDED_HEADERS=true`).

- **Limit**: `RATE_LIMIT_REQUESTS_PER_MINUTE` (default 1000 requests/minute)
- **Burst**: `RATE_LIMIT_BURST` (default 100 tokens available upfront)
- **Exempt paths**: `/health`, `/api/v1/stats`, SSE endpoints

**Response when rate limited:**

```
HTTP/1.1 429 Too Many Requests
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1704067260
```

## Supporting Resources

- [How It Works (Architecture)](./how-it-works.md)
- [Environment Variables](./get-started/environment-variables.md)
- [Troubleshooting](./troubleshooting.md)

## License

Copyright (C) 2025-2026 Intel Corporation

SPDX-License-Identifier: Apache-2.0
