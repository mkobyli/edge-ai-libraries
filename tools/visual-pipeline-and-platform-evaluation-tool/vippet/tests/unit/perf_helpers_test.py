# Copyright (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""Offline regression tests for the external performance harness."""

import csv
import json
import os
from copy import deepcopy
from pathlib import Path
from typing import Any
from unittest.mock import Mock

import pytest

from tests.performance.perf_helpers import config as perf_config
from tests.performance.perf_helpers import hw_monitor
from tests.performance.perf_helpers.hw_monitor import HardwareMonitor
from tests.performance.perf_helpers.reporters import (
    CSVReporter,
    JSONReporter,
    generate_html_report,
)

pytest_plugins = ["pytester"]


@pytest.fixture
def report_result() -> dict[str, Any]:
    return {
        "benchmark_id": "report_test",
        "test_cases": [
            {
                "pipeline_name": "Test pipeline",
                "pipeline_id": "test_pipeline",
                "variant_name": "NPU",
                "variant_id": "npu",
                "streams": 1,
                "status": "success",
                "total_fps": 20,
                "per_stream_fps": 20,
                "hw_metrics": {},
            }
        ],
    }


def test_csv_exports_mhz_warnings_and_distinguishes_missing_from_zero(
    tmp_path: Path,
) -> None:
    metrics: list[dict[str, Any]] = [
        {
            "cpu_util_pct_avg": 0,
            "npu_temperature_avg": 32,
            "kpi_status": {"CPU": "idle", "NPU": "no_samples"},
            "kpi_warnings": "CPU:idle;NPU:no_samples",
        },
        {
            "npu_utilization_avg": 0,
            "npu_frequency_mhz_avg": 0,
            "kpi_status": {"NPU": "idle"},
            "kpi_warnings": "NPU:idle",
        },
        {
            "npu_frequency_mhz_avg": 1600,
            "npu_frequency_mhz_max": 1700,
            "kpi_status": {"NPU": "ok"},
            "kpi_warnings": "",
        },
        {"kpi_status": {"GPU": "ok"}},
    ]
    output = tmp_path / "result.csv"

    CSVReporter.save({"test_cases": [{"hw_metrics": hw} for hw in metrics]}, output)

    with output.open(newline="") as report:
        rows = list(csv.DictReader(report))
    assert rows[0]["npu_utilization_avg"] == "NO_SAMPLES"
    assert rows[0]["npu_frequency_mhz_avg"] == "NO_SAMPLES"
    assert rows[0]["npu_temperature_avg"] == "32.00"
    assert rows[0]["cpu_util_pct_avg"] == "0.00"
    assert rows[0]["kpi_warnings"] == "CPU:idle;NPU:no_samples"
    assert rows[1]["npu_utilization_avg"] == rows[1]["npu_frequency_mhz_avg"] == "0.00"
    assert rows[1]["npu_temperature_avg"] == ""
    assert rows[2]["npu_frequency_mhz_avg"] == "1600.00"
    assert rows[2]["npu_frequency_mhz_max"] == "1700.00"
    assert rows[2]["kpi_warnings"] == ""
    assert rows[3]["npu_utilization_avg"] == rows[3]["kpi_warnings"] == ""
    assert "npu_frequency_avg" not in rows[0]
    assert "npu_frequency_max" not in rows[0]


@pytest.mark.parametrize("family", ["CPU", "GPU", "NPU"])
def test_csv_marks_only_missing_columns_of_requested_family(
    tmp_path: Path,
    family: str,
) -> None:
    output = tmp_path / "result.csv"
    CSVReporter.save(
        {
            "test_cases": [
                {
                    "hw_metrics": {
                        "kpi_status": {family: "no_samples"},
                    }
                }
            ]
        },
        output,
    )

    with output.open(newline="") as report:
        row = next(csv.DictReader(report))

    for candidate, keys in hw_monitor.FAMILY_KPI_KEYS.items():
        for key in keys:
            for suffix in ("_avg", "_max"):
                column = key + suffix
                if column in row:
                    assert row[column] == ("NO_SAMPLES" if candidate == family else "")


