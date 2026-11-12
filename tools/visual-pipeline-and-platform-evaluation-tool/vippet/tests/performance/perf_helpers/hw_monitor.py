# Copyright (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""Hardware Metrics Monitor.

Samples CPU, GPU (Xe), and NPU KPIs in a background thread during benchmark runs.
Source: VIPPET metrics-manager JSON API (CPU, GPU, NPU, memory, temperature, power).
"""

import json
import logging
import threading
import time
import warnings
from typing import Any

import httpx

logger = logging.getLogger(__name__)

DEFAULT_TRIM_FRACTION = 0.1
NPU_SETTLE_MIN_SAMPLES = 3

FAMILY_KPI_KEYS: dict[str, tuple[str, ...]] = {
    "CPU": ("cpu_util_pct", "cpu_freq_mhz", "cpu_temperature", "mem_used_percent"),
    "GPU": (
        "gpu_render_util_pct",
        "gpu_video_util_pct",
        "gpu_enhance_util_pct",
        "gpu_compute_util_pct",
        "gpu_copy_util_pct",
        "gpu_util_combined",
        "gpu_freq_mhz",
        "gpu_power_w",
        "pkg_power_w",
    ),
    "NPU": (
        "npu_utilization",
        "npu_power",
        "npu_frequency_mhz",
        "npu_temperature",
        "npu_memory_mb",
        "npu_bandwidth",
    ),
}
FAMILY_ACTIVITY_KEYS: dict[str, tuple[str, ...]] = {
    "CPU": ("cpu_util_pct",),
    "GPU": (
        "gpu_render_util_pct",
        "gpu_video_util_pct",
        "gpu_enhance_util_pct",
        "gpu_compute_util_pct",
        "gpu_copy_util_pct",
    ),
    "NPU": ("npu_utilization", "npu_frequency_mhz", "npu_power"),
}


def _trim_edge_samples(
    samples: list[dict[str, float]],
    trim_fraction: float,
) -> list[dict[str, float]]:
    """Trim time-ordered samples from each end, not sorted metric values."""
    trim_count = int(len(samples) * trim_fraction)
    if trim_count == 0 or trim_count * 2 >= len(samples):
        return samples
    return samples[trim_count:-trim_count]


def _drop_npu_settle_sample(
    samples: list[dict[str, float]],
) -> tuple[list[dict[str, float]], int]:
    """Remove only the first sample's NPU keys without mutating input data."""
    if len(samples) < NPU_SETTLE_MIN_SAMPLES or not any(
        key.startswith("npu_") for key in samples[0]
    ):
        return samples, 0
    first = {
        key: value for key, value in samples[0].items() if not key.startswith("npu_")
    }
    return [first, *samples[1:]], 1


def _family_sample_count(samples: list[dict[str, float]], family: str) -> int:
    """Count samples with any KPI belonging to the requested family."""
    keys = FAMILY_KPI_KEYS.get(family, ())
    return sum(any(key in sample for key in keys) for sample in samples)


def _family_kpi_status(samples: list[dict[str, float]], family: str) -> str:
    """Distinguish unavailable activity, measured inactivity and active hardware."""
    values = [
        sample[key]
        for sample in samples
        for key in FAMILY_ACTIVITY_KEYS.get(family, ())
        if key in sample
    ]
    if not values:
        return "no_samples"
    return "ok" if any(value != 0 for value in values) else "idle"


