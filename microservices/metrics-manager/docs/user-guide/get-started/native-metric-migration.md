<!--
SPDX-FileCopyrightText: (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# Native Metric Migration for 2026.3.0

The unreleased 2026.3.0 CPU/GPU/DRAM native collectors use bytes, bytes/second, Hz, W, and
degrees Celsius with explicit unit-bearing field names. This is a **breaking,
one-time contract migration** for the native Docker image and Debian package. Old names
are not emitted as aliases. Upgrade collectors and dashboards together.

NPU retains the established names and mixed wire units documented below:
frequency in Hz, power in W, temperature in degrees Celsius, bandwidth in
decimal MB/s, memory in binary MiB, and the frequency ceiling in MHz.
Intermediate unit-suffixed NPU names are removed, not retained as aliases.

Standard Telegraf inputs (including powerstat, Turbostat, CPU, memory, network,
and disk) are unchanged. So are the qmassa JSON input schema, engine/CPU
utilization percentages, counts, state/throttle flags, process resident set size (RSS)
bytes, and `rapl_power_power_w`. Percentages remain 0–100 (or the existing multi-core/
multi-engine semantics), not newly introduced 0–1 ratios.

## Prometheus Names and Conversions

Multiply the previous value by the factor below to obtain the new value.
For GPU memory, `{...}` denotes the existing memory field names; all fields
from qmassa's device/client memory maps receive the `_bytes` suffix.
Tags, device/tile identities, and collector selection behavior are preserved.

| Previous name                                            | New name                                               | Factor    |
| -------------------------------------------------------- | ------------------------------------------------------ | --------- |
| `cpu_frequency_avg_frequency`                            | `cpu_frequency_avg_frequency_hz`                       | 1,000     |
| `cpu_core_class_frequency_avg`                           | `cpu_core_class_frequency_avg_hz`                      | 1,000     |
| `gpu_frequency` (legacy `value` alias)                   | `gpu_frequency_cur_freq_hz`                            | 1,000,000 |
| `gpu_frequency_{act,cur,min,max}_freq`                   | `gpu_frequency_{act,cur,min,max}_freq_hz`              | 1,000,000 |
| `gpu_power`                                              | `gpu_power_power_w`                                    | 1         |
| `gpu_temperature`                                        | `gpu_temperature_temperature_c`                        | 1         |
| `gpu_fan` (RPM)                                          | `gpu_fan_speed_hz` (rotations/second)                  | 1/60      |
| `gpu_memory_{smem_total,smem_used,vram_total,vram_used}` | Same names with `_bytes` appended                      | 1         |
| `gpu_client_{smem_used,smem_rss,vram_used,vram_rss}`     | Same names with `_bytes` appended                      | 1         |
| `npu_power_w` (intermediate)                             | `npu_power`                                            | 1         |
| `npu_frequency_hz` (intermediate)                        | `npu_frequency`                                        | 1         |
| `npu_temperature_c` (intermediate)                       | `npu_temperature`                                      | 1         |
| `npu_bandwidth_bytes_per_second` (intermediate)           | `npu_bandwidth`                                        | 1/1,000,000 |
| `npu_memory_bytes` (intermediate)                        | `npu_memory_mb`                                        | 1/1,048,576 |
| `npu_frequency_max_hz` (intermediate)                    | `npu_frequency_max_mhz`                                | 1/1,000,000 |
| `memory_bandwidth_{read,write,total}_mibps`              | `memory_bandwidth_{read,write,total}_bytes_per_second` | 1,048,576 |

Line-protocol measurement names stay unchanged; their fields are renamed
accordingly. For example, `npu` has `frequency` and `memory_mb`;
`gpu_power` has `power_w` instead of `value`. There is no second GPU
`value` field repeating requested frequency.

The NPU frequency factor assumes the already-corrected native Hz calculation.
Earlier uncorrected builds reported values 1e6 too small and also used the wrong Intel® Core™
Ultra Series 2 phase-locked loop (PLL) formula; their historical data needs separate
correction, not just a rename. The current formulas remain MTL/ARL `ratio * 50e6 * 2/3` and
LNL/PTL `ratio * 50e6 / 2`.

## Display and Query Units

Convert raw metrics at the display or query boundary:

- Clocks: divide Hz by `1e6` for MHz or `1e9` for GHz.
- CPU/GPU memory sizes: divide bytes by `1048576` for MiB or `1073741824` for GiB.
- Whole-system DRAM throughput: divide bytes/s by `1e9` for decimal GB/s.
- NPU memory and throughput: `npu_memory_mb` is already MiB and
   `npu_bandwidth` is already decimal MB/s; do not convert either again.

Examples for external dashboards:

```promql
gpu_frequency_act_freq_hz / 1e6
npu_memory_mb
memory_bandwidth_total_bytes_per_second / 1e9
gpu_fan_speed_hz * 60
```

Do **not** apply `rate()` again to bandwidth metrics: they are already rates.
The memory-controller collector converts the sysfs MiB-per-event scale to
bytes before dividing by elapsed time. NPU bandwidth preserves actual sampling
interval and single 32-bit counter-wrap handling. Missing optional NPU data
stays absent; measured zeros stay zero. Memory is no longer rounded to two
decimal places of MiB before export.

MTL/ARL do not expose the required memory-traffic counter, so bandwidth is
omitted rather than reading offset zero. Memory reporting instead follows
sysfs availability on every generation. Invalid busy-time reads and resets
require a fresh baseline; a measured idle interval is still zero. Negative
power after an energy-counter decrease is omitted. Truncated PMT snapshots
do not advance any baselines. Non-positive elapsed time is rejected.

The shipped container, Debian and ViPPET Prometheus outputs use
`expiration_interval = "15s"` for all fields, preserving default 10-second
inputs while allowing missing NPU fields to disappear. Retained custom
Telegraf configurations must be updated separately. UI/history use `N/A`
and gaps for missing samples; harness reports convert Hz to MHz at their
boundary and keep KPI statuses, counts and warnings separate from numbers.

## Upgrade Checklist

1. Upgrade the native plugins for every producer read by your dashboards.
   Restart/recreate the selected services so old collector processes stop.
2. Replace old names in PromQL, recording/alert rules, and custom ingestion
   clients. Rescale numeric thresholds using the table; do not splice old and
   new raw values into one history without conversion.
3. Merge new examples into locally retained configuration files. Mounted
   configurations are not automatically overwritten.
4. Verify the new metric families at `/metrics`, check device/tile labels and
   missing-data behavior, and compare dashboard values after display conversion.

## TUI Configuration Migration

Upgrade `mm-tui` with the native collectors. The
[terminal dashboard](./terminal-dashboard.md) performs display conversions;
do not pre-convert the Prometheus endpoint or it will scale the values twice.

For retained custom chart configuration, replace the old DRAM chart IDs
`memory.bandwidthMiBps`, `memory.readMiBps`, and `memory.writeMiBps` with
`memory.bandwidthGBps`, `memory.readGBps`, and `memory.writeGBps`. Use `GB/s`
as the display unit and convert any fixed MiB/s bounds by multiplying by
`1048576 / 1e9`. Merge the current examples rather than discarding local settings.

## Debian Package Upgrades

Install the new package with `apt` so plugins and the dashboard are upgraded
together. Locally modified configuration is preserved: compare any `.dpkg-dist`
files with the active configuration, merge the needed changes, and restart
`mm-tui`. In particular, retained chart files may still use the old DRAM IDs.
Do not replace local settings wholesale or run old and new metric contracts
through the same unconverted history.