@pytest.mark.parametrize(
    ("family", "metric"),
    [
        ("CPU", "cpu_util_pct_avg"),
        ("GPU", "gpu_util_combined_avg"),
        ("NPU", "npu_utilization_avg"),
    ],
)
@pytest.mark.parametrize("status", ["ok", "idle", "no_samples"])
def test_html_marks_family_status_without_replacing_numeric_values(
    report_result: dict[str, Any],
    family: str,
    metric: str,
    status: str,
) -> None:
    metrics: dict[str, Any] = {"kpi_status": {family: status}}
    if status != "no_samples":
        metrics[metric] = 0 if status == "idle" else 12
    report_result["test_cases"][0]["hw_metrics"] = metrics

    report = generate_html_report([report_result])

    label = "no samples" if status == "no_samples" else status
    badge = f' <span class="badge warn">{label}</span>' if status != "ok" else ""
    value = (
        "\u2014"
        if status == "no_samples"
        else ("0.0%" if status == "idle" else "12.0%")
    )
    assert f'<td class="r">{value}{badge}</td>' in report
    assert report.count('class="badge warn"') == (0 if status == "ok" else 1)


@pytest.mark.parametrize("frequency", [0, 1600])
def test_html_shows_npu_frequency_in_mhz(
    report_result: dict[str, Any], frequency: float
) -> None:
    report_result["test_cases"][0]["hw_metrics"] = {"npu_frequency_mhz_avg": frequency}

    report = generate_html_report([report_result])

    assert '<th style="text-align:right">NPU Freq (MHz)</th>' in report
    assert f'<td class="r">{frequency:.1f} MHz</td>' in report


def test_exports_keep_json_numbers_statuses_and_warnings_separate(
    tmp_path: Path,
    report_result: dict[str, Any],
) -> None:
    report_result["test_cases"][0]["hw_metrics"] = {
        "sample_count": 3,
        "npu_sample_count": 2,
        "npu_settle_samples_dropped": 1,
        "npu_utilization_avg": 0,
        "npu_frequency_mhz_avg": 1600,
        "cpu_temperature_avg": 40,
        "kpi_status": {"NPU": "ok", "CPU": "no_samples"},
        "kpi_warnings": "CPU:no_samples",
    }
    expected = deepcopy(report_result)
    output = tmp_path / "result.json"

    CSVReporter.save(report_result, tmp_path / "result.csv")
    generate_html_report([report_result])
    JSONReporter.save(report_result, output)

    saved = json.loads(output.read_text())
    assert saved == expected
    assert report_result == expected
    metrics = saved["test_cases"][0]["hw_metrics"]
    assert isinstance(metrics["npu_frequency_mhz_avg"], (int, float))
    assert metrics["npu_utilization_avg"] == 0
    assert "cpu_util_pct_avg" not in metrics
    assert metrics["kpi_status"] == {"NPU": "ok", "CPU": "no_samples"}
    assert metrics["kpi_warnings"] == "CPU:no_samples"


def test_npu_settle_sample_preserves_cpu_gpu_and_global_count() -> None:
    monitor = HardwareMonitor()
    monitor._samples = [
        {
            "npu_utilization": 90,
            "npu_frequency_mhz": 1600,
            "npu_power": 4,
            "cpu_util_pct": 60,
            "gpu_render_util_pct": 30,
        },
        {
            "npu_utilization": 0,
            "npu_frequency_mhz": 0,
            "npu_power": 0,
            "cpu_util_pct": 0,
            "gpu_render_util_pct": 0,
        },
        {
            "npu_utilization": 0,
            "npu_frequency_mhz": 0,
            "npu_power": 0,
            "cpu_util_pct": 0,
            "gpu_render_util_pct": 0,
        },
    ]

    stats = monitor._aggregate(["CPU", "GPU", "NPU"])

    for metric in ("npu_utilization", "npu_frequency_mhz", "npu_power"):
        assert stats[f"{metric}_avg"] == 0
        assert stats[f"{metric}_min"] == 0
        assert stats[f"{metric}_max"] == 0
    assert stats["npu_settle_samples_dropped"] == 1
    assert stats["sample_count"] == 3
    assert stats["npu_sample_count"] == 2
    assert stats["cpu_sample_count"] == stats["gpu_sample_count"] == 3
    assert stats["kpi_status"] == {"CPU": "ok", "GPU": "ok", "NPU": "idle"}
    assert stats["kpi_warnings"] == "NPU:idle"
    assert stats["cpu_util_pct_avg"] == pytest.approx(20)
    assert stats["gpu_render_util_pct_avg"] == pytest.approx(10)
    assert monitor._samples[0]["npu_utilization"] == 90
    assert monitor._samples[0]["npu_frequency_mhz"] == 1600
    assert monitor._samples[0]["npu_power"] == 4


