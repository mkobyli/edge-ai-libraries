<!--
Copyright (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# Additional collectors in the native-enabled Docker image

This guide applies to the Docker image that bundles the Go `mm-plugin-*`
collectors. It does **not** configure the [bare-metal package](./bare-metal-package.md)
or the Python-reader image with `ENABLE_RAPL_POWER`, `ENABLE_DRAM_BW`,
`ENABLE_PSYS_POWER` and other entrypoint switches.

The native-enabled container does not implement those collector switches.
Likewise, `native/metrics-options.conf` and `native/configure-metrics.sh` configure
installed systemd services, not Docker containers. In particular, adding
`ENABLE_*` variables to a container environment does not enable these inputs.

## What is already enabled

The shipped `telegraf.conf` starts CPU, GPU, NPU, process and memory-bandwidth
collectors, alongside Telegraf CPU, memory, package-temperature and
`intel_powerstat` package-power/C-state inputs. GPU throttle/frequency detail is
already part of the native GPU collector. Supported readings depend on the
hardware, driver interfaces and container privileges.

The following are optional:

| Additional coverage | Configuration or bundled executable |
|---|---|
| Per-CPU frequency and temperature | `native/packaging/etc/telegraf-powerstat-extra.conf.example` |
| 100 ms package temperature and 1 s min/max/mean | `native/packaging/etc/telegraf-temperature.conf.example` |
| Numbered device IRQ counters | `native/packaging/etc/telegraf-interrupts.conf.example` |
| Network traffic and driver counters | `native/packaging/etc/telegraf-network.conf.example` |
| Filesystem usage and disk I/O | `native/packaging/etc/telegraf-storage.conf.example` |
| RAPL package and platform (`psys`) power | `/usr/libexec/metrics-manager/mm-plugin-rapl` |
| IPC, SMI and other Turbostat diagnostics | `native/packaging/etc/telegraf-turbostat.conf.example` plus a compatible host binary |

These example files also serve the bare-metal package. Their systemd restart
instructions and split privileged/unprivileged directories do not apply to the
single privileged container used below.

## How configuration is loaded

The current container starts Telegraf with:

```text
telegraf --config /etc/telegraf/telegraf.conf
```

It does **not** pass `--config-directory`. Although the repository's Compose
file mounts `/etc/telegraf/telegraf.d`, that mount alone does not load its files.
For this image, combine the required input definitions into one configuration
and mount it at `/etc/telegraf/telegraf.conf`, as below. Do not copy entire output
or agent configurations into it: keep only one agent, Prometheus output and
HTTP listener. Do not add a second copy of inputs already present in the base.

## Complete Docker Compose example

Run the following from `microservices/metrics-manager` in a checkout matching
your image. The example uses an existing image; it does not rebuild it.

### 1. Check the image and hardware prerequisites

Set the tag of your native-enabled image, for example:

```bash
export METRICS_MANAGER_IMAGE=metrics-manager:2026.3.0
docker image inspect "$METRICS_MANAGER_IMAGE" > /dev/null
```

Use your local tag instead if different. This recipe requires the image to
contain `mm-plugin-rapl` as well as the default native collectors.

- Use Linux with the required GPU/NPU drivers and readable telemetry interfaces.
- The example grants `privileged: true`, host PID visibility and read-only
  `/sys` and `/run` mounts. This is broad host access; run only a trusted image.
- For MSR-based powerstat and Turbostat fields, check `/dev/cpu/0/msr`. If missing,
  an administrator must load the host's `msr` module, where supported.
- Native DRAM bandwidth needs accessible IMC PMU counters and permission to open
  perf events. It does **not** require a `perf` executable or `INSTALL_PERF=true`.
- Turbostat requires a real binary compatible with the host hardware/kernel and
  the container's libraries. On Ubuntu, check:

```bash
export HOST_TURBOSTAT_BIN="/usr/lib/linux-tools/$(uname -r)/turbostat"
test -x "$HOST_TURBOSTAT_BIN"
"$HOST_TURBOSTAT_BIN" --version
```

If either check fails, stop and install matching host tools or select the correct
binary path. Do not mount a wrapper that relies on host-only paths as the binary.
The path may change after a kernel upgrade. The startup check below also verifies
that the mounted binary runs **inside** the image.

### 2. Assemble the selected inputs

The following enables all the optional inputs in the table. To select a subset,
omit unwanted example files and the RAPL block. Re-running these commands
replaces the generated `telegraf.docker.conf`; preserve any local edits first.

```bash
cat telegraf.conf \
  native/packaging/etc/telegraf-powerstat-extra.conf.example \
  native/packaging/etc/telegraf-temperature.conf.example \
  native/packaging/etc/telegraf-interrupts.conf.example \
  native/packaging/etc/telegraf-network.conf.example \
  native/packaging/etc/telegraf-storage.conf.example \
  native/packaging/etc/telegraf-turbostat.conf.example \
  > telegraf.docker.conf

cat >> telegraf.docker.conf <<'EOF'

[[inputs.execd]]
  command = ["/usr/libexec/metrics-manager/mm-plugin-rapl"]
  signal = "none"
  restart_delay = "10s"
  data_format = "influx"
EOF
```

Review network interface, disk device and filesystem selections before starting.
The storage example includes partitions as well as whole disks: select the
appropriate layer rather than summing both. Network and filesystem namespace
limitations are described below.

The disk I/O input requests `ID_MODEL` and `ID_SERIAL_SHORT` device tags and
retains `wwid` when supplied by udev/the kernel. Their availability depends on
the device and visible metadata; the read-only `/run` mount provides the host's
udev database. To update an older generated configuration, regenerate it from
the current example or add `device_tags = ["ID_MODEL", "ID_SERIAL_SHORT"]`
and remove `wwid` exclusions from its
`[[inputs.diskio]]` block, then restart Telegraf.

The extra powerstat input only adds per-CPU frequency/temperature, avoiding
duplicate baseline package/C-state collection. Its `package_metrics = []` is
intentional. Enable `current_dram_power_consumption` there only if this host has
a readable DRAM RAPL subzone; otherwise it logs errors every interval.

### 3. Create a standalone Compose file

Save this as `compose.native-collectors.yaml` in the same directory. It is
standalone: do not merge it with the repository's Compose file.

```yaml
name: metrics-manager-native-collectors

services:
  metrics-manager:
    image: ${METRICS_MANAGER_IMAGE:?Set METRICS_MANAGER_IMAGE}
    init: true
    privileged: true
    pid: host
    security_opt:
      - apparmor:unconfined
    devices:
      - /dev/dri:/dev/dri
    environment:
      METRICS_MANAGER_HOSTNAME: ${METRICS_MANAGER_HOSTNAME:-lab-node-42}
      TURBOSTAT_BIN: /usr/local/bin/turbostat
      TURBOSTAT_INTERVAL: "5"
    ports:
      - "127.0.0.1:${HOST_METRICS_PORT:-9090}:9090"
      - "127.0.0.1:${HOST_TELEGRAF_PORT:-9273}:9273"
    volumes:
      - /sys:/sys:ro
      - /run:/run:ro
      - ./telegraf.docker.conf:/etc/telegraf/telegraf.conf:ro
      - ${HOST_TURBOSTAT_BIN:?Set HOST_TURBOSTAT_BIN}:/usr/local/bin/turbostat:ro
    healthcheck:
      test: ["CMD", "python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:9090/health', timeout=5)"]
      interval: 10s
      timeout: 8s
      retries: 3
      start_period: 15s
```

If the host has no GPU, omit the `/dev/dri` device mapping. If you omitted the
Turbostat input, also omit its binary mount and two environment entries;
`HOST_TURBOSTAT_BIN` is then unnecessary.

`HOST_TURBOSTAT_BIN` is the **host** path used by Compose for the mount;
`TURBOSTAT_BIN` is the **container** path expanded by Telegraf. Variables in a
Compose `.env` file are not automatically container environment variables: the
required Telegraf variables are explicitly passed under `environment`.

The API and Prometheus ports are published on loopback only. The unauthenticated
HTTP ingest listener on `8186` stays container-internal.

### 4. Check, start and verify

```bash
docker compose -f compose.native-collectors.yaml config --quiet
docker compose -f compose.native-collectors.yaml run --rm --no-deps \
  metrics-manager /usr/local/bin/turbostat --version
docker compose -f compose.native-collectors.yaml up -d --no-build
docker compose -f compose.native-collectors.yaml exec metrics-manager supervisorctl status
docker compose -f compose.native-collectors.yaml logs --tail 100 metrics-manager
```

Skip the `turbostat --version` command if you omitted that input. Choose unused
host ports with `HOST_METRICS_PORT` and `HOST_TELEGRAF_PORT` if another instance is
running. Internal ports remain `9090` and `9273`; changing host mappings does not
change Telegraf's listeners or the application's internal connections.

Allow several seconds for startup, the first counter deltas and temperature
aggregation, and at least two Turbostat intervals for refreshed samples:

```bash
curl -fsS "http://127.0.0.1:${HOST_METRICS_PORT:-9090}/health"
curl -fsS "http://127.0.0.1:${HOST_TELEGRAF_PORT:-9273}/metrics" \
  > /tmp/metrics-manager-native.prom
grep -E '^(powerstat_core_cpu_(frequency|temperature)|temp_highres_|interrupts_|net_|ethtool_|diskio_|disk_|rapl_power_|turbostat_)' \
  /tmp/metrics-manager-native.prom
```

Check each selected family, not only whether `grep` found any line. Repeat the
scrape after several seconds and compare timestamps and active counters. A
healthy API alone does not prove that Telegraf or every hardware input works.

After changing the generated configuration, restart the service. After changing
Compose environment or mounts, recreate it:

```bash
docker compose -f compose.native-collectors.yaml restart metrics-manager
# After environment or mount changes:
docker compose -f compose.native-collectors.yaml up -d --no-build --force-recreate
```

To disable an input, regenerate the file without it and restart. Remove the
RAPL block to disable that collector. Do not remove the base powerstat input
just to disable its optional per-CPU fields.

Stop only this standalone stack:

```bash
docker compose -f compose.native-collectors.yaml down
```

## Hardware and namespace limits

- **Network:** bridge-mode `net` and `ethtool` read the container's network
  namespace, typically `eth0`, not physical host NICs. Neither `pid: host` nor
  mounting `/sys` changes that. For continuous host-NIC telemetry use the
  bare-metal package, or an explicitly configured host-network container.
  Host networking requires removing `ports` and avoiding listener conflicts;
  Docker port remapping and its loopback restriction no longer apply.
  The base listeners may then be reachable off-host: apply firewall/access
  controls before starting. Do not assume a host-NIC Compose profile from
  another image revision exists in this checkout.
- **Storage:** `diskio` sees kernel device counters, but `disk` reports
  filesystems visible in the container. Host PID visibility and `/sys` do not
  mount the host's data filesystems. Restrict devices/mounts to what you intend
  to monitor; a running input is not proof of complete host filesystem coverage.
- **RAPL:** `rapl_power_power_w` reports readable top-level domains. Without
  `psys`, package power can still appear, but whole-board power cannot.
- **Turbostat:** an unusable binary or insufficient MSR access can prevent this
  service input from starting and affect Telegraf as a whole. Check the binary
  in the container and inspect logs before relying on `/health`.
- **IRQ/process cardinality:** device IRQs add per-CPU series, and process/client
  rankings create changing label sets. Expect more time series when enabled.

## Comparing with Python-reader images

Enabling equivalent hardware coverage does not guarantee identical Prometheus
names or labels:

| Python-reader name | Native-enabled image name |
|---|---|
| `dram_bw_{read,write,total}_gbps` | `memory_bandwidth_{read,write,total}_mibps` |
| `gpu_throttle_act_freq_mhz` | `gpu_frequency_act_freq` |
| `gpu_throttle_driver_freq_mhz` | `gpu_frequency_cur_freq` |
| `gpu_throttle_throttled` | `gpu_throttle_status` |
| `gpu_throttle_throttle_{pl1,pl2,prochot,thermal}` | `gpu_throttle_{pl1,pl2,prochot,thermal}` |

Native memory bandwidth is in **MiB/s**, not decimal GB/s:
`GB/s = MiB/s * 1048576 / 1000000000`. Sampling windows and data sources can also
differ. GPU detail uses native `gpu_id`/`driver`/`tile` labels rather than the
Python throttle reader's `card`/`gt`. Adapt queries deliberately instead of
assuming that identical dashboards or exact series comparisons will work.
