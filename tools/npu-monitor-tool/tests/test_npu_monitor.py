# Copyright (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""Offline regression tests for the standalone monitor and exporter."""

import csv
import importlib.util
import json
from importlib.machinery import ModuleSpec
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from types import ModuleType
from unittest.mock import Mock

import pytest


@pytest.fixture
def monitor_module() -> ModuleType:
    path = Path(__file__).resolve().parents[1] / "npu-monitor-tool.py"
    spec = spec_from_file_location("npu_monitor_tool", path)
    assert spec is not None and spec.loader is not None
    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


@pytest.mark.parametrize(
    ("generation", "ratio", "expected"),
    [
        ("MTL", 48, 1_600_000_000),
        ("ARL", 48, 1_600_000_000),
        ("LNL", 78, 1_950_000_000),
        ("PTL", 78, 1_950_000_000),
        ("WCL", 78, 1_950_000_000),
        ("ARL", 0, 0),
    ],
)
def test_frequency_decodes_real_pmt_ratio(
    monitor_module: ModuleType,
    generation: str,
    ratio: int,
    expected: float,
) -> None:
    telemetry = monitor_module.PmtTelemetry.__new__(monitor_module.PmtTelemetry)
    telemetry.cpu_gen = monitor_module.CpuGen[generation]
    telemetry.regs = {"VPU_WORKPOINT": 0}
    telemetry.buffer = ratio.to_bytes(8, "little")
    assert telemetry.get_freq_hz() == expected


def test_bandwidth_uses_elapsed_time_and_wraparound(monitor_module: ModuleType) -> None:
    rate = monitor_module.noc_bandwidth_rate_mbs
    assert rate(10, 30, 2) == 10
    assert rate(30, 30, 2) == 0
    wrap = monitor_module.NOC_BW_COUNTER_WRAP_MB
    assert rate(wrap - 15, 5, 2) == pytest.approx(10)
    assert rate(2 * wrap - 15, 5, 2) == pytest.approx(10)


@pytest.mark.parametrize("elapsed", [0, -1, float("nan"), float("inf")])
def test_bandwidth_rejects_invalid_elapsed_time(
    monitor_module: ModuleType,
    elapsed: float,
) -> None:
    with pytest.raises(ValueError, match="elapsed_s"):
        monitor_module.noc_bandwidth_rate_mbs(10, 30, elapsed)


@pytest.mark.parametrize("buffer", [None, b"\x00" * 4])
def test_missing_or_short_pmt_buffer_is_not_zero(
    monitor_module: ModuleType,
    buffer: bytes | None,
) -> None:
    telemetry = monitor_module.PmtTelemetry.__new__(monitor_module.PmtTelemetry)
    telemetry.regs = {
        "VPU_WORKPOINT": 0,
        "VPU_ENERGY": 0,
        "SOC_TEMPERATURES": 0,
        "VPU_MEMORY_BW": [0],
    }
    telemetry.cpu_gen = monitor_module.CpuGen.LNL
    telemetry.buffer = buffer
    for getter in (
        "get_freq_hz",
        "get_voltage",
        "get_tile_config",
        "get_npu_temperature",
        "get_npu_energy",
        "get_noc_bandwidth",
    ):
        assert getattr(telemetry, getter)() is None


def test_failed_pmt_update_clears_previous_sample(
    monitor_module: ModuleType, tmp_path: Path
) -> None:
    telemetry = monitor_module.PmtTelemetry.__new__(monitor_module.PmtTelemetry)
    telemetry.telemetry_path = str(tmp_path / "missing")
    telemetry.buffer = b"\xff" * 8
    telemetry.update_buffer()
    assert telemetry.buffer is None


def test_bandwidth_unavailable_on_mtl_but_measured_zero_on_lnl(
    monitor_module: ModuleType,
) -> None:
    telemetry = monitor_module.PmtTelemetry.__new__(monitor_module.PmtTelemetry)
    telemetry.buffer = bytes(4096)
    telemetry.regs = monitor_module.get_mtl_regs()
    assert telemetry.get_noc_bandwidth() is None
    telemetry.regs = monitor_module.get_lnl_regs()
    assert telemetry.get_noc_bandwidth() == 0


