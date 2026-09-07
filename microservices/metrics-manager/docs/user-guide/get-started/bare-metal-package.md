<!--
Copyright (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# Metrics Manager — bare-metal package

Collects CPU, memory, GPU and NPU telemetry on an Intel edge system, publishes
it on a local Prometheus endpoint, and renders it in a terminal dashboard.

The package covers telemetry collection and the dashboard. The REST API and the
SSE stream remain container-only.

## Quick start

```bash
sudo apt install ./metrics-manager_<version>_amd64.deb
mm-tui
```

Everything starts at installation and again at boot. The raw readings are at
`http://127.0.0.1:9273/metrics`.

## Building the package

You need Docker and about 2 GB of free disk. You do **not** need a Go or Rust
toolchain on the machine: the collectors, Telegraf and qmassa are lifted out of
the container image, which pins its own toolchains, and the dashboard is
compiled with that same pinned Go image. The package and the image therefore
ship byte-identical binaries, so a reading that differs between the two
deployments cannot be blamed on a different build.

```bash
cd microservices/metrics-manager
./native/build.sh
```

The image is built first if it is not already present. The result lands in:

```
native/packaging/dist/metrics-manager_<version>_amd64.deb
```

While iterating on the dashboard or the packaging, reuse the image instead of
rebuilding it every time:

```bash
SKIP_IMAGE_BUILD=1 ./native/build.sh
```

The package is amd64 only. qmassa is Rust, and cross-building it for arm64 has
not been set up.

To check what you are about to install before installing it:

```bash
dpkg-deb --info native/packaging/dist/metrics-manager_*_amd64.deb
dpkg-deb --contents native/packaging/dist/metrics-manager_*_amd64.deb
```

## Installing and checking it works

```bash
sudo apt install ./native/packaging/dist/metrics-manager_*_amd64.deb
```

Installation creates the service account, loads the `msr` module, and starts
everything. Give it a few seconds, then check all six services came up:

```bash
systemctl list-units 'metrics-manager-*' --no-legend
```

Six units, all `active running`, is the answer you want. This form names them,
which `systemctl is-active` does not, and that matters when only one is
missing. Anything else has a reason in the journal:

```bash
journalctl -u 'metrics-manager-*' -n 40 --no-pager
```

Then look at the readings. The endpoint is the source of truth; the dashboard
only renders it.

```bash
curl -s http://127.0.0.1:9273/metrics | grep -v '^#' | sed 's/{.*//' | sort -u | wc -l
```

Expect roughly seventy metric families, though the exact number depends on the
hardware: a machine with no NPU publishes none of the `npu_` ones.

```bash
mm-tui
```

### What you should see, and how to tell it is real

The quickest way to trust a reading is to make it move.

**Memory bandwidth.** Watch the Memory panel while writing a few gigabytes into
RAM. It should jump from tens of MiB/s to thousands and settle back.

```bash
dd if=/dev/zero of=/dev/shm/mmtest bs=1M count=8000; rm -f /dev/shm/mmtest
```

**CPU and processes.** Load one core and watch the P row and the process table.
The busiest process should reach the top within a second or two.

```bash
timeout 20 sh -c 'while :; do :; done'
```

**Idle states.** On an unloaded machine the deepest state the part supports
should hold most of the residency. Under the load above, C0 rises.

Readings that stay at zero are not automatically wrong. GPU engines idle at
zero, an NPU with nothing scheduled reports zero utilisation and zero
frequency, and qmassa reports zero GPU power on some platforms. The
distinction the dashboard draws is between zero, which is a measurement, and
`—`, which means nothing published the metric at all.

Cross-check anything that looks suspicious against its source. The NPU is the
easiest, because the driver publishes the same facts in sysfs where anyone can
read them:

```bash
NPU=$(ls -d /sys/bus/pci/drivers/intel_vpu/0000:* | head -1)
cat "$NPU"/npu_memory_utilization "$NPU"/npu_max_frequency_mhz
curl -s http://127.0.0.1:9273/metrics | grep -E '^npu_(memory_mb|frequency_max)'
```

### Stopping it without removing it

```bash
sudo systemctl stop 'metrics-manager-*'
```

Stopping only Telegraf leaves the privileged collectors running with nowhere to
send samples; they will report the socket has gone and keep retrying. Stopping
all of them together avoids the noise. To start again:

```bash
sudo systemctl start 'metrics-manager-*' --all
```

`--all` is not optional here. A unit glob expands to units systemd currently
has loaded, and stopping them unloads them, so without it the command matches
nothing and silently starts nothing — systemd says as much in a warning that is
easy to scroll past.

To keep it installed but not running at boot:

```bash
sudo systemctl disable 'metrics-manager-*'
```

## What gets installed

| Path | Contents |
|---|---|
| `/usr/bin/mm-tui` | Terminal dashboard |
| `/usr/libexec/metrics-manager/` | Telegraf, qmassa and the three collector plugins |
| `/etc/metrics-manager/telegraf.conf` | Collection and publishing configuration |
| `/etc/metrics-manager/metrics-manager.env` | Settings shared by every service |
| `/etc/metrics-manager/tui-dashboard.json` | Dashboard thresholds, alert behavior and process display limit |
| `/etc/metrics-manager/tui-charts.json` | Trend history window, chart size and selected metrics |
| `/etc/metrics-manager/telegraf.d/` | Drop-in directory for local configuration |
| `/etc/metrics-manager/custom-metrics.d/` | Operator-supplied metric scripts |
| `/run/metrics-manager/` | Ingest socket and the qmassa FIFO |

## The dashboard

`mm-tui` reads the same Prometheus endpoint the package publishes, so it shows
exactly what any scraper would see. It keeps only a bounded in-memory trend
history, writes nothing, and needs no privileges; running it as an ordinary user
is enough.

```bash
mm-tui
```

| Key | Action |
|---|---|
| `q`, `Esc`, `Ctrl+C` | Quit |
| `Tab`, `Shift+Tab` | Switch between Overview and Trends |
| `1`, `2` | Open Overview or Trends directly |
| `↑` `↓`, `k` `j` | Scroll a line |
| `PgUp` `PgDn`, `Space` | Scroll a screen |
| `Home` `End`, `g` `G` | Jump to the top or the bottom |

Overview responds to the terminal width. It uses one column in a narrow window,
two columns once each panel can remain readable, and up to three columns in a
wide terminal. Scrolling remains available when the resulting rows do not fit
the terminal height. The panels retain their hardware-first reading order.

The footer reports which lines you are looking at, as in `lines 1-28 of 42`, so
a short terminal hides content rather than dropping it. The header reports how
long ago the last successful read was; when the endpoint stops answering, the
readings stay on screen and the age keeps climbing rather than the panel
emptying.

Panels appear only when the hardware is there: CPU and memory always, GPU once
qmassa reports a device, NPU on machines that have one. The GPU heading names
the kernel driver, as in `GPU 0 · i915`, because it decides what can be read.

What each panel shows:

| Panel | Contents |
|---|---|
| Header | CPU model, thread count, kernel release |
| CPU | Usage, frequency, package temperature and power against TDP, base and per-class turbo ceilings, uncore frequency, idle-state residency, and a row per core class with the evidence for the split |
| Memory | Used and available, and memory controller throughput |
| GPU | Power, temperature, memory, engine occupancy, per-tile frequencies and throttle reasons, and the processes using the device |
| NPU | Utilisation, frequency, power, temperature, bandwidth, memory, tile configuration |
| Processes | The busiest by CPU and by resident memory |

The Trends tab retains the most recent five minutes by default. It shows CPU
utilisation, memory usage, CPU temperature, and utilisation and temperature for
each available GPU and the NPU. Missing hardware produces no empty chart.
GPU utilisation is the busiest engine on that device for each sample. When
there are more samples than terminal columns, each horizontal bucket keeps its
peak so a short spike does not disappear during downsampling.

One figure is easy to misread. **Memory bandwidth is whole-system DRAM
throughput, not GPU bandwidth.** Neither Intel GPU driver publishes a
per-device byte counter: `i915` exposes engine occupancy, frequencies and
resident memory sizes but nothing counting transfers, and `xe` defines five
events in total, none of them a byte counter. It is reported under Memory
rather than under GPU for that reason, though on a machine whose GPU has no
dedicated memory the GPU is competing for exactly this bandwidth.

Two placeholders mean different things, and the difference is worth knowing:

| Shown | Meaning |
|---|---|
| `—` | The metric is absent. Nothing published it during this refresh. |
| `n/a` | The metric exists but this driver does not report it. |

`shared mem  n/a` on an `i915` GPU is the common case: unlike `xe`, that driver
does not expose shared memory usage, and the endpoint carries a zero for it. A
zero would read as "no memory in use", which is not what the kernel is saying,
so the dashboard declines to show one.

For a one-off reading, or to compare a machine against a container, render a
single frame instead of starting the interface:

```bash
mm-tui -once -width 100
```

A dashboard on another machine, or against the containerised service, is a
matter of pointing it elsewhere:

```bash
mm-tui -endpoint http://192.0.2.10:9273/metrics -interval 1s
```

The endpoint is unauthenticated and bound to loopback by default, so reaching
one across the network means you have deliberately exposed it. See
[Reachability](#reachability).

## Services and privileges

Six services run, and they deliberately do not share a privilege level. Each
collector holds the smallest privilege that was measured to work, and nothing
more.

| Service | Runs as | Why |
|---|---|---|
| `metrics-manager-telegraf` | `metrics-manager`, no capabilities | Collects unprivileged metrics, publishes the endpoint |
| `metrics-manager-proc` | `metrics-manager`, no capabilities | Busiest processes; needs to see other processes, not to be privileged |
| `metrics-manager-npu` | `metrics-manager` + `CAP_DAC_READ_SEARCH` | NPU telemetry is `0440 root:root` in sysfs |
| `metrics-manager-membw` | `metrics-manager` + `CAP_PERFMON` | Memory controller counters are read through perf |
| `metrics-manager-powerstat` | `metrics-manager` + `CAP_SYS_RAWIO`, `CAP_DAC_READ_SEARCH` | Package power, TDP and idle states come from MSRs and RAPL |
| `metrics-manager-qmassa` | `root` | GPU power and temperature come from model-specific registers |

Telegraf is the component with an attack surface: it listens on two loopback
sockets, parses whatever arrives on them, and executes the scripts in
`custom-metrics.d`. It is therefore the component that holds nothing worth
stealing. Collectors that need privileges are separate, single-purpose services
with no input of their own, and they hand their samples to Telegraf through the
unix socket at `/run/metrics-manager/collect.sock`.

Only qmassa runs as root, and only because upstream leaves no alternative:

```rust
if unsafe { libc::geteuid() } != 0 { return false; }   // qmlib/src/msr.rs
```

Both the perf-event path and the MSR fallback check this, so granting
`CAP_SYS_RAWIO`, `CAP_DAC_OVERRIDE` or `CAP_DAC_READ_SEARCH` to an unprivileged
user instead was measured to leave the power and temperature arrays empty even
though the MSR device itself opened successfully.

Being root is necessary but not sufficient, which matters if you harden the
unit further. Two settings in `metrics-manager-qmassa.service` are load-bearing,
and both fail silently — the readings disappear, nothing reports an error:

- `CapabilityBoundingSet` has to retain `CAP_SYS_RAWIO`. Uid 0 confers
  capabilities only up to that bound, so emptying it takes the MSR access away
  from root as surely as it would from anyone else. `CAP_PERFMON` alone is not
  enough on a host that reads the registers through the device node.
- `DeviceAllow` has to name the driver as `/proc/devices` does, which is
  `cpu/msr` and not `msr`. Under `DevicePolicy=closed` a rule that matches
  nothing is indistinguishable from no rule at all.

After changing either one, check that the readings survived:

```bash
curl -s http://127.0.0.1:9273/metrics | grep -E '^gpu_(power|temperature)'
```

## Reachability

Both listeners are bound to `127.0.0.1`:

- `127.0.0.1:9273` — Prometheus endpoint
- `127.0.0.1:8186` — InfluxDB Line Protocol ingest, `POST /write`

Neither is authenticated. Anything that can reach the ingest endpoint can
fabricate metrics, and anything that can reach the Prometheus endpoint learns
what the machine is doing. Put a reverse proxy that terminates TLS and performs
authentication in front of either one before exposing it off the host.

## Configuration

Settings shared by every service live in
`/etc/metrics-manager/metrics-manager.env` — the `host` tag and the qmassa
sampling interval. Telegraf itself is configured by
`/etc/metrics-manager/telegraf.conf`, with local additions in
`/etc/metrics-manager/telegraf.d/*.conf`, which is read afterwards and left
alone by upgrades.

```bash
sudo systemctl restart 'metrics-manager-*.service'
```

These files are marked as configuration, so an upgrade preserves your edits and
leaves the new packaged version alongside as `.dpkg-dist`.

### Dashboard thresholds and warnings

The dashboard colors monitored readings as OK, careful, warning or critical.
An alert appears only after the threshold is exceeded in three consecutive
samples. It clears after three consecutive samples below the threshold exit,
with a 5% hysteresis by default so a value close to a boundary does not make the
alert repeatedly appear and disappear.

The defaults and the maximum number of displayed processes are configured in
`/etc/metrics-manager/tui-dashboard.json`. The installed file documents the
complete JSON shape. After editing it, restart `mm-tui`; the collection services
do not need to be restarted.

The default thresholds are diagnostic starting points, not hardware safety
limits. Temperature behavior depends on the processor, accelerator, cooling
solution and sensor location, so tune the values for the deployed platform.
An existing configuration with invalid JSON, unknown fields, unsupported metric
names or unordered thresholds prevents the dashboard from starting and reports
the exact validation error instead of silently using different values.

By default, `PROCESS_LIMIT=10` in `metrics-manager.env` controls how many CPU and
memory candidates the collector publishes, while `processes.maxDisplayed=10`
controls how many combined rows `mm-tui` renders. Raising the collector limit
increases Prometheus series churn; raise only the display limit when the desired
processes are already present at the metrics endpoint.

### Trend chart configuration

`/etc/metrics-manager/tui-charts.json` controls the bounded history and the
order and range of the charts. The defaults are a five-minute window, 600
points per series (five minutes at the default 500 ms polling cadence), and
six-row charts. Supported metrics are:

- `cpu.totalPercent`
- `cpu.temperatureC`
- `memory.usedPercent`
- `memory.bandwidthMiBps`
- `gpu.utilizationPercent`
- `gpu.temperatureC`
- `npu.utilizationPercent`
- `npu.temperatureC`

Omit `min` or `max` to derive that side of the scale from the retained data.
The history duration accepts Go duration syntax such as `30s`, `5m` or `1h`.
The file is validated at startup, including metric names, duplicate charts,
ranges and memory bounds. Use `mm-tui -charts-config <path>` to try another
file without changing the packaged configuration. Both `historyDuration` and
`maxPoints` are retention limits; when `-interval` is reduced, raise
`maxPoints` if the configured time window must remain fully represented.

## Adding your own metrics

For anything that needs no privileges, drop an executable into
`/etc/metrics-manager/custom-metrics.d/`. It runs every ten seconds and writes
InfluxDB Line Protocol on stdout, in the style of `/etc/cron.daily`:

```bash
#!/bin/sh
printf 'fan_speed,location=intake rpm=%di\n' "$(cat /sys/class/hwmon/hwmon2/fan1_input)"
```

```bash
sudo install -m 0755 fan.sh /etc/metrics-manager/custom-metrics.d/fan
```

The directory is root-owned and not writable by the service user, because
Telegraf executes whatever is in it.

Programs that already speak line protocol can `POST` to the ingest endpoint
instead:

```bash
curl -X POST http://127.0.0.1:8186/write --data-binary 'build,job=nightly duration=91.4'
```

## Adding a collector that needs privileges

Scripts in `custom-metrics.d` inherit Telegraf's privileges, which are none. A
collector that has to read a root-owned sysfs file, open a device node or read
an MSR belongs on the other side of the boundary, as its own service.

Nothing in `telegraf.conf` changes: the socket multiplexes, so collectors are
added by installing units, not by editing configuration.

**1. Write to the socket instead of stdout.** Plugins built from this
repository take a flag for it:

```bash
/usr/libexec/metrics-manager/mm-plugin-example -out unix:///run/metrics-manager/collect.sock
```

Any program can do this — connect to the socket and write line protocol,
exactly as it would write to stdout.

**2. Install a unit modelled on `metrics-manager-npu.service`.** Grant the
smallest privilege that works, and check that it does rather than assuming:

- a root-owned file readable by its owner needs `CAP_DAC_READ_SEARCH`, not root
- a device node is usually better reached by adding the service user to the
  owning group than by granting a capability
- `User=root` is a last resort, for code that checks its own uid

Keep the rest of the hardening. A privileged collector should have
`PrivateNetwork=true`, `ProtectSystem=strict`, `NoNewPrivileges=true` and
`ReadWritePaths=/run/metrics-manager` and nothing more; it needs to read
hardware and write one socket.

**3. Verify what you granted.**

```bash
systemd-analyze security metrics-manager-example.service
```

**4. Confirm the metrics arrive.**

```bash
curl -s http://127.0.0.1:9273/metrics | grep '^example_'
```

**No GPU power on a machine using the xe driver.** qmassa reports zero for both
the graphics and package rails on Panther Lake, and it does so as plain root
outside the service too, so this is not the packaging holding it back. Use the
CPU panel's package power, which comes from a different source and does work
there.

**Fewer idle states than expected.** Which C-states a part implements varies:
Meteor Lake reports C0, C1, C6 and C7, Panther Lake only C0. The collector asks
for all of them and the plugin skips the unsupported ones with a single
warning, so a short list is the platform speaking rather than a fault.

**No turbo or base frequency.** Panther Lake publishes a max turbo of zero and
no base frequency at all. A ceiling of 0 MHz says nothing about the part, so
the dashboard omits it rather than showing it.

## Known limitations

What this tool cannot show, why, and what it would take to change that. Each
entry says how the claim was established, because "not supported" and "not
measured" are different things and only one of them is worth re-checking on new
hardware.

### Cannot be measured with current drivers

**Per-device GPU memory bandwidth.** Neither Intel GPU driver publishes a byte
counter per device. On `i915` four independent sources were checked on
hardware: the PMU exposes engine occupancy, semaphore and wait time,
frequencies, rc6 residency and interrupts; DRM `fdinfo` reports engine time in
nanoseconds and *resident* memory sizes rather than transfers; the card's sysfs
attributes are frequencies; and no OA metric sets are registered. On `xe` the
driver defines exactly five events — `gt-c6-residency`,
`engine-active-ticks`, `engine-total-ticks`, `gt-actual-frequency`,
`gt-requested-frequency` — confirmed both in the driver source and on a Panther
Lake machine. qmassa's own device state carries no bandwidth field either.

*Delivered instead:* whole-system DRAM throughput from the integrated memory
controller, reported under Memory rather than GPU. On a machine whose GPU has
no dedicated memory the GPU competes for exactly this bandwidth, so it is the
closest available signal; on a discrete card it says correspondingly less.

*To unblock:* a driver that exposes per-device byte counters. A discrete GPU
may expose counters on its own memory controller; that has not been tested.

**Per-process NPU utilisation.** The `intel_vpu` driver implements no
per-client accounting. Opening `/dev/accel/accel0` and reading
`/proc/self/fdinfo/<fd>` yields zero `drm-*` fields, where the same test on
`i915` and `xe` yields twenty-six. Verified on kernel 6.17 with Meteor Lake and
6.18 with Panther Lake. There is no NPU PMU either.

*To unblock:* a kernel where `intel_vpu` implements `show_fdinfo`. This is a
missing implementation rather than an architectural limit — the DRM usage-stats
interface already exists and both GPU drivers use it.

### Partially available

**P-state residency.** These machines run the `intel_pstate` driver with
hardware-managed states, so there is no `cpufreq/stats` directory and no
enumerable P-state table to attribute time to: the processor picks frequencies
in hardware along a continuum. What is reported instead is the base frequency,
the single-core turbo ceiling for each core kind, the uncore frequency, the
actual frequency per core class, and C-state residency.

*To unblock:* booting with `intel_pstate=disable` exposes
`cpufreq/stats/time_in_state` under `acpi-cpufreq`, at the cost of no longer
measuring how the machine actually runs.

**Which C-states exist varies by platform.** Meteor Lake reports C0, C1, C6 and
C7; Panther Lake reports only C0. Unsupported states are detected and skipped
with a single warning, so a short list is the platform speaking rather than a
fault.

### Not yet verified

**Discrete GPUs.** No discrete card was available. qmassa enumerates several
devices, the dashboard renders one panel per GPU, the VRAM fields are populated
and shown when non-zero, and the systemd device rule covers the whole DRM class
precisely so that additional card and render nodes are reachable. None of that
is a substitute for running it.

*To verify:* a machine with an Intel discrete GPU. Check for one panel per
device, non-zero `gpu_memory_vram_*`, and whether qmassa reports power for the
discrete card.

**NPU under load.** All readings were taken on an idle NPU, where they agree
exactly with the driver's own sysfs attributes: `npu_memory_utilization`
byte for byte, and `npu_busy_time_us` of zero matching a reported utilisation
of zero. What has not been seen is the counters rising.

*To verify:* any NPU inference workload, for example OpenVINO's
`benchmark_app -d NPU`, then confirm that utilisation, frequency, power and
bandwidth move.

### Upstream behaviour, not fixable here

**GPU power reads zero on Panther Lake.** qmassa returns zero for both the
graphics and package rails there. It does so as plain root outside the service
too, so this is not the sandboxing. Temperature and every other GPU reading
work. CPU package power comes from a different collector and does work, at
1.27 W against a 25 W thermal design power on that machine. No GPU `hwmon`
exposing power exists on that class of part.

**qmassa requires root.** Upstream gates both its perf-event and MSR paths on
the effective user id being zero, so no capability substitutes. One service of
six therefore runs as root; it reads hardware counters, writes one FIFO, and
accepts no input from anywhere.

### Deliberate scope decisions

- **amd64 only.** qmassa is Rust and an arm64 cross-build has not been set up.
- **Loopback and unauthenticated.** Both listeners bind to `127.0.0.1`. Put a
  reverse proxy that terminates TLS and authenticates in front of either one
  before exposing it.
- **Per-process series are capped**, ten by CPU and ten by resident memory for
  processes, ten per device for GPU clients. Those points are tagged with
  process ids, which are not reused predictably, so an uncapped stream would
  grow the series count for as long as a scrape runs.
- **Size.** The package is 92 MB and 324 MB installed, of which 301 MB is the
  official Telegraf binary, carried unmodified so that the package and the
  container image ship identical bytes. Telegraf's `custom_builder` would cut
  that substantially at the cost of that property.

## When something is missing

Start with the journal; every service logs there.

```bash
journalctl -u metrics-manager-telegraf -u metrics-manager-qmassa -u metrics-manager-npu -n 50
```

**No package power, TDP or idle states.** These come from
`metrics-manager-powerstat`, which needs the `msr` module for the same reason
qmassa does.

```bash
journalctl -u metrics-manager-powerstat -n 20
```

On a platform without a DRAM RAPL domain, adding
`current_dram_power_consumption` to `package_metrics` logs an error every
interval rather than being skipped. Unsupported C-states are handled
differently: the plugin reports one warning and carries on, so the default list
is safe to leave alone.

**No memory bandwidth.** The counters are read through perf, which the kernel
gates behind `perf_event_paranoid`. The collector says so on startup rather
than reporting zeros.

```bash
journalctl -u metrics-manager-membw -n 20
```

A machine whose memory controller exposes no free-running counters cannot
report this at all; the service idles instead of restarting in a loop.

**No process list.** `metrics-manager-proc` needs no privileges, but it does
need to see other processes. Hardening it with `ProtectProc=invisible`, or
mounting `/proc` with `hidepid`, leaves it able to see only itself.

**No GPU power or temperature, but the rest of the GPU panel works.** The `msr`
module is not loaded. Nothing reports this as an error — the readings are
simply absent.

```bash
lsmod | grep -w msr || sudo modprobe msr
```

If the module cannot be loaded at all, for instance because Secure Boot rejects
it, GPU power and temperature are not available on that machine.

**No GPU metrics at all.** qmassa blocks until the reader opens the FIFO, so
check that Telegraf is running first; then confirm the FIFO exists with the
right ownership.

```bash
ls -l /run/metrics-manager/qmassa.fifo    # expect: prw-r----- root metrics-manager
```

**No NPU metrics.** Either the machine has no NPU, or the capability was
dropped. The plugin says which on startup:

```bash
sudo -u metrics-manager /usr/libexec/metrics-manager/mm-plugin-npu -once
```

Run under `sudo` that will fail on the permissions — that is the expected way
to tell an absent NPU from a permissions problem.

**Nothing at all, and Telegraf will not start.** A stale socket is the usual
cause after an unclean shutdown; the unit clears it on start, so a restart is
normally enough.

```bash
sudo systemctl restart metrics-manager-telegraf
```

## Removal

```bash
sudo apt remove metrics-manager     # stops and removes the services
sudo apt purge metrics-manager      # also removes /etc/metrics-manager and the service account
```

To confirm nothing was left behind:

```bash
systemctl list-unit-files 'metrics-manager*' --no-legend | wc -l   # 0
id metrics-manager                                                # no such user
ls -d /etc/metrics-manager /run/metrics-manager /usr/libexec/metrics-manager
ls /etc/modules-load.d/metrics-manager.conf
ss -ltn | grep -E ':(9273|8186)'                                  # nothing
```

After a purge every one of those should be absent and the ports free.

The `msr` module is deliberately left loaded: something else on the machine may
be using it by the time the package is removed. Unload it by hand if you want
the machine back exactly as it was.

```bash
sudo modprobe -r msr
```
