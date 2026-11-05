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

## What gets installed

| Path | Contents |
|---|---|
| `/usr/bin/mm-tui` | Terminal dashboard |
| `/usr/libexec/metrics-manager/` | Telegraf, qmassa and the collector plugins |
| `/etc/metrics-manager/telegraf.conf` | Collection and publishing configuration |
| `/etc/metrics-manager/metrics-manager.env` | Settings shared by every service |
| `/etc/metrics-manager/telegraf.d/` | Drop-in directory for local configuration |
| `/etc/metrics-manager/custom-metrics.d/` | Operator-supplied metric scripts |
| `/run/metrics-manager/` | Ingest socket and the qmassa FIFO |

## Services and privileges

The services run without sharing a privilege level.

| Service | Runs as | Why |
|---|---|---|
| `metrics-manager-telegraf` | `metrics-manager`, no capabilities | Collects unprivileged metrics, publishes the endpoint |
| `metrics-manager-npu` | `metrics-manager` + `CAP_DAC_READ_SEARCH` | NPU telemetry is `0440 root:root` in sysfs |
| `metrics-manager-proc` | `metrics-manager`, no capabilities | Busiest processes |
| `metrics-manager-membw` | `metrics-manager` + `CAP_PERFMON` | Memory controller counters through perf |
| `metrics-manager-rapl` (opt-in) | `metrics-manager` + `CAP_DAC_READ_SEARCH` | Reads top-level RAPL energy counters for package and platform power |
| `metrics-manager-powerstat` | `metrics-manager` + `CAP_SYS_RAWIO`, `CAP_DAC_READ_SEARCH` | Package power and idle states |
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
`CAP_SYS_RAWIO`, `CAP_DAC_OVERRIDE` or `CAP_DAC_READ_SEARCH` instead was
measured to leave the power and temperature arrays empty even though the MSR
device itself opened successfully.

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
`/etc/metrics-manager/metrics-manager.env` — the `host` tag, Turbostat executable
and sampling interval, qmassa sampling interval and process limit. Telegraf itself is configured by
`/etc/metrics-manager/telegraf.conf`, with local additions in
`/etc/metrics-manager/telegraf.d/*.conf`, which is read afterwards and left
alone by upgrades.

| Variable | Default | Meaning |
| --- | --- | --- |
| `TURBOSTAT_INTERVAL` | `5` | Positive integer seconds between Turbostat samples |
| `TURBOSTAT_BIN` | `/usr/bin/turbostat` | Absolute host path, normally the distribution's kernel-selecting wrapper |
| `METRICS_MANAGER_HOSTNAME` | empty (kernel hostname) | Stable `host=` tag for all collectors |

From a source checkout, set these in `native/metrics-options.conf` and run
`native/configure-metrics.sh`, or pass a separate options file to that script:

```ini
TURBOSTAT_INTERVAL=2
TURBOSTAT_BIN=/usr/local/bin/turbostat
METRICS_MANAGER_HOSTNAME=lab-node-42
```

The helper persists selected values in the shared environment file before
restarting running services. Missing settings remain unchanged; an empty
`METRICS_MANAGER_HOSTNAME=` restores the kernel hostname. Values are unquoted
literals, with no shell expansion or spaces in binary paths; host tags accept
letters, digits, `_`, `.`, `:` and `-`. `auto` is only for `ENABLE_*` options.
Setting startup options does not enable Turbostat: use `ENABLE_TURBOSTAT=true`
for that. The helper checks the effective binary with `--version` when enabling
the input or changing its path.

For direct edits, restart with
`sudo systemctl try-restart 'metrics-manager-*.service'`; an environment-only
edit needs no `daemon-reload`. The Turbostat drop-in must use
`path = "${TURBOSTAT_BIN}"` and `interval = "${TURBOSTAT_INTERVAL}s"`.
The helper migrates explicitly selected fields in both active and disabled
`powerstat.d/turbostat.conf` files, preserving other local settings.
The service supplies defaults if an upgrade retains an older environment file.

Native deployment does not use container bind mounts or `make up` binary
resolution. Install kernel-matched tools and choose an executable accessible
inside the powerstat service's sandbox (not in a home or private temporary
directory). Recheck version-specific paths after a kernel upgrade.

Optional collectors are shipped as examples, not enabled by installation.
Install only those needed on this machine (do not overwrite an existing,
locally edited drop-in):

First make sure the current package is installed with
`dpkg-query -W metrics-manager`, then check the desired example exists under
`/usr/share/doc/metrics-manager/examples/`. If `dpkg-query` reports no
package or the example or service unit is missing, build and install the
current `.deb` from `native/packaging/dist/` before running the commands below.
Building the package alone does not install its examples or systemd units;
installation starts the Metrics Manager services. A reused version number
does not guarantee an older installation has these newer files.

```bash
sudo install -o root -g root -m 0644 /usr/share/doc/metrics-manager/examples/telegraf-network.conf /etc/metrics-manager/telegraf.d/network.conf
sudo install -o root -g root -m 0644 /usr/share/doc/metrics-manager/examples/telegraf-temperature.conf /etc/metrics-manager/telegraf.d/temperature.conf
sudo install -o root -g root -m 0644 /usr/share/doc/metrics-manager/examples/telegraf-interrupts.conf /etc/metrics-manager/telegraf.d/interrupts.conf
sudo systemctl restart metrics-manager-telegraf.service
```

The network example enables both `net` and `ethtool`. Temperature adds 100 ms
package samples and one-second min/max/mean alongside the existing one-second
reading; interrupts adds numbered device IRQ counters. `ethtool` fields depend
on the NIC driver. If `network.conf` is already installed, add the
`[[inputs.ethtool]]` block from the updated example to that file rather than
reinstalling it and losing your interface selection. Each corresponding TUI
panel appears only after its metrics reach the endpoint. Remove an example
from `telegraf.d` and restart the service to disable it.