@pytest.mark.parametrize("frequency", [None, 0, 1_600_000_000])
def test_collect_converts_npu_hz_to_mhz(
    monkeypatch: pytest.MonkeyPatch,
    frequency: float | None,
) -> None:
    metrics: dict[str, float] = {"npu_utilization": 0, "npu_power": 0}
    if frequency is not None:
        metrics["npu_frequency"] = frequency
    monkeypatch.setattr(hw_monitor, "_fetch_metrics_manager", lambda url: metrics)

    sample = HardwareMonitor()._collect()

    assert sample["npu_utilization"] == sample["npu_power"] == 0
    assert "npu_frequency" not in sample
    if frequency is None:
        assert "npu_frequency_mhz" not in sample
    else:
        assert sample["npu_frequency_mhz"] == frequency / 1e6


@pytest.mark.parametrize("count", [1, 2])
def test_short_runs_keep_first_npu_sample(count: int) -> None:
    monitor = HardwareMonitor()
    monitor._samples = [{"npu_utilization": 90.0}] + [
        {"npu_utilization": 0.0} for _ in range(count - 1)
    ]

    stats = monitor._aggregate(["NPU"])

    assert stats["npu_utilization_avg"] == 90 / count
    assert stats["npu_utilization_max"] == 90
    assert stats["npu_sample_count"] == count
    assert stats["npu_settle_samples_dropped"] == 0
    assert stats["kpi_status"] == {"NPU": "ok"}


def test_missing_first_npu_sample_does_not_drop_later_readings() -> None:
    monitor = HardwareMonitor()
    monitor._samples = [
        {"cpu_util_pct": 10},
        {"npu_utilization": 60},
        {"npu_utilization": 0},
    ]

    stats = monitor._aggregate(["NPU"])

    assert stats["npu_settle_samples_dropped"] == 0
    assert stats["npu_sample_count"] == 2
    assert stats["npu_utilization_avg"] == 30


@pytest.mark.parametrize(
    ("family", "sample", "status", "count"),
    [
        ("CPU", {}, "no_samples", 0),
        ("CPU", {"cpu_util_pct": 0}, "idle", 1),
        ("CPU", {"cpu_util_pct": 1}, "ok", 1),
        ("CPU", {"cpu_temperature": 40}, "no_samples", 1),
        ("GPU", {"cpu_util_pct": 10}, "no_samples", 0),
        ("GPU", {"gpu_compute_util_pct": 5}, "ok", 1),
        ("GPU", {"gpu_video_util_pct": 5}, "ok", 1),
        ("GPU", {"gpu_enhance_util_pct": 5}, "ok", 1),
        ("GPU", {"gpu_copy_util_pct": 5}, "ok", 1),
        ("GPU", {"gpu_render_util_pct": 0}, "idle", 1),
        ("NPU", {}, "no_samples", 0),
        ("NPU", {"npu_utilization": 0}, "idle", 1),
        ("NPU", {"npu_utilization": 1}, "ok", 1),
        ("NPU", {"npu_utilization": 0, "npu_frequency_mhz": 1600}, "ok", 1),
        ("NPU", {"npu_power": 2}, "ok", 1),
        ("NPU", {"npu_frequency_mhz": 0, "npu_power": 0}, "idle", 1),
        ("NPU", {"npu_memory_mb": 20}, "no_samples", 1),
    ],
)
def test_requested_family_status_and_sample_count(
    family: str,
    sample: dict[str, float],
    status: str,
    count: int,
) -> None:
    monitor = HardwareMonitor()
    monitor._samples = [sample]

    stats = monitor._aggregate([family])

    assert stats["kpi_status"] == {family: status}
    assert stats[f"{family.lower()}_sample_count"] == count
    assert stats["kpi_warnings"] == ("" if status == "ok" else f"{family}:{status}")


