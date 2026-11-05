# VIPPET Performance Benchmark Suite

Automated benchmarking of VIPPET pipelines on Intel platforms — CPU, GPU (Xe), and NPU.
Collects hardware KPIs per job and produces JSON, CSV, and HTML bar-chart reports.

## Prerequisites

- VIPPET running at `http://localhost:7860` with models downloaded
- Python dev venv set up (`make` from project root creates `.venv` automatically)

## Usage

```bash
make test-performance                    # default: CPU+GPU+NPU, 1 and 3 streams
make test-performance PERF_CONFIG=quick  # CPU+GPU only, 1 stream
make test-performance PERF_CONFIG=full   # all variants, 1/3/5/10 streams
```

Or call pytest directly for more control:

```bash
# Collect only (preview test matrix)
python -m pytest --collect-only vippet/tests/performance/

# Run a specific pipeline
python -m pytest --log-cli-level=INFO -m perf -k "object_detection" vippet/tests/performance/

# Generate JUnit XML for CI
python -m pytest -m perf --junitxml=results/perf.xml vippet/tests/performance/
```

## Configuration

Test parameters are controlled via YAML config files in `config/` and environment variables:

| Env var                 | Default                           | Description                                |
|-------------------------|-----------------------------------|--------------------------------------------|
| `VIPPET_BASE_URL`       | `http://localhost/api/v1`         | VIPPET API endpoint                        |
| `VIPPET_METRICS_URL`    | `http://localhost/metrics/stream` | Metrics endpoint (via nginx proxy)         |
| `PERF_CONFIG`           | `default`                         | Config preset (`default`, `quick`, `full`) |
| `PERF_RESULTS_DIR`      | `./results`                       | Output directory for reports               |
| `PERF_METRICS_INTERVAL` | `2.0`                             | HW sampling interval (seconds)             |

Set `metrics.trim_fraction` in the YAML preset, override it with
`PERF_METRICS_TRIM_FRACTION` (default `0.1`), or override it for one run:

```bash
python -m pytest --metrics-trim-fraction=0.1 vippet/tests/performance/
```

Precedence is CLI > environment > YAML > default (`0.1`). The value must be finite
and satisfy `0 <= value < 0.5`; `0` disables trimming. Each average discards
`int(sample_count * trim_fraction)` samples from each **chronological** end,
not the lowest/highest metric values. Minima and maxima still include both ends.
If a metric has no readings inside the trimmed window, its average uses all
remaining readings. Combined GPU utilization follows the same rule.

## Hardware Metrics

Aggregated values are stored under `hw_metrics` in JSON results:

- NPU frequency arrives from metrics-manager in Hz and is converted to
 `npu_frequency_mhz_avg`, `npu_frequency_mhz_min`, and `npu_frequency_mhz_max`.
 The old `npu_frequency_*` keys are no longer emitted.
- With at least three collected samples, only the NPU keys in the first sample
 are removed before aggregation. CPU/GPU data and the global `sample_count`
 are preserved. `npu_settle_samples_dropped` records `0` or `1`. One- and
 two-sample runs keep their initial NPU readings.
- `kpi_status` contains only the families requested by the variant, including
 both families for `GPU_NPU`: `ok` for observed nonzero activity, `idle` for
 measured activity that stays zero, and `no_samples` when activity readings
 are absent. CPU uses utilization; GPU checks all utilization engines; NPU
 checks utilization, frequency, and power, so active frequency/power can
 establish activity even when NPU utilization is zero or missing.
- `cpu_sample_count`, `gpu_sample_count`, and `npu_sample_count` count samples
 carrying any KPI for each requested family. A family with only temperature
 or memory readings can have a positive count but `no_samples` activity.
 Statuses and counts use settled, untrimmed samples, not the average window.
- Non-`ok` families produce both a log warning and a pytest warning summary.
 `kpi_warnings` stores semicolon-separated entries such as
 `GPU:no_samples;NPU:idle`, or an empty string when every requested family is
 `ok`. Warnings do not change the job outcome.

JSON keeps numeric readings, `kpi_status`, and `kpi_warnings` as separate values;
report generation does not substitute warning text for numbers in the source data.

CSV adds `kpi_warnings` and renames `npu_frequency_avg` / `npu_frequency_max` to
`npu_frequency_mhz_avg` / `npu_frequency_mhz_max`. Consumers of the old CSV column
names must switch to the new names. Available values, including zero, remain
numeric. Missing cells for a requested family with status `no_samples` contain
`NO_SAMPLES`; other missing cells remain empty. HTML adds NPU frequency in MHz and
`no samples` / `idle` badges next to each affected family's utilization.

The quick and full presets explicitly configure the metrics endpoint, sampling
interval, and trimming. `benchmark.variants` lists allowed device families, not
every combination: full permits `cpu`, `gpu`, and `npu`, which also selects mixed
`GPU_NPU` variants. Quick omits NPU, so it excludes those mixed variants.
The built-in VIPPET benchmark's trimming is unchanged.

Run the offline harness regression tests without a backend or hardware:

```bash
cd vippet
python -m pytest -q tests/unit/perf_helpers_test.py
```

## Layout

```text
conftest.py                 # pytest fixtures and parametrize hook
pytest.ini                  # markers and pythonpath
test_pipeline_performance.py  # test module
perf_helpers/
├── config.py               # env-var-driven constants
├── hw_monitor.py           # background HW metric sampler
└── reporters.py            # JSON/CSV export + HTML report generation
config/
├── default.yaml            # CPU+GPU+NPU, 1 & 3 streams
├── quick.yaml              # CPU+GPU, 1 stream
└── full.yaml               # all variants, 1/3/5/10 streams
```

## Reports

After a run, results are saved to `results/bench_YYYYMMDD_HHMMSS/`:

- `.json` — structured results (all test cases + HW metrics)
- `.csv` — flat table for spreadsheet analysis
- `.html` — interactive Chart.js report with FPS, utilization, and power charts
