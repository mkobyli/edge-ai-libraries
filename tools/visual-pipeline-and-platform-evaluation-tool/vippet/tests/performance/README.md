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

### Deploy the matching collector and UI

Run from the ViPPET root on an already provisioned Linux host with Docker,
the required drivers, a healthy ViPPET backend, and downloaded models/videos.
Preserve local Compose changes and the existing hardware profile. The
metrics-manager service retains its existing privileged host access; use only
trusted images. Do not install the bare-metal services alongside the container
on the same telemetry ports.

Build a uniquely tagged pair from this checkout, then extract the bare-metal
package from that exact metrics-manager image without rebuilding its collectors:

```bash
set -e
REPO_ROOT=$(git rev-parse --show-toplevel)
MM_DIR="$REPO_ROOT/microservices/metrics-manager"
export NPU_BUILD_TAG="npu-$(date -u +%Y%m%d%H%M%S)"
export METRICS_MANAGER_IMAGE="metrics-manager:$NPU_BUILD_TAG"
export VIPPET_UI_IMAGE="vippet-ui:$NPU_BUILD_TAG"
make -C "$MM_DIR" build TAG="$NPU_BUILD_TAG"
MM_IMAGE_ID=$(docker image inspect -f '{{.Id}}' "$METRICS_MANAGER_IMAGE")
IMAGE="$MM_IMAGE_ID" SKIP_IMAGE_BUILD=1 "$MM_DIR/native/build.sh"
docker build --build-arg http_proxy --build-arg https_proxy --build-arg no_proxy \
	-t "$VIPPET_UI_IMAGE" -f ui/Dockerfile .
UI_IMAGE_ID=$(docker image inspect -f '{{.Id}}' "$VIPPET_UI_IMAGE")
```

The `.deb` is in `microservices/metrics-manager/native/packaging/dist/`; its
version still comes from metrics-manager's `VERSION`, not the image tag.
`build.sh` copies all six collectors from the image and builds only `mm-tui`
separately. Verify the packaged files against the image before distribution:

```bash
docker run --rm --entrypoint /bin/sh \
	-v "$MM_DIR/native/packaging/dist:/artifacts:ro" "$MM_IMAGE_ID" -ec '
		dpkg-deb -x /artifacts/metrics-manager_*.deb /tmp/package
		for plugin in cpu gpu npu proc membw rapl; do
			cmp /usr/libexec/metrics-manager/mm-plugin-$plugin \
				/tmp/package/usr/libexec/metrics-manager/mm-plugin-$plugin
			sha256sum /tmp/package/usr/libexec/metrics-manager/mm-plugin-$plugin
		done'
```

Persist both image choices in the local, Git-ignored `.env.images` file. Back up
any existing selection before replacing it. This is trusted, shell-compatible
configuration: use only image references, never secrets or shell commands.
`setup_env.sh` leaves this file intact and appends it to the generated `.env`.
Standard `make run`, `make run-dev`, and build commands therefore retain the
pair when they regenerate `.env`; direct Compose commands also read the
generated selection. Other services keep their existing tag configuration.

```bash
cat > .env.images <<EOF
METRICS_MANAGER_IMAGE=$METRICS_MANAGER_IMAGE
VIPPET_UI_IMAGE=$VIPPET_UI_IMAGE
EOF
./setup_env.sh
```

Use the detected profile in `.env` (`cpu`, `gpu`, or `npu`). Confirm
the backend is healthy before stopping the UI. This deliberately creates a brief
UI outage instead of exposing the old consumer to the new collector. No `latest`
tag or automatic pull is needed for this local rollout.

```bash
set -a
. ./.env
set +a
compose=(docker compose -f compose.yml -f "compose.$COMPOSE_PROFILES.yml")
"${compose[@]}" config --quiet
"${compose[@]}" ps vippet metrics-manager vippet-ui
"${compose[@]}" stop vippet-ui metrics-manager
"${compose[@]}" up -d --no-deps --no-build --pull never --force-recreate \
	--wait --wait-timeout 120 metrics-manager vippet-ui
test "$(docker inspect -f '{{.Image}}' "$("${compose[@]}" ps -q metrics-manager)")" = "$MM_IMAGE_ID"
test "$(docker inspect -f '{{.Image}}' "$("${compose[@]}" ps -q vippet-ui)")" = "$UI_IMAGE_ID"
curl -fsS http://localhost:9090/health
curl -fsS http://localhost/ > /dev/null
```

Keep the previous pair of image IDs for rollback and roll back both services
together by updating both entries in `.env.images` before regenerating `.env`.
To restore the published defaults, remove `.env.images`, unset any exported
`METRICS_MANAGER_IMAGE` and `VIPPET_UI_IMAGE`, then run `./setup_env.sh` again.
Do not edit the auto-generated `.env` as the persistent source of image choices.
Close existing UI tabs or hard-refresh them after deployment; an open
tab can still execute the older bundle. Building the UI does not replace its
separate TypeScript checks. Resolve or explicitly track failed release gates
before a production rollout.

