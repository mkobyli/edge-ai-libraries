# NPU System Monitoring Tool (npu-monitor-tool)

## Overview

The NPU System Monitoring Tool (`npu-monitor-tool`) is a comprehensive command-line utility
for monitoring Neural Processing Unit (NPU) performance metrics in real-time. This tool provides
detailed insights into Intel NPU operations, enabling developers and system administrators
to track and analyze NPU performance.

### Key Features

- **Real-time Performance Monitoring**: Continuously monitor NPU metrics at customizable intervals
- **Power Consumption Tracking**: Monitor power usage in watts
- **Thermal Management**: Track NPU temperature in Celsius
- **Utilization Metrics**: View processing unit utilization percentage
- **Memory Monitoring**: Track available memory usage in MB/GB
- **Frequency Information**: Display the DPU clock in MHz
- **Bandwidth Analysis**: Monitor NPU DDR average bandwidth in MB/s or GB/s
- **Tile Configuration**: View current tile configuration
- **CSV Export**: Export metrics to CSV format for data analysis and visualization

Disclaimer: The utilization percentage is a calculated metric, based on the difference in NPU busy timestamps when the inference starts and ends on NPU. It does not represent an exact hardware-level measurement. Hence, this metric is an approximation of workload and not a precise hardware utilization figure.

### Prometheus/Grafana Dashboard

[requirements-exporter.txt](./requirements-exporter.txt) is Python dependencies for the Prometheus exporter,
which is not required for running npu-monitor-tool.py. Please read [docs/npu-metrics-exporter](docs/npu-metrics-exporter.md) for more details.

### Supported Platforms

The tool supports the following Intel CPU generations with integrated NPU:
- **Meteor Lake (MTL)**
- **Arrow Lake (ARL)**
- **Lunar Lake (LNL)**
- **Panther Lake (PTL)**
- **Wildcat Lake (WCL)**

### Requirements

- Linux operating system
- Intel NPU with `intel_vpu` driver loaded
- Python 3.10 or later
- Root or appropriate permissions to access sysfs interfaces
- PMT (Platform Monitoring Technology) support enabled. (spec: https://www.intel.com/content/www/us/en/content-details/710389/intel-platform-monitoring-technology-intel-pmt-technical-specification.html)

## How to Use

### Basic Usage

Run the tool once to get a snapshot of current NPU metrics:

```bash
sudo ./npu-monitor-tool.py
or
sudo python3 npu-monitor-tool.py
```

### Continuous Monitoring

Monitor NPU metrics continuously with a specified interval (in milliseconds):

```bash
sudo ./npu-monitor-tool.py -i 1000
or
sudo python3 npu-monitor-tool.py -i 1000
```

This will update the display every 1000ms (1 second).

### Verbose Output

Enable verbose output for debugging purposes:

```bash
sudo ./npu-monitor-tool.py -v
or
sudo python3 npu-monitor-tool.py -v
```

### CSV Export

Export metrics to a CSV file for analysis:

```bash
sudo ./npu-monitor-tool.py --csv -i 1000
or
sudo python3 npu-monitor-tool.py --csv -i 1000
```

The CSV file will contain the following columns:
- timestamp
- power (W)
- frequency (Hz)
- bandwidth (always MB/s, regardless of terminal display scaling)
- tile_config
- temperature (°C)
- utilization (%)
- memory_usage (MB)
- device (PCI device ID)

Unavailable readings are empty CSV cells and `N/A` on screen, not `0` or `-1`.
Measured zero remains zero. Memory availability is detected through sysfs, without
a CPU-generation restriction. Memory-traffic counters are unavailable on platforms
that do not expose the corresponding PMT registers.

### Units and Sampling

The PLL reference clock is 50 MHz. MTL/ARL use `ratio * 50 MHz * 2 / 3`;
LNL/PTL/WCL use `ratio * 50 MHz / 2`. For example, ARL ratio 48 is
1,600,000,000 Hz, displayed as 1600 MHz. No display-specific scaling is applied.

Bandwidth is the change in the scaled PMT memory-traffic counter divided by the
actual monotonic elapsed time, not the requested sleep interval. Each register's
32-bit wrap range is `2^32 / 1000` MB. A 20 MB increase over 2 seconds is 10 MB/s,
including across wraparound. Utilization and power also use actual elapsed time.
Missing counter readings or busy-time/energy resets require a new baseline before
a rate is available. Non-positive elapsed time omits derived readings and keeps
the previous baseline. The CLI interval must be finite and positive.

CSV column names remain unchanged, but `frequency` now contains real Hz and
`bandwidth` contains real MB/s rather than the counter delta. Historical values
written by the older scaling are not directly comparable. Update the exporter and
Grafana dashboard together, and compare measurements collected after the update.

The CLI uses only the Python standard library. The standalone tool and its
exporter are independent of metrics-manager's native Go plugin; neither becomes
a dependency of that plugin.

### Command-Line Options

| Option | Description |
|--------|-------------|
| `-i, --interval <msec>` | Probing interval in milliseconds for continuous monitoring |
| `-v, --verbose` | Enable verbose output with debug information |
| `--csv` | Output data in CSV format into the npu_output folder with timestamped filename. |
| `-h, --help` | Display help message and exit |

### Example Output

```
+-----------------------------------------------------------------------------------------------+
| INTEL NPU Device: 0x7d1d | version:                                          1.0.0 |
| Firmware version: IVPU_MTL_20240112_v2024.01                                                 |
|                                                                                               |
+===============================================================================================+
|       Power Usage        |      DPU Freq        | NPU DDR Average Bandwidth |    Tile Conf    |
|                2.5 [W]   |       1600 [MHz]     |               123.45 [MB/s] |               4 |
+===============================================================================================+
|       NPU Temperature    |       NPU Utilization       |      Memory Usage                    |
|              45 [°C]     |                       25%   |                         512.00 [MB] |
+-----------------------------------------------------------------------------------------------+
```

### Troubleshooting

**Error: "Intel NPU driver 'intel_vpu' seems not to be loaded"**
- Ensure the Intel NPU driver is loaded: `lsmod | grep intel_vpu`
- Load the driver if needed: `sudo modprobe intel_vpu`

**Error: "PMT sysfs interface not found"**
- Verify PMT support is enabled in your kernel
- Check if `/sys/class/intel_pmt` exists

**Error: "No CPU telemetry devices found with known GUIDs"**
- Your CPU generation may not be supported
- Ensure you're running on a supported Intel platform with NPU

### Permissions

The tool requires root privileges or appropriate permissions to access:
- `/sys/class/intel_pmt/` - PMT telemetry interface
- `/sys/bus/pci/drivers/intel_vpu/` - NPU driver interface
- `/sys/kernel/debug/accel/` - NPU debug interface

### Offline Regression Tests

No NPU, root access, or running Prometheus/Grafana instance is needed:

```bash
python3 -m pip install pytest -r requirements-exporter.txt
python3 -m pytest -q tests/
```

Tests cover PLL ratios, counter wrap, real-time CSV rates, missing versus zero
readings, counter recovery, actual Prometheus exposition and the Flask endpoint,
and the dashboard's units and missing-data settings. Hardware validation is still
needed to confirm readings on a particular platform.