@pytest.mark.parametrize("contents", [None, "", "bad", "-1", "0", "1024"])
def test_optional_sysfs_missing_invalid_and_zero(
    monitor_module: ModuleType,
    tmp_path: Path,
    contents: str | None,
) -> None:
    path = tmp_path / "counter"
    if contents is not None:
        path.write_text(contents)
    monitor = monitor_module.NpuMonitor.__new__(monitor_module.NpuMonitor)
    monitor.npu_busy_path = monitor.mem_util_path = str(path)
    valid = contents in ("0", "1024")
    assert monitor.read_busy_time() == (int(contents) if valid else None)
    assert monitor.read_mem_util() == (contents if valid else None)


@pytest.mark.parametrize("available", [True, False])
def test_cli_csv_uses_real_rates_and_empty_cells(
    monitor_module: ModuleType,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
    available: bool,
) -> None:
    telemetry = Mock()
    telemetry.get_npu_energy.side_effect = [100, 104] if available else [None, None]
    telemetry.get_noc_bandwidth.side_effect = [10, 30] if available else [None, None]
    telemetry.get_freq_hz.return_value = 1_600_000_000 if available else None
    telemetry.get_tile_config.return_value = 4 if available else None
    telemetry.get_npu_temperature.return_value = 40 if available else None
    device = Mock()
    device.get_pmt_telemetry.return_value = telemetry
    device.read_busy_time.side_effect = (
        [1_000_000, 1_200_000] if available else [None, None]
    )
    device.read_mem_util.return_value = "0" if available else None
    device.read_pciid.return_value = "0x7d1d"
    device.read_fw_version.return_value = "test firmware"
    device.read_driver_version.return_value = "test driver"
    clock = [100.0]

    def advance_clock(interval: float) -> None:
        clock[0] += 2

    monkeypatch.setattr(monitor_module, "NpuMonitor", lambda: device)
    monkeypatch.setattr(monitor_module, "sleep", advance_clock)
    monkeypatch.setattr(monitor_module, "CLEAR_CMD", None)
    monkeypatch.setattr(monitor_module.time_module, "monotonic", lambda: clock[0])
    monkeypatch.setattr(monitor_module.sys, "argv", ["npu-monitor-tool", "--csv"])
    monkeypatch.chdir(tmp_path)

    monitor_module.main()

    with next((tmp_path / "npu_output").glob("*.csv")).open(newline="") as output:
        row = next(csv.DictReader(output))
    terminal = capsys.readouterr().out
    if available:
        assert float(row["frequency"]) == 1_600_000_000
        assert float(row["bandwidth"]) == 10
        assert float(row["power"]) == 2
        assert float(row["utilization"]) == 10
        assert float(row["memory_usage"]) == 0
        assert "1600 [MHz]" in terminal
    else:
        for column in (
            "power",
            "frequency",
            "bandwidth",
            "tile_config",
            "temperature",
            "utilization",
            "memory_usage",
        ):
            assert row[column] == ""
        assert terminal.count("N/A") == 7


@pytest.mark.parametrize("delay_stage", ["clear", "memory", "pmt"])
def test_cli_busy_time_interval_excludes_later_sampling_delays(
    monitor_module: ModuleType,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    delay_stage: str,
) -> None:
    clock = [0.0]
    samples = 0

    def advance_interval(interval: float) -> None:
        nonlocal samples
        if samples == 2:
            raise KeyboardInterrupt
        samples += 1
        clock[0] += interval

    def delay(stage: str) -> None:
        if stage == delay_stage:
            clock[0] += 0.2

    def read_memory() -> str:
        delay("memory")
        return "0"

    telemetry = Mock()
    telemetry.update_buffer.side_effect = lambda: delay("pmt")
    telemetry.get_npu_energy.side_effect = lambda: clock[0] * 2
    telemetry.get_noc_bandwidth.side_effect = lambda: clock[0] * 10
    telemetry.get_freq_hz.return_value = 1_600_000_000
    telemetry.get_tile_config.return_value = 4
    telemetry.get_npu_temperature.return_value = 40
    device = Mock()
    device.get_pmt_telemetry.return_value = telemetry
    device.read_busy_time.side_effect = lambda: round(clock[0] * 1e6)
    device.read_mem_util.side_effect = read_memory
    device.read_pciid.return_value = "0x7d1d"
    device.read_fw_version.return_value = "test firmware"
    device.read_driver_version.return_value = "test driver"
    monkeypatch.setattr(monitor_module, "NpuMonitor", lambda: device)
    monkeypatch.setattr(monitor_module, "sleep", advance_interval)
    monkeypatch.setattr(monitor_module, "CLEAR_CMD", "/test/clear")
    monkeypatch.setattr(
        monitor_module.subprocess, "run", lambda *args, **kwargs: delay("clear")
    )
    monkeypatch.setattr(monitor_module.time_module, "monotonic", lambda: clock[0])
    monkeypatch.setattr(
        monitor_module.sys, "argv", ["npu-monitor-tool", "--csv", "-i", "200"]
    )
    monkeypatch.chdir(tmp_path)

    with pytest.raises(KeyboardInterrupt):
        monitor_module.main()

    with next((tmp_path / "npu_output").glob("*.csv")).open(newline="") as output:
        rows = list(csv.DictReader(output))
    assert len(rows) == 2
    for row in rows:
        assert float(row["utilization"]) == pytest.approx(100, abs=1)
        assert float(row["power"]) == pytest.approx(2)
        assert float(row["bandwidth"]) == pytest.approx(10)