### Verify under load

Preview the matrix with `PERF_CONFIG=full python -m pytest --collect-only -q
vippet/tests/performance/`. Select an NPU case with an installed model and video,
then run its exact pytest node ID with `PERF_CONFIG=full` and
`PERF_RESULTS_DIR=./results/npu-rollout`. The quick preset excludes NPU. Require
at least three metrics samples and record the model, input, stream count,
runtime, hardware profile, source revision, image IDs, and any dirty source
changes with the results. A skipped case or one-shot idle reading is not a load
benchmark.

During the same run, compare `:9273/metrics`, `/metrics/stream`,
`mm-tui -once -endpoint http://localhost:9273/metrics`, and the refreshed UI.
The planned NPU contract retains `npu_frequency` in actual Hz:
ARL ratio 48 is `1600000000` Hz, displayed as 1600 MHz
(or 1.60 GHz in TUI). Historical display-scaled samples are not comparable.
Check matching host labels and sample timestamps rather than requiring equality
between independent reads. Prometheus exports `npu_bandwidth` in decimal MB/s,
`npu_memory_mb` in binary MiB and `npu_frequency_max_mhz` in MHz.
The harness preserves its report units of MHz, MB/s and MiB; it converts only
the current NPU frequency from Hz to MHz. Confirm actual zero remains zero,
and unavailable readings become absent after the 15-second exporter TTL plus
flush/poll latency.

Inspect the generated JSON, CSV, and HTML reports: frequency columns migrate
from `npu_frequency_avg/max` to `npu_frequency_mhz_avg/max`, with no raw-Hz values
under the new names. Check `npu_settle_samples_dropped`, `npu_sample_count`,
`kpi_status`, and `kpi_warnings`. Missing requested families must produce
`no_samples` and report markings, not fabricated zeros. The independent Python
CLI/exporter is not a plugin dependency; do not equate traffic measurements from
different counter scopes merely because both use MB/s.

### Local acceptance run (2026-11-06)

This historical run predates the 2026.3.0 metric-name migration. Its images
must not be paired with consumers requiring the new names. It does not validate
the merged sources; repeat acceptance after building and deploying matching images.

Built from base revision `66f944d5acfc6fd5e85523de38df9a04ce9a3540` with the
working-tree test, runner, Compose, and documentation changes. Both running
containers were checked against the image IDs, not just their tags:

`metrics-manager:npu-20261106-step7`:
`sha256:b981e7deac41f3f3ddfdf8bc45a2d3020aa1d83c0aa52675de063e435137b2e5`.

`vippet-ui:npu-20261106-step7`:
`sha256:ebd8cf3d6d186bf87e85dc109f1f254d6ff79f7c1b604d9a3585c9b882f2a941`.

The generated `metrics-manager_2026.1.0_amd64.deb` passed byte comparisons for
all six collectors against that image. The package was built, not installed.
The local stack used the NPU Compose profile, a backend built from this checkout,
and existing models/videos; no model-download or discovery service was needed.
Both collector/UI image tags are now retained in local `.env.images`, independently
of the generator's repeated writes to `.env`.

On Core Ultra 9 285 / Intel AI Boost, the existing `motion_detection_npu_x1`
case (YOLOv8n, one stream, output disabled, input run to completion) passed at
157.93 FPS in 16.04 seconds. Its JSON/CSV/HTML reports are under
`vippet/tests/performance/results/npu-20261106-step7/bench_20261106_083345/`.
Eight samples yielded seven NPU samples after one settle sample was removed;
NPU status was `ok`, warnings were empty, frequency max was 1600 MHz, and
utilization max was 100%. JSON/CSV frequency average was 1371.43 MHz; HTML's
existing whole-MHz rounding displayed 1371.0 MHz.

A separate 30-second repeat completed at 158.78 FPS. During that repeat,
Prometheus reported `1.6e+09` Hz and 99% utilization; TUI showed
`1.60 GHz of 1.60 GHz` and 99%, and the deployed UI showed 99%.
An additional run started from the UI completed at 158.40 FPS and retained
an NPU frequency history reaching 1600 MHz.

This is local hardware validation, not production release approval. The two
TypeScript errors in `useUndoRedo.ts` and `vite.config.ts` were subsequently
fixed in source: the timer uses the return type of `setTimeout`, and the proxy
reads the URL from its request argument. Both UI TypeScript configurations,
four UI tests, targeted ESLint, and the production build now pass. The image
IDs above predate this follow-up; no replacement images were deployed for it.
The pre-existing difference in bandwidth counter scope
between the native collector and standalone Python reader also remains;
these runs do not establish equivalence of their traffic measurements.

### Harness settings

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

`make test` from the ViPPET root runs both the existing unittest suite and the
harness/report pytest regressions. Coverage from both runs is combined before
generating the text and HTML reports.

To run only the offline harness regression tests without a backend or hardware:

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