def _fetch_metrics_manager(url: str) -> dict[str, float]:
    """Fetch latest metrics from VIPPET metrics-manager JSON API.

    Returns flat dict of metric_key -> value.
    Tagged metrics use composite keys (e.g. gpu_power_power_w__gpu_cur_power).
    Only gpu_id=0 is collected for GPU metrics with multiple IDs. Metrics
    reported per GPU tile (e.g. gt0 render + gt1 media) keep only the
    primary, lowest-named tile, so tiles never overwrite each other.

    Handles two response formats:
      - SSE stream (``data: {json}\\n``) — reads the last complete event
      - Plain JSON with ``metrics`` as a list or dict
    """
    try:
        raw = ""
        for attempt in range(3):
            with httpx.stream("GET", url, timeout=5) as resp:
                resp.raise_for_status()
                for line in resp.iter_lines():
                    if line.startswith("data: "):
                        raw = line[6:]
                        break
            if raw:
                break
            time.sleep(1)

        if not raw:
            return {}

        data = json.loads(raw)
        metrics = data.get("metrics", data)
        result: dict[str, float] = {}
        tile_of: dict[str, str] = {}

        def _store(composite: str, value: float, tile: str | None) -> None:
            if tile is not None:
                kept = tile_of.get(composite)
                if kept is not None and kept <= tile:
                    return
                tile_of[composite] = tile
            result[composite] = value

        if isinstance(metrics, list):
            for entry in metrics:
                name = entry.get("name", "")
                val = entry.get("value")
                if val is None or not name:
                    continue
                labels = entry.get("labels", {})

                gpu_id = labels.get("gpu_id")
                if gpu_id is not None and gpu_id != "0":
                    continue

                type_tag = labels.get("type") or labels.get("engine")
                composite = f"{name}__{type_tag}" if type_tag else name
                _store(composite, float(val), labels.get("tile"))
        elif isinstance(metrics, dict):
            for key, entry in metrics.items():
                if isinstance(entry, dict):
                    val = entry.get("fields", {}).get("value")
                    if val is None:
                        continue
                    name = entry.get("name", key.split("{")[0])
                    tags = entry.get("tags", {})

                    gpu_id = tags.get("gpu_id")
                    if gpu_id is not None and gpu_id != "0":
                        continue

                    type_tag = tags.get("type") or tags.get("engine")
                    composite = f"{name}__{type_tag}" if type_tag else name
                    _store(composite, float(val), tags.get("tile"))
                elif isinstance(entry, (int, float)):
                    result[key] = float(entry)

        return result
    except Exception as e:
        logger.debug("metrics-manager fetch failed: %s", e)
        return {}