def test_trimming_is_temporal_and_preserves_extrema_counts_and_activity() -> None:
    monitor = HardwareMonitor(trim_fraction=0.2)
    monitor._samples = [
        {"cpu_util_pct": value, "gpu_render_util_pct": value, "gpu_video_util_pct": 0}
        for value in [20, 0, 100, 10, 90, 30]
    ]
    monitor._samples[0]["gpu_compute_util_pct"] = 5

    stats = monitor._aggregate(["CPU", "GPU"])

    assert stats["cpu_util_pct_avg"] == stats["gpu_util_combined_avg"] == 50
    assert stats["cpu_util_pct_min"] == 0
    assert stats["cpu_util_pct_max"] == stats["gpu_util_combined_max"] == 100
    assert stats["gpu_compute_util_pct_avg"] == 5
    assert (
        stats["sample_count"]
        == stats["cpu_sample_count"]
        == stats["gpu_sample_count"]
        == 6
    )
    assert stats["kpi_status"] == {"CPU": "ok", "GPU": "ok"}


def test_trimming_falls_back_when_optional_key_has_only_edge_samples() -> None:
    monitor = HardwareMonitor()
    monitor._samples = [{"cpu_util_pct": 0} for _ in range(10)]
    monitor._samples[0].update({"gpu_render_util_pct": 20, "cpu_temperature": 40})
    monitor._samples[-1]["gpu_video_util_pct"] = 30

    stats = monitor._aggregate(["GPU"])

    assert stats["cpu_temperature_avg"] == 40
    assert stats["gpu_util_combined_avg"] == 25
    assert stats["gpu_util_combined_max"] == 30
    assert stats["gpu_sample_count"] == 2
    assert stats["kpi_status"] == {"GPU": "ok"}


def test_trimming_zero_disables_trimming() -> None:
    monitor = HardwareMonitor(trim_fraction=0)
    monitor._samples = [{"cpu_util_pct": value} for value in range(10)]
    assert monitor._aggregate()["cpu_util_pct_avg"] == 4.5


@pytest.mark.parametrize(
    "fraction", [-0.01, 0.5, 1, float("nan"), float("inf"), -float("inf")]
)
def test_monitor_rejects_invalid_trim_fraction(fraction: float) -> None:
    with pytest.raises(ValueError, match="0 <= value < 0.5"):
        HardwareMonitor(trim_fraction=fraction)


def test_empty_monitor_reports_missing_requested_families() -> None:
    stats = HardwareMonitor()._aggregate(["CPU", "GPU", "NPU"])
    assert stats["sample_count"] == stats["npu_settle_samples_dropped"] == 0
    assert stats["kpi_status"] == dict.fromkeys(["CPU", "GPU", "NPU"], "no_samples")
    assert (
        stats["cpu_sample_count"]
        == stats["gpu_sample_count"]
        == stats["npu_sample_count"]
        == 0
    )


def test_stop_warns_in_logging_and_pytest_only_for_requested_non_ok_families(
    caplog: pytest.LogCaptureFixture,
) -> None:
    monitor = HardwareMonitor()
    monitor._samples = [{"cpu_util_pct": 10, "npu_utilization": 0}]

    with pytest.warns(UserWarning) as captured:
        stats = monitor.stop(["CPU", "GPU", "NPU"])

    assert len(captured) == 2
    assert len(caplog.records) == 2
    assert "'GPU' reported kpi_status='no_samples' (0 of 1" in caplog.text
    assert "'NPU' reported kpi_status='idle' (1 of 1" in caplog.text
    assert [str(warning.message) for warning in captured] == [
        record.message for record in caplog.records
    ]
    assert stats["kpi_warnings"] == "GPU:no_samples;NPU:idle"


def test_stop_without_requested_families_emits_no_warnings(
    recwarn: pytest.WarningsRecorder,
) -> None:
    assert "kpi_status" not in HardwareMonitor().stop()
    assert not recwarn


