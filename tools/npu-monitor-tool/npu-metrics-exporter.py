#!/usr/bin/python3

# Copyright 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# pylint: disable=line-too-long,missing-module-docstring,invalid-name,too-many-locals,too-many-statements

import traceback
import time
import os
import sys
import logging as LOG
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from types import ModuleType

from flask import Flask, Response
from enum import Enum, unique
from prometheus_client import (
    CollectorRegistry,
    Gauge,
    generate_latest
)

KB = 1024

def load_npu_monitor() -> ModuleType:
    """Dynamically load the standalone monitor module."""
    module_path = Path(__file__).resolve().parent / 'npu-monitor-tool.py'
    spec = spec_from_file_location('npu_monitor_tool', module_path)
    if spec is None or spec.loader is None:
        raise ImportError(f'Unable to load monitor module from {module_path}')

    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

npu_monitor_tool = load_npu_monitor()
NpuMonitor = npu_monitor_tool.NpuMonitor
noc_bandwidth_rate_mbs = npu_monitor_tool.noc_bandwidth_rate_mbs

# NPU Monitor
npu_mon = NpuMonitor()
pu = npu_mon.get_pmt_telemetry()
if pu is None:
    print('Failed to setup NPU monitor')
    sys.exit(1)

pciid = npu_mon.read_pciid()

# Prometheus data source registry
registries = {}
metrics_owner = f'npu'
registry, metrics = registries.setdefault(
                        metrics_owner, (CollectorRegistry(), {}))

# Get initial value for interval-related metrics (utilization and power)
curr_time_ms = 0
prev_time_ms = time.monotonic_ns() // 1_000_000
prev_busy_time = npu_mon.read_busy_time()
pu.update_buffer()
prev_energy = pu.get_npu_energy()
prev_mem_bandwidth_mb = pu.get_noc_bandwidth()
prev_mem_bandwidth_ts = time.monotonic()

@unique
class PromMetric(Enum):
    desc: str
    ext_labelnames: list[str]

    npu_monitor_temperature = ('npu_monitor_temperature', 'NPU Temperature [°C]') # nopep8
    npu_monitor_freq_hz = ('npu_monitor_freq_hz', 'DPU Frequency [Hz]') # nopep8
    npu_memory_util = ('npu_memory_util', 'NPU Memory Utilization [MB]') # nopep8
    npu_memory_bandwidth =('npu_memory_bandwidth', 'NPU DDR Average Bandwidth [MB/s]') # nopep8
    npu_util = ('npu_util', 'NPU Utilization [%]') # nopep8
    npu_monitor_power = ('npu_monitor_power', 'NPU Power Consumption [Watts]') # nopep8
    npu_tile_config = ('npu_tile_config', 'NPU Tile Configuration') # nopep8

    def __new__(cls, name, desc=None, ext_labelnames=[]):
        obj = object.__new__(cls)
        obj._value_ = name
        obj.desc = f'{name}_desc' if desc is None else desc
        obj.ext_labelnames = ext_labelnames
        return obj

class Metric:
    def __init__(self, prom_metric: PromMetric) -> None:
        self.prom_metric = prom_metric

metrics_map = {
    'NPU_TEMPERATURE': Metric(PromMetric.npu_monitor_temperature),
    'NPU_FREQUENCY': Metric(PromMetric.npu_monitor_freq_hz),
    'NPU_MEM_UTIL': Metric(PromMetric.npu_memory_util),
    'NPU_MEM_BANDWIDTH': Metric(PromMetric.npu_memory_bandwidth),
    'NPU_UTIL': Metric(PromMetric.npu_util),
    'NPU_POWER': Metric(PromMetric.npu_monitor_power),
    'NPU_TILE_CFG':  Metric(PromMetric.npu_tile_config),
}

def set_or_remove_gauge(
    metric_key: str, labelnames: list[str], labelvalues: list[str],
    value: float | int | None,
) -> None:
    """Remove unavailable samples, including values exposed by earlier scrapes."""
    metric = metrics_map[metric_key]
    metric_name = metric.prom_metric.name
    gauge = metrics.get(metric_name)
    if value is None:
        if gauge is not None:
            gauge.remove(*labelvalues)
        return
    if gauge is None:
        gauge = Gauge(metric_name, metric.prom_metric.desc,
                      labelnames=labelnames, registry=registry)
        metrics[metric_name] = gauge
    gauge.labels(*labelvalues).set(value)


def process_npu_temperature() -> None:
    set_or_remove_gauge('NPU_TEMPERATURE', ['dev_id'], [str(pciid)], pu.get_npu_temperature())


def process_npu_frequency() -> None:
    set_or_remove_gauge('NPU_FREQUENCY', ['dev_id'], [str(pciid)], pu.get_freq_hz())


