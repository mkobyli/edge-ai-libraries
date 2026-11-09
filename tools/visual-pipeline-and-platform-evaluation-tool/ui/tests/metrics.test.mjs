import assert from "node:assert/strict";
import test from "node:test";
import reducer, {
  messageReceived,
  selectCpuMetrics,
  selectGpuMetrics,
  selectNpuMetric,
  selectNpuMetrics,
} from "../src/store/reducers/metrics.ts";

const message = (values) =>
  messageReceived(
    JSON.stringify({
      timestamp: 1,
      metrics: Object.entries(values).map(([name, value]) => ({
        name,
        value,
        timestamp: 1,
        labels: { host: "test" },
      })),
    }),
  );

test("NPU frequency is always converted from Hz to MHz", () => {
  for (const [frequencyHz, frequencyMHz] of [
    [1_600_000_000, 1600],
    [1_000_000, 1],
    [1600, 0.0016],
    [0, 0],
  ]) {
    const state = {
      metrics: reducer(undefined, message({ npu_frequency_hz: frequencyHz })),
    };
    assert.equal(selectNpuMetrics(state).frequency, frequencyMHz);
  }
});

test("NPU snapshots distinguish measured zero from missing fields", () => {
  let state = {
    metrics: reducer(
      undefined,
      message({ npu_utilization: 50, npu_frequency_hz: 1_600_000_000 }),
    ),
  };
  assert.equal(selectNpuMetric(state), 50);
  state = {
    metrics: reducer(
      state.metrics,
      message({ npu_utilization: 0, npu_frequency_hz: 0 }),
    ),
  };
  assert.equal(selectNpuMetric(state), 0);
  assert.equal(selectNpuMetrics(state).frequency, 0);
  state = { metrics: reducer(state.metrics, message({ npu_power_w: 2 })) };
  assert.equal(selectNpuMetric(state), undefined);
  assert.equal(selectNpuMetrics(state).utilization, undefined);
  assert.equal(selectNpuMetrics(state).frequency, undefined);
  assert.equal(selectNpuMetrics(state).power, 2);
});

test("native CPU and GPU base units preserve GHz and watt display values", () => {
  for (const scale of [0, 1]) {
    const state = {
      metrics: {
        metrics: [
          { name: "cpu_frequency_avg_frequency_hz", value: 2.4e9 * scale, labels: {} },
          { name: "gpu_frequency_cur_freq_hz", value: 800e6 * scale, labels: { gpu_id: "0", type: "cur_freq" } },
          { name: "gpu_power_power_w", value: 2.5 * scale, labels: { gpu_id: "0", type: "gpu_cur_power" } },
          { name: "gpu_power_power_w", value: 10.5 * scale, labels: { gpu_id: "0", type: "pkg_cur_power" } },
          { name: "gpu_frequency_cur_freq_hz", value: 1.2e9 * scale, labels: { gpu_id: "1", type: "cur_freq" } },
          { name: "npu_temperature_c", value: 40 * scale, labels: {} },
        ],
      },
    };
    assert.equal(selectCpuMetrics(state).avgFrequency, 2.4 * scale);
    assert.equal(selectGpuMetrics(state).frequency, 0.8 * scale);
    assert.equal(selectGpuMetrics(state, "1").frequency, 1.2 * scale);
    assert.equal(selectGpuMetrics(state).gpuPower, 2.5 * scale);
    assert.equal(selectGpuMetrics(state).pkgPower, 10.5 * scale);
    assert.equal(selectNpuMetrics(state).temperature, 40 * scale);
  }
  const state = { metrics: { metrics: [] } };
  assert.equal(selectGpuMetrics(state).frequency, undefined);
  assert.equal(selectGpuMetrics(state).gpuPower, undefined);
  assert.equal(selectNpuMetrics(state).power, undefined);
  assert.equal(selectNpuMetrics(state).temperature, undefined);
});