@pytest.mark.parametrize(
    ("cli", "environment", "yaml_config", "expected"),
    [
        (None, None, {}, 0.1),
        (None, None, {"trim_fraction": 0.2}, 0.2),
        (None, "0.3", {"trim_fraction": 0.2}, 0.3),
        ("0.4", "0.3", {"trim_fraction": 0.2}, 0.4),
        (0.0, "0.3", {"trim_fraction": 0.2}, 0.0),
        (None, "0", {"trim_fraction": 0.2}, 0.0),
        (None, None, {"trim_fraction": 0}, 0.0),
        ("0.49", "invalid", {"trim_fraction": "invalid"}, 0.49),
        (None, "0.2", {"trim_fraction": "invalid"}, 0.2),
    ],
)
def test_trim_configuration_precedence(
    monkeypatch: pytest.MonkeyPatch,
    cli: str | float | None,
    environment: str | None,
    yaml_config: dict[str, str | float],
    expected: float,
) -> None:
    monkeypatch.setattr(perf_config, "_METRICS_CFG", yaml_config)
    monkeypatch.delenv("PERF_METRICS_TRIM_FRACTION", raising=False)
    if environment is not None:
        monkeypatch.setenv("PERF_METRICS_TRIM_FRACTION", environment)

    assert perf_config.resolve_metrics_trim_fraction(cli) == expected


@pytest.mark.parametrize("source", ["cli", "environment", "yaml"])
@pytest.mark.parametrize(
    "value", ["-0.1", "0.5", "1", "nan", "inf", "-inf", "invalid", ""]
)
def test_trim_configuration_rejects_invalid_values(
    monkeypatch: pytest.MonkeyPatch,
    source: str,
    value: str,
) -> None:
    monkeypatch.setattr(perf_config, "_METRICS_CFG", {})
    monkeypatch.delenv("PERF_METRICS_TRIM_FRACTION", raising=False)
    cli = None
    if source == "cli":
        cli = value
    elif source == "environment":
        monkeypatch.setenv("PERF_METRICS_TRIM_FRACTION", value)
    else:
        monkeypatch.setattr(perf_config, "_METRICS_CFG", {"trim_fraction": value})

    with pytest.raises(ValueError, match="0 <= value < 0.5"):
        perf_config.resolve_metrics_trim_fraction(cli)


@pytest.mark.parametrize("preset", ["default", "quick", "full"])
def test_yaml_presets_expose_metrics(
    monkeypatch: pytest.MonkeyPatch, preset: str
) -> None:
    monkeypatch.setattr(perf_config, "PERF_CONFIG", preset)
    config = perf_config._load_perf_config()
    assert config["metrics"] == {
        "metrics_url": "http://localhost/metrics/stream",
        "sample_interval_seconds": 2.0,
        "trim_fraction": 0.1,
    }
    if preset == "full":
        assert config["benchmark"]["variants"] == ["cpu", "gpu", "npu"]


@pytest.fixture
def harness_pytester(
    pytester: pytest.Pytester, monkeypatch: pytest.MonkeyPatch
) -> pytest.Pytester:
    tests_dir = Path(__file__).resolve().parents[1]
    monkeypatch.setenv(
        "PYTHONPATH",
        os.pathsep.join(
            str(path)
            for path in (
                tests_dir.parent,
                tests_dir / "performance",
                tests_dir / "functional",
            )
        ),
    )
    monkeypatch.delenv("PERF_METRICS_TRIM_FRACTION", raising=False)
    monkeypatch.setenv("PERF_CONFIG", "default")
    pytester.makeconftest("""
        from helpers import pipeline_case_helpers

        pipeline_case_helpers.discover_pipeline_cases_for_pytest = lambda: ([], [])
        pytest_plugins = ["tests.performance.conftest"]
    """)
    return pytester


@pytest.mark.parametrize("preset", ["full", "quick"])
def test_mixed_variant_selection_uses_required_families(
    harness_pytester: pytest.Pytester,
    monkeypatch: pytest.MonkeyPatch,
    preset: str,
) -> None:
    monkeypatch.setenv("PERF_CONFIG", preset)
    harness_pytester.makeconftest("""
        from helpers import pipeline_case_helpers

        case = pipeline_case_helpers.PipelineCase(
            "mixed", "pipeline", "variant", "GPU_NPU", "Mixed"
        )
        pipeline_case_helpers.discover_pipeline_cases_for_pytest = lambda: ([case], ["mixed"])
        pytest_plugins = ["tests.performance.conftest"]
    """)
    harness_pytester.makepyfile("""
        from perf_helpers.config import STREAM_COUNTS, VARIANT_FILTER

        def test_mixed(pipeline_case, stream_count):
            assert "gpu_npu" not in VARIANT_FILTER
            assert {"gpu", "npu"} <= set(VARIANT_FILTER)
            assert pipeline_case.device_family == "GPU_NPU"
            assert stream_count in STREAM_COUNTS
    """)

    result = harness_pytester.runpytest_subprocess("-q", "-p", "no:cacheprovider")

    if preset == "full":
        result.assert_outcomes(passed=4)
    else:
        result.assert_outcomes(skipped=1)