@pytest.fixture
def exporter_module(
    monitor_module: ModuleType, monkeypatch: pytest.MonkeyPatch
) -> ModuleType:
    telemetry = Mock()
    telemetry.get_npu_energy.return_value = 100
    telemetry.get_noc_bandwidth.return_value = 10
    telemetry.get_freq_hz.return_value = 1_600_000_000
    telemetry.get_npu_temperature.return_value = 40
    telemetry.get_tile_config.return_value = 4
    device = Mock()
    device.get_pmt_telemetry.return_value = telemetry
    device.read_busy_time.return_value = 1_000_000
    device.read_mem_util.return_value = "1048576"
    device.read_pciid.return_value = "0x7d1d"
    monkeypatch.setattr(monitor_module, "NpuMonitor", lambda: device)
    path = Path(__file__).resolve().parents[1] / "npu-metrics-exporter.py"
    spec = spec_from_file_location("npu_metrics_exporter", path)
    assert spec is not None and spec.loader is not None
    module = module_from_spec(spec)
    with monkeypatch.context() as loader_patch:
        loader_patch.setattr(
            importlib.util,
            "spec_from_file_location",
            lambda name, path: ModuleSpec(name, Mock()),
        )
        loader_patch.setattr(
            importlib.util, "module_from_spec", lambda spec: monitor_module
        )
        spec.loader.exec_module(module)
    return module


def exported_samples(exporter: ModuleType) -> dict[str, float]:
    from prometheus_client.parser import text_string_to_metric_families

    return {
        sample.name: sample.value
        for family in text_string_to_metric_families(
            exporter.generate_latest(exporter.registry).decode()
        )
        for sample in family.samples
    }


@pytest.mark.parametrize(
    "metric",
    [
        "NPU_TEMPERATURE",
        "NPU_FREQUENCY",
        "NPU_MEM_UTIL",
        "NPU_MEM_BANDWIDTH",
        "NPU_UTIL",
        "NPU_POWER",
        "NPU_TILE_CFG",
    ],
)
def test_exporter_removes_missing_gauge_but_preserves_zero(
    exporter_module: ModuleType, metric: str
) -> None:
    exporter = exporter_module
    name = exporter.metrics_map[metric].prom_metric.name
    for value in (12, 0, None, None, 25):
        exporter.set_or_remove_gauge(metric, ["dev_id"], ["0x7d1d"], value)
        samples = exported_samples(exporter)
        if value is None:
            assert name not in samples
        else:
            assert samples[name] == value