class HardwareMonitor:
    """Background-thread hardware sampler.

    Usage:
        monitor = HardwareMonitor("http://localhost/metrics/stream")
        monitor.start()
        ... run workload ...
        hw_stats = monitor.stop(["GPU", "NPU"])   # returns aggregated dict
    """

    def __init__(
        self,
        metrics_url: str = "http://localhost/metrics/stream",
        sample_interval: float = 2.0,
        trim_fraction: float = DEFAULT_TRIM_FRACTION,
    ):
        if not 0 <= trim_fraction < 0.5:
            raise ValueError("metrics.trim_fraction must satisfy 0 <= value < 0.5")
        self._metrics_url = metrics_url
        self._interval = sample_interval
        self._trim_fraction = trim_fraction
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self._samples: list[dict[str, float]] = []

    def start(self) -> None:
        self._stop.clear()
        self._samples.clear()
        self._thread = threading.Thread(
            target=self._loop, daemon=True, name="hw-monitor"
        )
        self._thread.start()
        logger.debug("HardwareMonitor started (interval=%.1fs)", self._interval)

    def stop(self, families: list[str] | None = None) -> dict[str, Any]:
        self._stop.set()
        if self._thread:
            self._thread.join(timeout=max(self._interval * 2, 10))
        stats = self._aggregate(families)
        logger.debug("HardwareMonitor stopped (%d samples)", len(self._samples))
        for family, status in stats.get("kpi_status", {}).items():
            if status == "ok":
                continue
            count = stats[f"{family.lower()}_sample_count"]
            message = (
                f"HardwareMonitor: device family {family!r} reported "
                f"kpi_status={status!r} ({count} of {stats['sample_count']} "
                f"sample(s) carried a {family} KPI key)"
            )
            logger.warning(message)
            warnings.warn(message, stacklevel=2)
        return stats

    def _loop(self) -> None:
        while not self._stop.is_set():
            try:
                sample = self._collect()
                if sample:
                    self._samples.append(sample)
            except Exception as e:
                logger.debug("hw sample error: %s", e)
            self._stop.wait(self._interval)

    def _collect(self) -> dict[str, float]:
        sample: dict[str, float] = {}

        mm = _fetch_metrics_manager(self._metrics_url)

        # CPU
        idle = mm.get("cpu_usage_idle")
        if idle is not None:
            sample["cpu_util_pct"] = round(100.0 - idle, 2)
        for key in ("cpu_usage_user", "cpu_usage_system", "mem_used_percent"):
            if key in mm:
                sample[key] = mm[key]
        freq = mm.get("cpu_frequency_avg_frequency_hz")
        if freq is not None:
            sample["cpu_freq_mhz"] = round(freq / 1e6, 0)
        temp = mm.get("temp_temp")
        if temp is not None:
            sample["cpu_temperature"] = temp

        # GPU (Xe/i915) — per-engine utilization.
        # NOTE: ``gpu_render_util_pct`` (engine="render"/"rcs") has been
        # observed to always read 0 on tested hardware, even when the GPU
        # is clearly busy per "video"/"compute". This looks like an
        # upstream qmassa/metrics-manager reporting gap for the render
        # engine specifically, not a bug in this harness — tracked as a
        # separate follow-up issue rather than fixed here. The per-family
        # activity check (FAMILY_ACTIVITY_KEYS) deliberately unions across
        # all GPU engine keys so this doesn't cause false "idle" GPU
        # classifications.
        for label, aliases in [
            ("gpu_render_util_pct", ("render", "rcs")),
            ("gpu_video_util_pct", ("video", "vcs")),
            ("gpu_enhance_util_pct", ("video-enhance", "vecs")),
            ("gpu_compute_util_pct", ("compute", "ccs")),
            ("gpu_copy_util_pct", ("copy", "bcs")),
        ]:
            for engine in aliases:
                val = mm.get(f"gpu_engine_usage_usage__{engine}")
                if val is not None:
                    sample[label] = round(val, 2)
                    break

        # GPU frequency
        val = mm.get("gpu_frequency_cur_freq_hz__cur_freq")
        if val is not None:
            sample["gpu_freq_mhz"] = val / 1e6

        # GPU power
        val = mm.get("gpu_power_power_w__gpu_cur_power")
        if val is not None:
            sample["gpu_power_w"] = round(val, 3)
        val = mm.get("gpu_power_power_w__pkg_cur_power")
        if val is not None:
            sample["pkg_power_w"] = round(val, 3)

        frequency = mm.get("npu_frequency_hz")
        if frequency is not None:
            sample["npu_frequency_mhz"] = round(frequency / 1e6, 1)

        # NPU
        for source, target, divisor in (
            ("npu_utilization", "npu_utilization", 1),
            ("npu_power_w", "npu_power", 1),
            ("npu_temperature_c", "npu_temperature", 1),
            ("npu_memory_bytes", "npu_memory_mb", 1 << 20),
            ("npu_bandwidth_bytes_per_second", "npu_bandwidth", 1e6),
        ):
            if source in mm:
                sample[target] = mm[source] / divisor

        return sample

    def _aggregate(self, families: list[str] | None = None) -> dict[str, Any]:
        samples, settle_dropped = _drop_npu_settle_sample(self._samples)
        trimmed = _trim_edge_samples(samples, self._trim_fraction)
        agg: dict[str, Any] = {
            "sample_count": len(samples),
            "npu_settle_samples_dropped": settle_dropped,
        }
        keys = {key for sample in samples for key in sample}

        for key in sorted(keys):
            values = [sample[key] for sample in samples if key in sample]
            average_values = [
                sample[key] for sample in trimmed if key in sample
            ] or values
            agg[f"{key}_avg"] = round(sum(average_values) / len(average_values), 2)
            agg[f"{key}_min"] = round(min(values), 2)
            agg[f"{key}_max"] = round(max(values), 2)

        # Combined GPU utilization: max(render, video) per sample, then averaged.
        combined_per_sample = []
        combined_trimmed = []
        for index, sample in enumerate(samples):
            render = sample.get("gpu_render_util_pct")
            video = sample.get("gpu_video_util_pct")
            vals = [v for v in (render, video) if v is not None]
            if vals:
                combined_per_sample.append(max(vals))
                trim_count = int(len(samples) * self._trim_fraction)
                if trim_count <= index < len(samples) - trim_count:
                    combined_trimmed.append(max(vals))
        if combined_per_sample:
            agg["gpu_util_combined_max"] = round(max(combined_per_sample), 2)
            average_values = combined_trimmed or combined_per_sample
            agg["gpu_util_combined_avg"] = round(
                sum(average_values) / len(average_values), 2
            )

        if families:
            kpi_status = {
                family: _family_kpi_status(samples, family) for family in families
            }
            agg["kpi_status"] = kpi_status
            for family in families:
                agg[f"{family.lower()}_sample_count"] = _family_sample_count(
                    samples, family
                )
            agg["kpi_warnings"] = ";".join(
                f"{family}:{status}"
                for family, status in kpi_status.items()
                if status != "ok"
            )

        return agg