@pytest.mark.parametrize(
    ("arguments", "environment", "expected"),
    [
        ([], None, 0.1),
        ([], "0.2", 0.2),
        (["--metrics-trim-fraction=0"], "invalid", 0),
        (["--metrics-trim-fraction", "0.25"], "0.3", 0.25),
    ],
)
def test_pytest_fixture_uses_resolved_trim_fraction(
    harness_pytester: pytest.Pytester,
    monkeypatch: pytest.MonkeyPatch,
    arguments: list[str],
    environment: str | None,
    expected: float,
) -> None:
    if environment is not None:
        monkeypatch.setenv("PERF_METRICS_TRIM_FRACTION", environment)
    harness_pytester.makepyfile(f"""
        def test_monitor(hw_monitor):
            assert hw_monitor._trim_fraction == {expected!r}
    """)

    result = harness_pytester.runpytest_subprocess(
        "-q", "-p", "no:cacheprovider", *arguments
    )

    result.assert_outcomes(passed=1)


@pytest.mark.parametrize("argument", ["0.5", "nan", "invalid"])
def test_pytest_rejects_invalid_trim_fraction_before_tests(
    harness_pytester: pytest.Pytester,
    argument: str,
) -> None:
    harness_pytester.makepyfile("def test_not_run(): assert False")

    result = harness_pytester.runpytest_subprocess(
        f"--metrics-trim-fraction={argument}"
    )

    assert result.ret == pytest.ExitCode.USAGE_ERROR
    result.stderr.fnmatch_lines(["*metrics.trim_fraction*0 <= value < 0.5*"])


def test_harness_warning_appears_in_pytest_summary(
    harness_pytester: pytest.Pytester,
) -> None:
    harness_pytester.makepyfile("""
        def test_monitor(hw_monitor):
            hw_monitor._samples = [{"gpu_compute_util_pct": 5, "npu_utilization": 0}]
            stats = hw_monitor.stop(["GPU", "NPU"])
            assert stats["kpi_status"] == {"GPU": "ok", "NPU": "idle"}
    """)

    result = harness_pytester.runpytest_subprocess("-q", "-p", "no:cacheprovider")

    result.assert_outcomes(passed=1, warnings=1)
    result.stdout.fnmatch_lines(["*warnings summary*", "*NPU*idle*"])


@pytest.mark.parametrize("job_fails", [False, True])
def test_pipeline_passes_mixed_variant_families_to_monitor(
    monkeypatch: pytest.MonkeyPatch,
    pytestconfig: pytest.Config,
    job_fails: bool,
) -> None:
    pytestconfig.addinivalue_line("markers", "perf: Performance benchmark test")
    tests_dir = Path(__file__).resolve().parents[1]
    monkeypatch.syspath_prepend(str(tests_dir / "performance"))
    monkeypatch.syspath_prepend(str(tests_dir / "functional"))
    from tests.performance import test_pipeline_performance as pipeline_test

    case = pipeline_test.PipelineCase(
        "mixed", "pipeline", "variant", "GPU_NPU", "Mixed"
    )
    attempt = Mock(
        return_value={"state": "COMPLETED", "total_fps": 20, "per_stream_fps": 20}
    )
    if job_fails:
        attempt.side_effect = RuntimeError("job failed")
    monkeypatch.setattr(pipeline_test, "_attempt_performance_job", attempt)
    monitor = Mock(spec=HardwareMonitor)
    results = []

    if job_fails:
        with pytest.raises(RuntimeError, match="job failed"):
            pipeline_test.test_pipeline_performance(Mock(), case, 1, monitor, results)
    else:
        pipeline_test.test_pipeline_performance(Mock(), case, 1, monitor, results)
        assert results[0]["hw_metrics"] == monitor.stop.return_value

    monitor.start.assert_called_once()
    monitor.stop.assert_called_once_with(families=["GPU", "NPU"])
