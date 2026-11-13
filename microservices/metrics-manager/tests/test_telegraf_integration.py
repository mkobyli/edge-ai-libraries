# Copyright (C) 2025-2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""
Wiring tests for Telegraf configuration mounting and custom-metrics.

These tests parse the actual config files shipped with the image (telegraf.conf,
compose.yaml, supervisord.conf, Dockerfile) and assert that the mount points,
override mechanisms and ordering guarantees are wired up correctly. They do not
require a running Docker daemon - any regression in the wiring (e.g. someone
breaks the TELEGRAF_CONFIG override path, drops the custom-metrics volume or
removes the Prometheus output the SSE stream depends on) will fail in CI.

End-to-end Docker-based tests are documented at the bottom of this file.
"""

import asyncio
import configparser
import json
import shutil
import socket
import subprocess
import time
import tomllib
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import httpx
import pytest
import yaml

from app.sse import fetch_metrics

REPO_ROOT = Path(__file__).resolve().parent.parent
TELEGRAF_CONF = REPO_ROOT / "telegraf.conf"
COMPOSE_YAML = REPO_ROOT / "compose.yaml"
SUPERVISORD_CONF = REPO_ROOT / "supervisord.conf"
DOCKERFILE = REPO_ROOT / "Dockerfile"
ENTRYPOINT = REPO_ROOT / "entrypoint.sh"
QMASSA_UNIT = REPO_ROOT / "native" / "packaging" / "systemd" / "metrics-manager-qmassa.service"
VIPPET_TELEGRAF_CONF = (
    REPO_ROOT.parents[1] / "tools/visual-pipeline-and-platform-evaluation-tool/vippet-telegraf.conf"
)
EXPORT_CONFIGS = [
    pytest.param(TELEGRAF_CONF, id="metrics-manager"),
    pytest.param(REPO_ROOT / "native/packaging/etc/telegraf.conf", id="bare-metal"),
    pytest.param(
        VIPPET_TELEGRAF_CONF, id="vippet",
        marks=pytest.mark.skipif(
            not VIPPET_TELEGRAF_CONF.is_file(), reason="requires the ViPPET checkout",
        ),
    ),
]


@pytest.fixture(scope="module")
def telegraf_text() -> str:
    return TELEGRAF_CONF.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def compose_text() -> str:
    return COMPOSE_YAML.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def supervisord_text() -> str:
    return SUPERVISORD_CONF.read_text(encoding="utf-8")


class TestDefaultTelegrafConfig:
    """Default telegraf.conf must declare the inputs/outputs the service relies on."""

    def test_no_websocket_output(self, telegraf_text: str):
        # The service streams metrics to clients via SSE (/metrics/stream),
        # which scrapes the Prometheus endpoint below. The legacy
        # [[outputs.websocket]] relay to /ws/collector has been removed - guard
        # against accidental re-introduction (Telegraf would log endless
        # "connection refused" errors against a non-existent endpoint).
        assert "[[outputs.websocket]]" not in telegraf_text
        assert "/ws/collector" not in telegraf_text

    def test_prometheus_output_enabled(self, telegraf_text: str):
        assert "[[outputs.prometheus_client]]" in telegraf_text
        config = tomllib.loads(telegraf_text)
        assert config["outputs"]["prometheus_client"][0]["listen"] == ":${TELEGRAF_PORT}"

    @pytest.mark.parametrize("config_path", EXPORT_CONFIGS)
    def test_prometheus_expiration_preserves_default_ten_second_inputs(self, config_path: Path):
        output = tomllib.loads(config_path.read_text())["outputs"]["prometheus_client"][0]
        assert output["expiration_interval"] == "15s"
        assert output["export_timestamp"] is True

    def test_http_listener_for_custom_metrics_relay(self, telegraf_text: str):
        # /api/v1/metrics endpoints persist via Telegraf's HTTP listener on :8186.
        assert "[[inputs.http_listener_v2]]" in telegraf_text
        config = tomllib.loads(telegraf_text)
        assert (
            config["inputs"]["http_listener_v2"][0]["service_address"] == ":${TELEGRAF_HTTP_PORT}"
        )

    def test_custom_metrics_directory_input(self, telegraf_text: str):
        # The default config must execute user scripts dropped into /app/custom-metrics.
        assert "/app/custom-metrics" in telegraf_text
        assert "[[inputs.exec]]" in telegraf_text

    def test_hostname_override_wired_in_agent(self, telegraf_text: str):
        # METRICS_MANAGER_HOSTNAME must reach Telegraf's [agent] block so the
        # built-in inputs (cpu/mem/temp) carry the same `host=` tag as the
        # native plugins.
        assert 'hostname = "${METRICS_MANAGER_HOSTNAME}"' in telegraf_text


def test_qmassa_service_can_read_other_users_gpu_clients():
    unit = configparser.ConfigParser(interpolation=None, strict=False)
    assert unit.read(QMASSA_UNIT)
    service = unit["Service"]

    assert "ProtectProc" not in service
    assert set(service["CapabilityBoundingSet"].split()) == {
        "CAP_SYS_RAWIO",
        "CAP_PERFMON",
        "CAP_SYS_PTRACE",
        "CAP_DAC_READ_SEARCH",
    }


class TestHostnameOverrideWiring:
    """METRICS_MANAGER_HOSTNAME must be wired through compose + the native plugins."""

    def test_compose_passes_env_var(self, compose_text: str):
        services = yaml.safe_load(compose_text)["services"]
        for name in ("metrics-manager", "metrics-manager-host-nic"):
            assert (
                services[name]["environment"]["METRICS_MANAGER_HOSTNAME"]
                == "${METRICS_MANAGER_HOSTNAME:-}"
            )

    @pytest.mark.parametrize("plugin", ["mm-plugin-gpu", "mm-plugin-npu"])
    def test_native_plugin_reads_env_var(self, plugin: str):
        # The GPU and NPU plugins stamp the host tag themselves, so they have to
        # honour the override independently of Telegraf's [agent] hostname. The
        # CPU plugin emits no host tag at all and inherits Telegraf's, matching
        # the shell script it replaced.
        source = REPO_ROOT / "native" / "cmd" / plugin / "main.go"
        text = source.read_text(encoding="utf-8")
        assert 'os.Getenv("METRICS_MANAGER_HOSTNAME")' in text
        assert "os.Hostname()" in text


class TestComposeWiring:
    """compose.yaml must expose the override knobs and mount points users rely on."""

    def test_telegraf_config_override_env_var(self, compose_text: str):
        # Users override the default config by setting TELEGRAF_CONFIG=./my.conf.
        assert "TELEGRAF_CONFIG" in compose_text
        assert "/etc/telegraf/telegraf.conf" in compose_text

    def test_telegraf_d_directory_mount(self, compose_text: str):
        # Drop-in directory for additional .conf files.
        assert "/etc/telegraf/telegraf.d" in compose_text

    def test_custom_metrics_volume_mounted(self, compose_text: str):
        assert "/app/custom-metrics" in compose_text
        assert "custom-metrics:" in compose_text

    def test_telegraf_ports_exposed(self, compose_text: str):
        assert "9273" in compose_text  # Prometheus
        assert "8186" in compose_text  # HTTP listener
        assert "9090" in compose_text  # API + SSE


class TestSupervisordOrdering:
    """Telegraf must come up before metrics-manager so the API has a Prometheus
    endpoint to scrape (for SSE) and an HTTP listener to push custom metrics to
    as soon as it starts serving requests."""

    def test_telegraf_priority_lower_than_metrics_manager(self, supervisord_text: str):
        # Lower priority => earlier start in supervisord. metrics-manager depends
        # on Telegraf at runtime (scrapes :9273, pushes to :8186), so Telegraf
        # must start first.
        ms_section = supervisord_text.split("[program:metrics-manager]", 1)[1].split(
            "[program:", 1
        )[0]
        tg_section = supervisord_text.split("[program:telegraf]", 1)[1].split("[program:", 1)[0]

        def _priority(section: str) -> int:
            for line in section.splitlines():
                if line.strip().startswith("priority="):
                    return int(line.split("=", 1)[1].strip())
            raise AssertionError("priority= missing from supervisord program section")

        assert _priority(tg_section) < _priority(ms_section)


class TestImageDirectories:
    """Dockerfile/entrypoint must create the directories that Telegraf reads from."""

    def test_dockerfile_creates_custom_metrics_dir(self):
        text = DOCKERFILE.read_text(encoding="utf-8")
        assert "/app/custom-metrics" in text

    def test_entrypoint_creates_custom_metrics_dir(self):
        text = ENTRYPOINT.read_text(encoding="utf-8")
        assert "/app/custom-metrics" in text

    def test_no_dead_collector_signals_references(self):
        # Legacy directory removed during OEP cleanup - regressions would
        # silently re-introduce an unused mount.
        for path in (DOCKERFILE, ENTRYPOINT):
            text = path.read_text(encoding="utf-8")
            assert (
                "collector-signals" not in text
            ), f"{path.name} still references the removed collector-signals directory"


@pytest.mark.integration
@pytest.mark.skipif(shutil.which("telegraf") is None, reason="requires the shipped Telegraf binary")
@pytest.mark.asyncio
@pytest.mark.parametrize("exporter_config", EXPORT_CONFIGS)
async def test_npu_fields_expire_while_other_fields_refresh(tmp_path: Path, exporter_config: Path):
    def unused_port() -> int:
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            return listener.getsockname()[1]

    input_port, output_port = unused_port(), unused_port()
    config = tomllib.loads(exporter_config.read_text())
    output = dict(config["outputs"]["prometheus_client"][0])
    output["listen"] = f"127.0.0.1:{output_port}"
    output["expiration_interval"] = "1s"
    output_text = "\n".join(f"{key} = {json.dumps(value)}" for key, value in output.items())
    config_path = tmp_path / "telegraf.conf"
    config_path.write_text(
        '[agent]\ninterval = "100ms"\nflush_interval = "100ms"\nomit_hostname = true\n'
        '[[inputs.http_listener_v2]]\n'
        f'service_address = "127.0.0.1:{input_port}"\n'
        'paths = ["/write"]\nmethods = ["POST"]\ndata_format = "influx"\n'
        f'[[outputs.prometheus_client]]\n{output_text}\n'
    )
    endpoint = f"http://127.0.0.1:{output_port}"
    with (tmp_path / "telegraf.log").open("w+") as log:
        process = subprocess.Popen(
            [shutil.which("telegraf"), "--config", str(config_path)],
            stdout=log, stderr=log,
        )
        try:
            async with httpx.AsyncClient() as client:
                async def wait_for(
                    fields: str, expected: dict[str, float | None],
                    previous_timestamps: dict[str, int] | None = None,
                ) -> dict:
                    deadline = time.monotonic() + 6
                    timestamp_ns = time.time_ns()
                    payload = {}
                    while time.monotonic() < deadline:
                        if process.poll() is not None:
                            log.seek(0)
                            pytest.fail(log.read())
                        try:
                            response = await client.post(
                                f"http://127.0.0.1:{input_port}/write",
                                content=f"npu,host=test frequency=1600000000,power=2,bandwidth=10{fields} {timestamp_ns if fields else time.time_ns()}\n",
                            )
                            response.raise_for_status()
                            with patch("app.sse.get_settings", return_value=SimpleNamespace(
                                prometheus_telegraf_endpoint=endpoint,
                            )):
                                payload = json.loads(await fetch_metrics())
                            values = {metric["name"]: metric["value"] for metric in payload["metrics"]}
                            for metric in payload["metrics"]:
                                if previous_timestamps and metric["name"] in previous_timestamps:
                                    assert metric["timestamp"] == previous_timestamps[metric["name"]]
                            if all(values.get(name) == value for name, value in expected.items()):
                                return payload
                        except httpx.TransportError:
                            pass
                        await asyncio.sleep(0.1)
                    pytest.fail(f"Expected {expected}, got {payload}")

                await wait_for(
                    ",utilization=50i,memory_mb=512", {"npu_utilization": 50, "npu_memory_mb": 512},
                )
                zero = await wait_for(
                    ",utilization=0i,memory_mb=0", {"npu_utilization": 0, "npu_memory_mb": 0},
                )
                timestamps = {
                    metric["name"]: metric["timestamp"] for metric in zero["metrics"]
                    if metric["name"] in {"npu_utilization", "npu_memory_mb"}
                }
                memory_only = await wait_for(
                    ",memory_mb=128", {
                        "npu_utilization": None, "npu_memory_mb": 128,
                        "npu_frequency": 1_600_000_000, "npu_power": 2,
                        "npu_bandwidth": 10,
                    }, {"npu_utilization": timestamps["npu_utilization"]},
                )
                timestamps = {
                    metric["name"]: metric["timestamp"] for metric in memory_only["metrics"]
                    if metric["name"] == "npu_memory_mb"
                }
                expired = await wait_for("", {
                    "npu_utilization": None, "npu_memory_mb": None,
                    "npu_frequency": 1_600_000_000, "npu_power": 2,
                    "npu_bandwidth": 10,
                }, timestamps)
                assert expired["timestamp"] >= memory_only["timestamp"] >= zero["timestamp"]
                await wait_for(
                    ",utilization=25i,memory_mb=0", {
                        "npu_utilization": 25, "npu_memory_mb": 0,
                        "npu_frequency": 1_600_000_000, "npu_power": 2,
                        "npu_bandwidth": 10,
                    },
                )
        finally:
            process.terminate()
            process.wait(timeout=5)


# -----------------------------------------------------------------------------
# Manual end-to-end test commands (require a running Docker daemon)
# -----------------------------------------------------------------------------
"""
1. Start the full stack:
   docker compose up -d

2. Verify Telegraf is publishing system metrics:
   curl http://localhost:9273/metrics | grep cpu_usage

3. Test custom script execution:
   docker exec metrics-manager sh -c 'cat > /app/custom-metrics/test.sh <<EOF
   #!/bin/sh
   echo "test_metric value=123"
   EOF
   chmod +x /app/custom-metrics/test.sh'
   # Wait ~10s for the exec input interval, then:
   curl http://localhost:9273/metrics | grep test_metric

4. Test custom telegraf.conf override:
   TELEGRAF_CONFIG=./my-telegraf.conf docker compose up -d

5. Confirm the SSE stream is delivering metrics:
   curl -N -H 'Accept: text/event-stream' http://localhost:9090/metrics/stream

6. Verify the NPU plugin (on NPU-equipped hosts only):
   docker exec metrics-manager ps -ef | grep mm-plugin-npu
   curl -s http://localhost:9273/metrics | grep '^npu_'
"""