def process_npu_mem_util() -> None:

    mem_util = npu_mon.read_mem_util()
    mem_util_mb = None
    if mem_util is not None:
        try:
            mem_util_mb = int(mem_util) / KB / KB
        except (TypeError, ValueError) as err:
            LOG.warning('Failed to parse memory utilization: %s', err)
    set_or_remove_gauge('NPU_MEM_UTIL', ['dev_id'], [str(pciid)], mem_util_mb)

def process_npu_mem_bandwidth() -> None:
    global prev_mem_bandwidth_mb
    global prev_mem_bandwidth_ts

    try:
        curr_mem_bandwidth_mb = pu.get_noc_bandwidth()
    except (TypeError, ValueError) as err:
        LOG.warning('Failed to parse memory bandwidth: %s', err)
        curr_mem_bandwidth_mb = None

    curr_mem_bandwidth_ts = time.monotonic()
    delta_ts = curr_mem_bandwidth_ts - prev_mem_bandwidth_ts

    bandwidth_mbps = None
    if delta_ts > 0:
        if prev_mem_bandwidth_mb is not None and curr_mem_bandwidth_mb is not None:
            bandwidth_mbps = noc_bandwidth_rate_mbs(prev_mem_bandwidth_mb, curr_mem_bandwidth_mb, delta_ts)
        prev_mem_bandwidth_mb = curr_mem_bandwidth_mb
        prev_mem_bandwidth_ts = curr_mem_bandwidth_ts
    else:
        LOG.warning('Non-positive bandwidth interval; keeping the previous counter baseline.')
    set_or_remove_gauge('NPU_MEM_BANDWIDTH', ['dev_id'], [str(pciid)], bandwidth_mbps)

def process_npu_utilization() -> None:
    global curr_time_ms
    global prev_time_ms
    global prev_busy_time

    curr_busy_time = npu_mon.read_busy_time()
    interval_us = (curr_time_ms - prev_time_ms) * 1000
    utilization = None
    if interval_us > 0:
        if prev_busy_time is not None and curr_busy_time is not None and curr_busy_time >= prev_busy_time:
            utilization = min(100, int(100 * (curr_busy_time - prev_busy_time) / interval_us))
        prev_busy_time = curr_busy_time
    set_or_remove_gauge('NPU_UTIL', ['dev_id'], [str(pciid)], utilization)

def process_npu_power() -> None:
    global curr_time_ms
    global prev_time_ms
    global prev_energy

    curr_energy = pu.get_npu_energy()
    interval_ms = curr_time_ms - prev_time_ms
    power = None
    if interval_ms > 0:
        if prev_energy is not None and curr_energy is not None and curr_energy >= prev_energy:
            power = (curr_energy - prev_energy) / (interval_ms * 1e-3)
        prev_energy = curr_energy
    set_or_remove_gauge('NPU_POWER', ['dev_id'], [str(pciid)], power)

def process_npu_tile_config() -> None:
    set_or_remove_gauge('NPU_TILE_CFG', ['dev_id'], [str(pciid)], pu.get_tile_config())

def tidy_response(resp):
    resp_str = resp.decode('UTF-8')
    tidy_resp = []
    is_comment = False
    for line in resp_str.splitlines():
        if not line.startswith('#'):
            if line not in tidy_resp:
                tidy_resp.append(line)
                is_comment = False
        else:
            if is_comment == False:
                tidy_resp.append('\n' + line)
                is_comment = True
            else:
                tidy_resp.append(line)

    return '\n'.join(tidy_resp)

def get_metrics():
    global curr_time_ms
    global prev_time_ms

    try:
        curr_time_ms = time.monotonic_ns() // 1_000_000
        pu.update_buffer()

        process_npu_temperature()
        process_npu_frequency()
        process_npu_mem_util()
        process_npu_mem_bandwidth()
        process_npu_utilization()
        process_npu_power()
        process_npu_tile_config()

        if curr_time_ms > prev_time_ms:
            prev_time_ms = curr_time_ms

        resp = generate_latest(registry)
        return tidy_response(resp)

    except Exception as e:
        traceback.print_exc()
        return "#nodata: due to unexpected failure", 500

def export_metrics():
    result = get_metrics()

    # Handle (body, status) or just body
    if isinstance(result, tuple):
        body, status = result
        body = body + '\n'
    else:
        body, status = result + '\n', 200

    return Response(body, status=status, content_type='text/plain')

def hello_world():
    body = "NPU telemetry source for Prometheus\n"
    status = 200
    return Response(body, status=status, content_type='text/plain')

app = Flask(__name__)

app.add_url_rule(
    '/', view_func=hello_world, methods=['GET'])

app.add_url_rule(
    '/metrics', view_func=export_metrics, methods=['GET'])