def test_exporter_bandwidth_wrap_invalid_time_and_recovery(
    exporter_module: ModuleType,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    exporter = exporter_module
    monkeypatch.setattr(
        exporter,
        "prev_mem_bandwidth_mb",
        exporter.npu_monitor_tool.NOC_BW_COUNTER_WRAP_MB - 15,
    )
    monkeypatch.setattr(exporter, "prev_mem_bandwidth_ts", 100)
    clock = [102.0]
    monkeypatch.setattr(exporter.time, "monotonic", lambda: clock[0])
    exporter.pu.get_noc_bandwidth.return_value = 5
    exporter.process_npu_mem_bandwidth()
    assert exported_samples(exporter)["npu_memory_bandwidth"] == pytest.approx(10)
    exporter.pu.get_noc_bandwidth.return_value = 25
    exporter.process_npu_mem_bandwidth()
    assert "npu_memory_bandwidth" not in exported_samples(exporter)
    assert exporter.prev_mem_bandwidth_mb == 5
    assert exporter.prev_mem_bandwidth_ts == 102
    clock[0] = 104
    exporter.process_npu_mem_bandwidth()
    assert exported_samples(exporter)["npu_memory_bandwidth"] == 10
    for moment, value, expected in [(106, None, None), (108, 40, None), (110, 40, 0)]:
        clock[0] = moment
        exporter.pu.get_noc_bandwidth.return_value = value
        exporter.process_npu_mem_bandwidth()
        assert exported_samples(exporter).get("npu_memory_bandwidth") == expected


def test_exporter_utilization_reset_and_missing_rebaseline(
    exporter_module: ModuleType,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    exporter = exporter_module
    monkeypatch.setattr(exporter, "prev_time_ms", 100)
    monkeypatch.setattr(exporter, "prev_busy_time", 1_000_000)
    for moment, value, expected in [
        (1100, 1_100_000, 10),
        (2100, 10, None),
        (3100, 100010, 10),
        (4100, None, None),
        (5100, 9000000, None),
        (6100, 9000000, 0),
    ]:
        monkeypatch.setattr(exporter, "curr_time_ms", moment)
        exporter.npu_mon.read_busy_time.return_value = value
        exporter.process_npu_utilization()
        assert exported_samples(exporter).get("npu_util") == expected
        monkeypatch.setattr(exporter, "prev_time_ms", moment)


def test_exporter_endpoint_keeps_frequency_when_optional_metrics_disappear(
    exporter_module: ModuleType,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    exporter = exporter_module
    monkeypatch.setattr(exporter, "prev_time_ms", 100_000)
    monkeypatch.setattr(exporter, "prev_mem_bandwidth_ts", 100)
    clock = [102.0]
    monkeypatch.setattr(exporter.time, "monotonic", lambda: clock[0])
    monkeypatch.setattr(exporter.time, "monotonic_ns", lambda: int(clock[0] * 1e9))
    client = exporter.app.test_client()
    assert client.get("/metrics").status_code == 200
    assert exported_samples(exporter)["npu_memory_util"] == 1
    assert exported_samples(exporter)["npu_util"] == 0
    exporter.npu_mon.read_mem_util.return_value = None
    exporter.npu_mon.read_busy_time.return_value = None
    clock[0] += 2
    response = client.get("/metrics")
    assert response.status_code == 200
    samples = exported_samples(exporter)
    assert "npu_memory_util" not in samples
    assert "npu_util" not in samples
    assert samples["npu_monitor_freq_hz"] == 1_600_000_000


def test_grafana_units_and_missing_data_settings() -> None:
    path = Path(__file__).resolve().parents[1] / "docs" / "grafana-npu-dashboard.json"
    dashboard = json.loads(path.read_text())
    panels = [panel for panel in dashboard["panels"] if panel.get("targets")]
    frequency_panels = [panel for panel in panels if panel["title"] == "DPU Frequency"]
    assert len(frequency_panels) == 2
    for panel in panels:
        defaults = panel["fieldConfig"]["defaults"]
        assert defaults["noValue"] == "N/A"
        if panel["type"] == "timeseries":
            assert defaults["custom"]["spanNulls"] is False
        else:
            assert panel["targets"][0]["instant"] is True
            assert panel["options"]["reduceOptions"]["calcs"] == ["last"]
    for panel in frequency_panels:
        assert panel["targets"][0]["expr"] == "npu_monitor_freq_hz{} / 1e6"
        assert panel["fieldConfig"]["defaults"]["unit"] == "suffix:MHz"
    bandwidth = next(
        panel for panel in panels if panel["title"] == "NPU Memory Bandwidth"
    )
    assert bandwidth["targets"][0]["expr"] == "npu_memory_bandwidth{}"
    assert bandwidth["fieldConfig"]["defaults"]["unit"] == "suffix:MB/s"
    utilization = next(panel for panel in panels if panel["title"] == "NPU Utilization")
    assert utilization["targets"][0]["expr"] == "npu_util{}"
    assert utilization["fieldConfig"]["defaults"]["unit"] == "percent"
    assert utilization["fieldConfig"]["defaults"]["max"] == 100


@pytest.mark.parametrize("interval", ["0", "-1", "nan", "inf"])
def test_cli_rejects_invalid_interval_before_hardware_access(
    monitor_module: ModuleType,
    monkeypatch: pytest.MonkeyPatch,
    interval: str,
) -> None:
    monkeypatch.setattr(
        monitor_module.sys, "argv", ["npu-monitor-tool", "-i", interval]
    )
    with pytest.raises(SystemExit) as error:
        monitor_module.main()
    assert error.value.code == 2
