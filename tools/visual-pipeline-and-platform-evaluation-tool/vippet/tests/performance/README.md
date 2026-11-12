# VIPPET Performance Benchmark Suite

Automated benchmarking of VIPPET pipelines on Intel platforms — CPU, GPU (Xe), and NPU.
Collects hardware KPIs per job and produces JSON, CSV, and HTML bar-chart reports.

## Prerequisites

- ViPPET running and reachable (default API base URL: `http://localhost/api/v1`; change it
  with `--base-url`) with the models of the pipelines you want to benchmark installed
- Python 3 with `venv` (see [Setup](#setup))

## Setup

All commands in this document run from the project root,
`tools/visual-pipeline-and-platform-evaluation-tool`.

**Using `make` (recommended).** `make test-performance` creates the `.venv` virtual
environment on first use, installs `vippet/requirements-dev.txt` into it, activates it
and sets `PYTHONPATH`. Nothing else has to be prepared.

**Running the CLI or pytest directly.** Create and activate the virtual environment
yourself, then export `PYTHONPATH` in the same shell:

```bash
python3 -m venv .venv                              # once
source .venv/bin/activate                          # in every new shell
python -m pip install -r vippet/requirements-dev.txt   # once, or after requirements change
export PYTHONPATH=vippet/tests/performance
```

Without the activated venv `python -m perf_helpers.cli` fails with `No module named ...`
for the dependencies (`httpx`, `PyYAML`, `requests`, `pytest`), and without `PYTHONPATH`
it fails with `No module named perf_helpers`.

## Usage

To benchmark your own pipeline, see [Benchmark your own pipeline](#benchmark-your-own-pipeline).

```bash
make test-performance                    # default: CPU+GPU+NPU, 1 and 3 streams
make test-performance PERF_CONFIG=quick  # CPU+GPU only, 1 stream
make test-performance PERF_CONFIG=full   # all variants, 1/3/5/10 streams

# Extra CLI flags via PERF_ARGS
make test-performance PERF_ARGS="--dry-run"
make test-performance PERF_CONFIG=quick PERF_ARGS="--streams 1,5 --pipelines goods-detection"
make test-performance PERF_ARGS="--report-only"   # rebuild HTML from results/latest

# Verbose: stream INFO logs live (arguments after `--` go to pytest).
# The full INFO log is always saved next to the reports; see "Reports" below.
make test-performance PERF_ARGS="-- --log-cli-level=INFO"
```

`make test-performance` delegates to the CLI below and prints the same console output
(see [Console output](#console-output)).

### CLI

Run from the project root, in the shell where the virtual environment is activated and
`PYTHONPATH` is set (see [Setup](#setup)):

```bash
source .venv/bin/activate
export PYTHONPATH=vippet/tests/performance

python -m perf_helpers.cli --help
python -m perf_helpers.cli --config quick --base-url http://10.0.0.5/api/v1
python -m perf_helpers.cli --variants cpu,gpu --streams 1,3,5 --results-dir /tmp/perf
python -m perf_helpers.cli --config ./my-config.yaml

# Arguments after `--` go to pytest unchanged
python -m perf_helpers.cli --streams 1 -- -k goods_detection --junitxml=results/perf.xml
```

The CLI builds the effective configuration and runs `python -m pytest -m perf` in a
subprocess. It passes `vippet/tests/performance` as the test path, unless an argument
after `--` already selects a `.py` file, directory or node id inside it. Option values
such as `--junitxml results/perf.xml` never replace the test path.

The CLI passes its `--config` and setting flags to pytest through the
`PERF_CLI_LAYER` env var (set by the CLI; don't set it yourself). pytest resolves the
settings again from them, so the result JSON records the same values and sources the
CLI used. The CLI exits with pytest's exit code. If the config or a flag is invalid, it exits with `2`.

#### Dry run

`--dry-run` checks that ViPPET is up and reads `/pipelines`, `/devices` and
`/models`. It never submits a job. Combine it with any selection flag to preview
exactly what a run would do:

```bash
python -m perf_helpers.cli --dry-run                                   # default preset
python -m perf_helpers.cli --dry-run --config quick
python -m perf_helpers.cli --dry-run --pipelines vehicle-detection --variants cpu,gpu --streams 1,2,4
python -m perf_helpers.cli --dry-run --base-url http://10.0.0.5/api/v1
make test-performance PERF_ARGS="--dry-run --pipelines vehicle-detection"   # same, via make
```

It prints:

- the effective setting of every key and where each value came from (`default`,
  `yaml`, `env`, `cli`)
- the **Matrix**: pipeline × variant × streams, with the pytest test id
- **Excluded** pipelines/variants, with a reason:

  | Reason               | Meaning                                                     |
  |----------------------|-------------------------------------------------------------|
  | `pipeline_filter`    | Pipeline not listed in `benchmark.pipelines`                |
  | `skip_pipelines`     | Pipeline listed in `benchmark.filters.skip_pipelines`       |
  | `skip_variants`      | Variant listed in `benchmark.filters.skip_variants`         |
  | `variant_filter`     | Variant uses a device family not in `benchmark.variants`    |
  | `unsupported_family` | Unknown device family, or a family the host does not report |
  | `malformed`          | Pipeline/variant entry from the API has no id or name       |

- **Skipped at run time: missing_models**: runs that are in the matrix but that pytest
  will skip because their models are not installed, with the model names. With
  `--no-skip-missing-models` (`benchmark.filters.skip_missing_models: false`) these
  runs are counted in the matrix and listed as **Missing models, will run anyway**
  instead.

```text
Matrix: 4 run(s) = pipeline x variant x streams [1, 3]
pipeline          variant  streams  test id
----------------  -------  -------  -----------------------
goods-detection  CPU      1        goods_detection_cpu_x1
...
Excluded: 2 pipeline/variant(s)
pipeline          variant  reason              detail
----------------  -------  ------------------  ------------------------------------------
goods-detection  NPU      unsupported_family  host does not report ['NPU']
smart-parking     (all)    skip_pipelines      listed in benchmark.filters.skip_pipelines
```

If ViPPET cannot be reached, `--dry-run` exits with `2`. It exits with `0` in every
other case.

A normal run and direct `pytest --collect-only` print the same matrix after collection
(see [Console output](#console-output)).

#### Report only

`--report-only` rebuilds the HTML report from saved result JSON. It never
contacts ViPPET and never runs jobs.

```bash
python -m perf_helpers.cli --report-only                     # defaults to <results-dir>/latest
python -m perf_helpers.cli --report-only results/latest
python -m perf_helpers.cli --report-only results/bench_A/bench_A.json
python -m perf_helpers.cli --report-only results/bench_A results/bench_B --report-output /tmp/compare.html
make test-performance PERF_ARGS="--report-only"              # same as the first line, via make
```

The paths are relative to the current directory. A plain run stores its results in
`vippet/tests/performance/results/` (see [Reports](#reports)), so from the project
root use e.g. `vippet/tests/performance/results/latest`. Without a path,
`--report-only` reads `<results-dir>/latest`, where `--results-dir` / `PERF_RESULTS_DIR`
default to that same directory.

- Accepted inputs: a result JSON file, a single run directory, or the
  `latest` symlink. The whole results folder is not accepted.
- Several inputs render as one report with several runs; duplicate inputs
  are ignored. Charts and the summary table keep each run's measurements
  separate (an extra "Run" column/legend label appears) even when two runs
  share the same pipeline/variant/stream count, so comparisons across runs
  are not lost. Each run also gets its own system-info card (processor,
  GPU/NPU, memory, OS, discovered devices) labeled by run id, since runs
  being compared may come from different machines.
- Output location:
  - one run: overwrites `<run_dir>/<benchmark_id>.html`;
  - several runs: `<results-dir>/report_YYYYMMDD_HHMMSS.html`;
  - `--report-output PATH` overrides both.
- The JSON must have been saved by the run (`json` listed in
  `results.formats` / `--formats`). Results written before the `config` block
  existed still render, just without the **Configuration** section.
- The report has a **Configuration** section: each setting with its value and
  source, and per run the config name, code revisions, applied filters and excluded
  pipeline variants. With several runs, settings that differ between them are listed
  first and highlighted.
- Exit codes: `0` on success; `2` for a missing, unreadable or invalid
  input; `2` for `--report-output` without `--report-only`. If the
  implicit default (`<results-dir>/latest`) is missing, the error
  suggests running a benchmark first or passing an explicit path.

### Direct pytest

You can still call pytest directly. It reads the same YAML and env vars; there are no
CLI flags in this mode:

```bash
# Collect only (preview test matrix)
python -m pytest --collect-only vippet/tests/performance/

# Run a specific pipeline
python -m pytest -m perf -k "goods_detection" vippet/tests/performance/

# Generate JUnit XML for CI
python -m pytest -m perf --junitxml=results/perf.xml vippet/tests/performance/
```

### Console output

Every run (CLI or direct pytest) prints these sections in order:

1. **ViPPET readiness**: the `/health` and `/status` checks, then the `/devices`,
   capabilities and version snapshots used for the report.
1. **Discovered hardware**: the ViPPET version and the device names per CPU/GPU/NPU family.
1. **Benchmark matrix**: the same tables as `--dry-run`, covering runs, exclusions
   with reasons, and runs skipped for missing models.
1. **Per-test status**: one `PASSED`/`FAILED`/`SKIPPED` line per run (`-v` is set in
   `pytest.ini`; pass `-q` after `--` for dots).
1. **Benchmark artefacts**: the run directory, the JSON/CSV/HTML reports, the log file
   and the `latest` link, or why no reports were written. Also a warning if no
   hardware metrics were sampled.
1. pytest's short summary with every skip and failure reason.

The path of the log file is also printed in pytest's header (`benchmark log: ...`), so
it is known even when the run stops at the readiness check.

Logs are not streamed to the console by default. Every run writes its full INFO log to
`bench_<timestamp>.log` in its run directory (see [Reports](#reports)), and the INFO
log of a failed test is also shown in its "Captured log" block. Passing
`--log-cli-level=INFO` to pytest (after `--` for the CLI) streams logs live, which adds
`live log ...` blocks between these sections and splits each per-test status line
around the logs of its job. Per-request `httpx`/`httpcore` lines (e.g. job status polls
and metrics-manager samples) are kept out of the console below WARNING, but they are
written to the log file.

Every error says what happened, why, and how to fix it ("To fix: ..."), naming the
flag, env var or file to change. The exit codes are listed in
[Exit codes](#exit-codes).

## Benchmark your own pipeline

No source change and no harness configuration is needed to benchmark a pipeline you
created yourself. At every run the suite asks ViPPET for its pipelines (`GET /pipelines`),
so a pipeline created in ViPPET (in the UI, or with `POST /pipelines`) is discovered and
benchmarked exactly like a predefined one: same variants, stream counts, retries, result
export and reports.

### 1. Prepare the pipeline in ViPPET

- **Variant names decide what can run.** Name each variant after the device(s) it uses:
  `CPU`, `GPU`, `NPU`, or a combination joined by `_` such as `GPU_NPU`. A variant with
  any other name is excluded as `unsupported_family`.
- **Install the models** the pipeline uses (ViPPET UI Models page, or `POST /models/download`).
  Runs of a pipeline with missing models are skipped, with the model names shown.
  `--no-skip-missing-models` runs them anyway, and they will most likely fail.

### 2. Find the pipeline id

Pipelines are selected by their **id**, which ViPPET derives from the pipeline name: a
lowercase slug (`Vehicle Detection` becomes `vehicle-detection`), with a short hash
appended if that id is already taken. The `pipeline` column of the dry-run matrix shows
the ids of all discovered pipelines:

```bash
python -m perf_helpers.cli --dry-run
```

The id is also the `id` field of the pipeline in the `GET /pipelines` response.

### 3. Select it

| Goal                                     | Command                                                            |
|------------------------------------------|--------------------------------------------------------------------|
| Your pipeline only                       | `--pipelines vehicle-detection`                                    |
| Several of your pipelines                | `--pipelines vehicle-detection,my-other-pipeline`                  |
| Your pipeline together with all defaults | no `--pipelines` (the default is `'*'`: everything ViPPET reports) |
| Your pipeline with selected defaults     | `--pipelines vehicle-detection,object-detection`                   |
| Everything except some pipelines         | `--skip-pipelines smart-parking`                                   |

The same selection can be saved in a YAML file. Keys you leave out keep their defaults:

```yaml
# my-config.yaml
benchmark:
  pipelines: ["vehicle-detection"]   # or "*" for customer plus predefined pipelines
  variants: [cpu, gpu]
  stream_counts: [1, 2, 4]
```

```bash
python -m perf_helpers.cli --config ./my-config.yaml
```

Ids in `--pipelines` and `--skip-pipelines` are matched case-insensitively. An id that
ViPPET does not know stops the run before any job starts, with exit
code `4`, and the error lists the valid ids. With `--on-unknown-id warn`
(`PERF_ON_UNKNOWN_ID=warn`) such an id is reported as a skipped case and the rest runs.

### 4. Confirm with a dry run

```bash
python -m perf_helpers.cli --dry-run --pipelines vehicle-detection --variants cpu,gpu --streams 1,2
```

```text
Host device families: CPU, GPU

Matrix: 4 run(s) = pipeline x variant x streams [1, 2]
pipeline           variant  streams  test id
-----------------  -------  -------  ------------------------
vehicle-detection  CPU      1        vehicle_detection_cpu_x1
vehicle-detection  CPU      2        vehicle_detection_cpu_x2
vehicle-detection  GPU      1        vehicle_detection_gpu_x1
vehicle-detection  GPU      2        vehicle_detection_gpu_x2

Excluded: 11 pipeline/variant(s)
pipeline          variant  reason           detail
----------------  -------  ---------------  -----------------------------------
object-detection  (all)    pipeline_filter  not listed in benchmark.pipelines
...
```

Your pipeline must appear in the **Matrix**. If it does not, look it up in **Excluded**:

| What the dry run shows                               | Cause and fix                                                                                     |
|------------------------------------------------------|---------------------------------------------------------------------------------------------------|
| Pipeline in neither table                            | ViPPET does not report it. Check that it exists and that `--base-url` points to the right ViPPET. |
| `pipeline_filter`                                    | Its id is not in `--pipelines`.                                                                   |
| `skip_pipelines` / `skip_variants`                   | Remove it from `--skip-pipelines` / `--skip-variants`.                                            |
| `variant_filter`                                     | The variant needs a device family that is missing in `--variants`. Add it.                        |
| `unsupported_family`, "unknown device family"        | The variant name is not CPU, GPU, NPU or a combination of them. Rename the variant in ViPPET.     |
| `unsupported_family`, "host does not report"         | The host has no such device. Nothing to fix; the variant cannot run here.                         |
| In the Matrix and also under **Skipped at run time** | Models are missing. Install them in ViPPET.                                                       |

### 5. Run it

```bash
python -m perf_helpers.cli --pipelines vehicle-detection --variants cpu,gpu --streams 1,2,4
make test-performance PERF_ARGS="--pipelines vehicle-detection --streams 1,2,4"   # via make
```

The pipeline then appears in the console matrix and logs, and in the JSON, CSV and HTML
results, under the name and id that ViPPET reports. Open the HTML report from the
**Benchmark artefacts** section of the console output. To rebuild it later, see
[Report only](#report-only).

## CLI reference

```text
python -m perf_helpers.cli [OPTIONS] [-- PYTEST_ARGS]
```

`python -m perf_helpers.cli --help` lists the options in the same (alphabetical) order.
Run it from the project root with the virtual environment active and `PYTHONPATH` set,
see [Setup](#setup). `make test-performance PERF_CONFIG=<config> PERF_ARGS="<options>"`
runs the same CLI.

### Run modes and general options

| Option                     | Description                                                                                                                                 |
|----------------------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| `--config NAME_OR_PATH`    | Preset name in `config/` (`default`, `quick`, `full`), a YAML path, or a result JSON / run directory. See [Configuration](#configuration).  |
| `--dry-run`                | Print the settings, the matrix and the exclusions, then exit without running jobs. See [Dry run](#dry-run). Exclusive with `--report-only`. |
| `-h`, `--help`             | Show the option list and exit.                                                                                                              |
| `--report-only [PATH ...]` | Rebuild the HTML report from saved result JSON, without contacting ViPPET. See [Report only](#report-only). Exclusive with `--dry-run`.     |
| `--report-output PATH`     | HTML file to write with `--report-only`. Requires `--report-only`.                                                                          |
| `-- PYTEST_ARGS`           | Everything after `--` goes to pytest unchanged, e.g. `-- -k vehicle_detection --log-cli-level=INFO`.                                        |

Without `--dry-run` and `--report-only` the CLI runs the benchmark.

### Settings

Every setting has a CLI flag and a YAML key, and some have an environment variable. A
flag overrides the environment variable, which overrides the YAML file, which overrides
the built-in default. The defaults below are those of the `default` preset.

| Flag                                                | Default                                           | YAML key                                  | Env var                      | Meaning                                                                     |
|-----------------------------------------------------|---------------------------------------------------|-------------------------------------------|------------------------------|-----------------------------------------------------------------------------|
| `--base-url URL`                                    | `http://localhost/api/v1`                         | `vippet.base_url`                         | `VIPPET_BASE_URL`            | ViPPET API base URL                                                         |
| `--capabilities-url URL`                            | `http://<base-url host>:9090/api/v1/capabilities` | `metrics.capabilities_url`                | `VIPPET_CAPABILITIES_URL`    | metrics-manager capabilities endpoint (not behind the nginx proxy)          |
| `--formats FORMATS`                                 | `json,csv`                                        | `results.formats`                         |                              | Export formats: `json`, `csv`. The HTML report is always written.           |
| `--latest-link`, `--no-latest-link`                 | on                                                | `results.create_latest_link`              |                              | Point `<results-dir>/latest` at the newest run                              |
| `--max-job-duration SECONDS`                        | `600`                                             | `vippet.max_job_duration`                 | `VIPPET_JOB_TIMEOUT_SECONDS` | Longest wait for one job, whole seconds                                     |
| `--max-retries N`                                   | `2`                                               | `benchmark.execution.max_retries`         |                              | Retries for a job that does not complete                                    |
| `--max-runtime SECONDS`                             | `30`                                              | `benchmark.execution.max_runtime`         |                              | How long each run lasts; `0` runs until the end of the video                |
| `--metrics-interval SECONDS`                        | `2.0`                                             | `metrics.sample_interval_seconds`         | `PERF_METRICS_INTERVAL`      | Hardware metrics sampling interval                                          |
| `--metrics-trim-fraction FRACTION`                  | `0.1`                                             | `metrics.trim_fraction`                   | `PERF_METRICS_TRIM_FRACTION` | Share of samples trimmed from each end before averaging, `0` to below `0.5` |
| `--metrics-url URL`                                 | `http://<base-url host>/metrics/stream`           | `metrics.metrics_url`                     | `VIPPET_METRICS_URL`         | Hardware metrics endpoint                                                   |
| `--on-unknown-id {fail,warn}`                       | `fail`                                            | `benchmark.filters.on_unknown_id`         | `PERF_ON_UNKNOWN_ID`         | Unknown id in a filter: abort the run, or warn and skip it                  |
| `--output-mode {disabled,file,live_stream}`         | `disabled`                                        | `benchmark.execution.output_mode`         |                              | Pipeline output mode                                                        |
| `--pipelines IDS`                                   | `*`                                               | `benchmark.pipelines`                     |                              | `*` for all pipelines, or comma-separated pipeline ids                      |
| `--poll-interval SECONDS`                           | `2`                                               | `vippet.poll_interval`                    | `VIPPET_JOB_POLL_INTERVAL`   | Job status polling interval                                                 |
| `--readiness-timeout SECONDS`                       | `60`                                              | `vippet.readiness_timeout_seconds`        |                              | Longest wait for ViPPET to become ready before the run starts               |
| `--results-dir DIR`                                 | `vippet/tests/performance/results`                | `results.output_dir`                      | `PERF_RESULTS_DIR`           | Directory that receives the run directories                                 |
| `--retry-delay SECONDS`                             | `5`                                               | `benchmark.execution.retry_delay_seconds` |                              | Pause between retries                                                       |
| `--save-log`, `--no-save-log`                       | on                                                | `results.save_log`                        |                              | Write the run's INFO log next to its reports                                |
| `--skip-missing-models`, `--no-skip-missing-models` | on                                                | `benchmark.filters.skip_missing_models`   |                              | Skip pipelines with missing models, or run them anyway                      |
| `--skip-pipelines IDS`                              | none                                              | `benchmark.filters.skip_pipelines`        |                              | Comma-separated pipeline ids to leave out; `''` clears the list             |
| `--skip-variants VARIANTS`                          | none                                              | `benchmark.filters.skip_variants`         |                              | Comma-separated variant names to leave out, e.g. `GPU_NPU`                  |
| `--streams COUNTS`                                  | `1,3`                                             | `benchmark.stream_counts`                 |                              | Comma-separated stream counts                                               |
| `--timeout SECONDS`                                 | `600`                                             | `vippet.timeout`                          |                              | HTTP request timeout                                                        |
| `--variants FAMILIES`                               | `cpu,gpu,npu`                                     | `benchmark.variants`                      |                              | Allowed device families (see [Configuration](#configuration))               |

### Presets

| Preset    | Variants      | Streams     | `max_runtime`      | Formats   | Other differences                                              |
|-----------|---------------|-------------|--------------------|-----------|----------------------------------------------------------------|
| `default` | CPU, GPU, NPU | 1, 3        | 30 s               | JSON, CSV |                                                                |
| `quick`   | CPU, GPU      | 1           | 10 s               | JSON      | `timeout` and `max_job_duration` 300, 1 retry, 3 s retry delay |
| `full`    | CPU, GPU, NPU | 1, 3, 5, 10 | until end of video | JSON, CSV |                                                                |

### Exit codes

| Exit code | Cause                                                                                                                                                                                   |
|-----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `0`       | All benchmarks passed, or `--dry-run` / `--report-only` succeeded                                                                                                                       |
| `1`       | At least one benchmark failed (state, attempts, job id and the fix are shown per test)                                                                                                  |
| `2`       | Invalid config or flag; ViPPET unreachable, not the ViPPET API, or not ready within `--readiness-timeout`; invalid input for `--report-only`; `--report-output` without `--report-only` |
| `4`       | A filter id in `pipelines` / `variants` / `skip_*` matches nothing and `on_unknown_id` is `fail` (default)                                                                              |

Other exit codes come from pytest itself, which the CLI passes through.

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

Settings are combined in this order. Each layer overrides the one before it:

```text
built-in defaults -> YAML config -> environment variables -> CLI flags
```

The YAML file is chosen in this order: `--config`, then `PERF_CONFIG_FILE` (a path),
then `PERF_CONFIG` (a preset name in `config/` or a path), then `default`. An unknown
preset, a missing file, an unknown YAML key or an invalid value stops the run with an
error. Older versions silently fell back to `default.yaml` instead. `--config` (and
`PERF_CONFIG`) can also name a result JSON or run directory, see
[Reproducing a run](#reproducing-a-run).

Every YAML key has a CLI flag. The [CLI reference](#cli-reference) lists all flags with their
YAML keys, environment variables and defaults. `--config` is the only flag without a YAML
key; the environment variables `PERF_CONFIG_FILE` and `PERF_CONFIG` select the YAML file the
same way.

Notes:

- List flags take comma-separated values, e.g. `--streams 1,3,5` or `--pipelines a,b`.
  `--pipelines '*'` selects all pipelines. Pass `--skip-pipelines ''` to clear a list.
- `--variants` lists the allowed device **families**. A variant runs only if every
  family it uses is allowed: `GPU_NPU` needs both `gpu` and `npu`. So listing
  `gpu_npu` is redundant once `gpu` and `npu` are listed (as in `full.yaml`).
- `--no-skip-missing-models` lets pipelines with missing models run anyway
  (and fail at runtime) instead of being skipped with the missing-model
  reason shown.
- `--metrics-url`/`--capabilities-url` default to `--base-url`'s host (same
  host/port for metrics, same host on port 9090 for capabilities), so setting
  only `--base-url` (or `VIPPET_BASE_URL`) is enough to target a remote
  device; set either flag explicitly to override its default.
- A relative `--results-dir` / `results.output_dir` / `PERF_RESULTS_DIR`
  value is resolved against the current working directory, like any other
  path argument. If you don't set it, it defaults to `results` inside
  `vippet/tests/performance` (an absolute path), so a benchmark run and a
  later `--report-only` agree on where results live regardless of which
  directory the CLI is invoked from — as long as you don't override it
  with a relative path from a different directory.
- The CLI always passes `vippet/tests/performance` to pytest, so arguments after
  `--` can't move collection elsewhere. Paths or node ids after `--` don't narrow
  the run; use `--pipelines`, `--variants`, `--streams` or `-k` instead.

Environment variable defaults:

| Env var                      | Default                                          | Description                                                                                                                                                               |
|------------------------------|--------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `VIPPET_BASE_URL`            | `http://localhost/api/v1`                        | VIPPET API endpoint. Its host (and port) is reused to build the default `VIPPET_METRICS_URL`/`VIPPET_CAPABILITIES_URL` below, so set this alone to target a remote device |
| `VIPPET_METRICS_URL`         | derived from `VIPPET_BASE_URL`'s host/port       | Metrics endpoint (via nginx proxy); set explicitly to override                                                                                                            |
| `VIPPET_CAPABILITIES_URL`    | derived from `VIPPET_BASE_URL`'s host, port 9090 | metrics-manager capabilities endpoint (direct, not nginx-proxied); set explicitly to override                                                                             |
| `VIPPET_JOB_POLL_INTERVAL`   | `2.0`                                            | Job status polling interval (seconds)                                                                                                                                     |
| `VIPPET_JOB_TIMEOUT_SECONDS` | `600`                                            | Max wait for a job, whole seconds                                                                                                                                         |
| `PERF_CONFIG`                | `default`                                        | Config preset (`default`, `quick`, `full`)                                                                                                                                |
| `PERF_CONFIG_FILE`           | (unset)                                          | Explicit YAML path; wins over `PERF_CONFIG`                                                                                                                               |
| `PERF_RESULTS_DIR`           | `vippet/tests/performance/results`               | Output directory for reports                                                                                                                                              |
| `PERF_METRICS_INTERVAL`      | `2.0`                                            | HW sampling interval (seconds)                                                                                                                                            |
| `PERF_METRICS_TRIM_FRACTION` | `0.1`                                            | Fraction of HW samples trimmed from each end before averaging (see [HW metric trimming](#hw-metric-trimming))                                                             |
| `PERF_ON_UNKNOWN_ID`         | `fail`                                           | Unknown id in `pipelines` / `skip_pipelines` / `skip_variants` / `variants`: `fail` aborts the run, `warn` only logs a warning                                            |

`VIPPET_JOB_POLL_INTERVAL` and `VIPPET_JOB_TIMEOUT_SECONDS` are also read by the
functional-test helpers. The CLI and direct pytest both resolve them the same way.
Numbers must be finite (`nan` and `inf` are rejected). With direct pytest, an invalid
config also prints a one-line error and exits with `2`.

### Reproducing a run

Each benchmark run saves a result JSON (`results.formats` must include `json`).
Its `config` block records the settings the run used (see [Reports](#reports)).
Pass the JSON, or its run directory, as the config to run the same benchmark again.
Run these from the project root (`tools/visual-pipeline-and-platform-evaluation-tool`),
because paths are resolved against the current working directory:

```bash
# via make (sets PYTHONPATH for you)
make test-performance PERF_CONFIG=vippet/tests/performance/results/bench_20261020_154904

# via the CLI directly (venv active, PYTHONPATH set as in the "CLI" section)
python -m perf_helpers.cli --config vippet/tests/performance/results/latest --dry-run
```

- The recorded `config.values` replace the YAML layer, so every setting shows source
  `yaml`. Env vars and CLI flags still override them.
- The recorded metrics URLs are reused as they are. When you change `--base-url`, also
  pass `--metrics-url` and `--capabilities-url`.
- What runs is decided by the current host as usual: cases whose models are missing
  are skipped (`benchmark.filters.skip_missing_models`), and models installed since
  the recording are used. The `config` block does not record the host's matrix.
- A result written by a suite version with different setting keys is refused
  (exit `2`); copy its settings into a YAML config instead.

### HW metric trimming

`metrics.trim_fraction` (default `0.1`, override with `PERF_METRICS_TRIM_FRACTION`)
trims that fraction of time-ordered hardware samples from **both ends** before
computing each KPI's average — the same edge trimming VIPPET's own built-in
benchmark applies (`managers.benchmark_metrics.TRIM_FRACTION`), so warm-up and
wind-down samples don't drag the reported average down. `_min`/`_max` are
always computed from the **untrimmed** samples, so a real transient peak
still shows in the report.

With the default 2s sampling interval and a 30s run (~16 samples), a 10% trim
only drops one sample from each end — some startup idle may still be present
in the average. This matches VIPPET's built-in benchmark behavior today.

The value must satisfy `0 <= trim_fraction < 0.5` (`0` disables trimming).

### Per-family KPI status (`ok` / `no_samples` / `idle`)

For each device family requested by a test case (derived from
`pipeline_case.device_family`, e.g. `GPU_NPU` → `GPU`, `NPU`), the HW monitor
classifies that family's activity across the run:

| Status       | Meaning                                                                                                                                                                                                                                           |
|--------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `ok`         | At least one activity KPI (e.g. `npu_utilization`, falling back to `npu_frequency_mhz`/`npu_power` for NPU) was nonzero somewhere in the run. `npu_frequency_mhz` is derived from the metrics-manager `npu_frequency_hz` metric divided by 1e6.   |
| `no_samples` | None of the family's KPI keys were ever reported by metrics-manager — the device is missing or the harness never got a successful poll.                                                                                                           |
| `idle`       | Samples exist for the family, but every activity value was zero for the entire run (hardware present but never dispatched work).                                                                                                                  |

A requested family that isn't `ok` triggers both a `logger.warning` and a
`warnings.warn` (so it's visible in the pytest warnings summary), and shows up
in the reports instead of a blank cell:

- **CSV**: a `no_samples` family's `_avg`/`_max` cells show the literal
  `NO_SAMPLES` marker instead of an empty string. An `idle` family keeps its
  real (zero) numeric values — only the `kpi_warnings` column
  (e.g. `NPU:idle` or `NPU:no_samples;GPU:idle`) flags it.
- **HTML**: the per-pipeline table shows a small amber "no samples" / "idle"
  warning badge next to the affected family's utilization cell.
- **JSON**: numeric KPI values stay numeric; the status lives in
  `hw_metrics.kpi_status` (per-family) and `hw_metrics.kpi_warnings`
  (semicolon-joined summary string).

### NPU settle sample

The first metrics-manager poll of a test case can still carry NPU readings from
the previous test case, because NPU telemetry is sampled over the preceding
interval. When a run has at least 3 samples, the HW monitor drops only the NPU
keys (`npu_utilization`, `npu_power`, `npu_frequency_mhz`, `npu_temperature`,
`npu_memory_mb`, `npu_bandwidth`) from the first sample before it computes the NPU
`_avg`/`_min`/`_max`, `kpi_status` and `npu_sample_count`. A first sample with NPU
activity followed only by zeros is therefore reported as `NPU:idle`. CPU and GPU
values of that sample are kept, and the edge trimming above is unchanged.
`hw_metrics.npu_settle_samples_dropped` (`0` or `1`) records whether this happened;
`sample_count` / `hw_sample_count` still counts every poll.

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
├── cli.py                  # CLI entry point (python -m perf_helpers.cli)
├── config.py               # module constants derived from settings.py
├── discovery.py            # read-only ViPPET queries feeding matrix.py
├── hw_monitor.py           # background HW metric sampler + per-family status
├── job_outcome.py          # actionable messages for failed/rejected/empty jobs
├── matrix.py               # pure matrix builder, exclusion reasons, matrix rendering, filter-id checks
├── preflight.py            # readiness checks + /devices, /capabilities, /status snapshots, hardware lines
├── reporters.py            # JSON/CSV export + HTML report generation (incl. the Configuration section)
├── run_config.py           # builds the result "config" block
├── settings.py             # setting specs + defaults -> YAML -> env -> CLI resolver
└── system_info.py          # builds the report's System info block
config/
├── default.yaml            # CPU+GPU+NPU, 1 & 3 streams
├── quick.yaml              # CPU+GPU, 1 stream
└── full.yaml               # all variants, 1/3/5/10 streams
```

## Reports

Each run gets its own directory, `results/bench_YYYYMMDD_HHMMSS/`. The timestamp is the
run's start time and every file in the directory uses it, so the reports and the log of
one benchmark always belong together (the JSON `timestamp` is the same start time). The
exact paths are listed in the run's **Benchmark artefacts** section:

- `.json` — structured results (all test cases + HW metrics) and a `config` block:
  the config name (`selector`), every resolved setting (`values`) with its `sources`
  (`default`, `yaml`, `env` or `cli`), the applied `filters` with the excluded
  pipeline variants, and the code `revisions` (suite git commit, ViPPET version)
- `.csv` — flat table for spreadsheet analysis
- `.html` — interactive Chart.js report with FPS, utilization, and power charts
- `.log` — the full INFO log of the run, including the per-request HTTP lines hidden from
  the console (`results.save_log`, on by default)

```text
results/
├── bench_20261020_154904/
│   ├── bench_20261020_154904.json
│   ├── bench_20261020_154904.html
│   └── bench_20261020_154904.log
└── latest -> bench_20261020_154904
```

At the end of every run, `latest` is pointed at the run directory, including runs that
failed or stopped at the readiness check, because it is still the latest run. With
`--no-save-log` a run that writes no reports creates no directory and leaves `latest`
unchanged.

Log options (arguments after `--` go to pytest):

```bash
# More detail in the log file
python -m perf_helpers.cli -- --log-file-level=DEBUG
# Write the log somewhere else (no log in the run directory then)
python -m perf_helpers.cli -- --log-file=/tmp/perf.log
# No log file
python -m perf_helpers.cli --no-save-log
```

Rebuild the HTML later with `--report-only` (see [Report only](#report-only)).