### Metric names and network namespaces

The native GPU collector already provides the container's additional GPU
frequency and throttling readings under different Prometheus names:

| Container name | Native name |
|---|---|
| `gpu_throttle_act_freq_mhz` | `gpu_frequency_act_freq` |
| `gpu_throttle_driver_freq_mhz` | `gpu_frequency_cur_freq` |
| `gpu_throttle_throttled` | `gpu_throttle_status` |
| `gpu_throttle_throttle_pl1` | `gpu_throttle_pl1` |
| `gpu_throttle_throttle_pl2` | `gpu_throttle_pl2` |
| `gpu_throttle_throttle_prochot` | `gpu_throttle_prochot` |
| `gpu_throttle_throttle_thermal` | `gpu_throttle_thermal` |

Native GPU series identify devices and tiles with `gpu_id`, `driver` and `tile`
rather than `card` and `gt`; queries must use the corresponding native labels.

The container's `eth0` can be a `veth` device, whereas the native service reads
the host's network namespace. The `veth` driver exports `peer_ifindex`,
`rx_pp_*` page-pool counters and per-queue XDP/drop counters that a physical
NIC driver such as `e1000e` does not provide. These are not missing collectors:
the existing `ethtool` input collects them when a matching interface is selected.
To include host-side container links, set `ENABLE_NETWORK_VIRTUAL=true` in
`native/metrics-options.conf` and run `native/configure-metrics.sh` after
installing the current package. This manages a separate `network-virtual.conf`
drop-in selecting `veth*` and `lxc*`, independently of `ENABLE_NETWORK`.
Use `false` to disable it or `auto` to leave it unchanged. Existing local
drop-ins are preserved. Adjust its patterns to local interface names and
avoid selecting the same interfaces in another `inputs.ethtool` block.
This does not select `eth0` inside a container or change `inputs.net`:
including virtual interfaces there can double-count traffic. The host peer
has its own counters and `peer_ifindex`, not the container endpoint's values.
Unsupported driver counters are omitted, not synthesized as zero.

### Additional privileged collectors

Extra per-CPU powerstat measurements need the **privileged** Telegraf instance,
not the regular `telegraf.d` directory:

```bash
sudo install -o root -g root -m 0644 /usr/share/doc/metrics-manager/examples/telegraf-powerstat-extra.conf /etc/metrics-manager/powerstat.d/extra.conf
sudo systemctl restart metrics-manager-powerstat.service
```

This adds per-CPU frequency and temperature without duplicating package power
or C-state series. On a host with a readable DRAM RAPL subzone, edit
`/etc/metrics-manager/powerstat.d/extra.conf` to set
`package_metrics = ["current_dram_power_consumption"]`, then restart the
powerstat service. Do not enable this on hosts without that subzone: Telegraf
logs a collection error on every interval. Changes to the main powerstat
configuration remain independent of this optional drop-in.

For opt-in RAPL package and platform (`psys`) power, start the separately
packaged collector; it does not require a Telegraf drop-in:

```bash
sudo systemctl enable --now metrics-manager-rapl.service
curl -fsS http://127.0.0.1:9273/metrics | grep '^rapl_power_power_w'
```

Use `sudo systemctl disable --now metrics-manager-rapl.service` to stop it
and keep it off after reboot. Its TUI panel appears only after samples arrive.

For opt-in IPC/SMI and other Turbostat diagnostics, first install a turbostat
binary matching the running kernel and verify the configured
`TURBOSTAT_BIN --version` succeeds
(`--help` can return a nonzero exit status even when the binary works).
Turbostat reads CPU MSRs, so its example belongs in the **privileged**
powerstat directory:

```bash
sudo install -o root -g root -m 0644 /usr/share/doc/metrics-manager/examples/telegraf-turbostat.conf /etc/metrics-manager/powerstat.d/turbostat.conf
sudo systemctl restart metrics-manager-powerstat.service
curl -fsS http://127.0.0.1:9273/metrics | grep -E '^turbostat_(ipc|smi)'
```

Rename `turbostat.conf` to `turbostat.conf.disabled` and restart
`metrics-manager-powerstat.service` to disable it without stopping baseline
powerstat collection. The NPU collector now also exports `npu_power_state`:
`0`–`4` mean `D0`–`D3cold`; `-1` means unavailable or unknown.

```bash
sudo systemctl restart 'metrics-manager-*.service'
```

Both files are marked as configuration, so an upgrade preserves your edits and
leaves the new packaged version alongside as `.dpkg-dist`.

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

## When something is missing

Start with the journal; every service logs there.

```bash
journalctl -u metrics-manager-telegraf -u metrics-manager-qmassa -u metrics-manager-npu -n 50
```

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

**No memory bandwidth.** `mm-plugin-membw` reports system-wide DRAM read,
write and total throughput in MiB/s (not GPU-only bandwidth). Check
`journalctl -u metrics-manager-membw.service` and
`cat /proc/sys/kernel/perf_event_paranoid`. On kernels that set this value to
4, `CAP_PERFMON` alone may be refused for uncore events; the service will stay
running but idle with a `permission denied` diagnostic. A root diagnostic
`sudo /usr/libexec/metrics-manager/mm-plugin-membw -once` can confirm that
the counters themselves work.
An administrator may choose a less restrictive perf policy, after assessing
the information-exposure implications; do not grant the service broad
`CAP_SYS_ADMIN` merely to make the reading appear. Restart the membw service
after changing host perf policy so it can reopen the counters.

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

The `msr` module is deliberately left loaded: something else on the machine may
be using it by the time the package is removed.
