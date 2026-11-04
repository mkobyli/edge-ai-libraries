import assert from "node:assert/strict";
import test from "node:test";
import reducer, {
  messageReceived,
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
      metrics: reducer(undefined, message({ npu_frequency: frequencyHz })),
    };
    assert.equal(selectNpuMetrics(state).frequency, frequencyMHz);
  }
});

test("NPU snapshots distinguish measured zero from missing fields", () => {
  let state = {
    metrics: reducer(
      undefined,
      message({ npu_utilization: 50, npu_frequency: 1_600_000_000 }),
    ),
  };
  assert.equal(selectNpuMetric(state), 50);
  state = {
    metrics: reducer(
      state.metrics,
      message({ npu_utilization: 0, npu_frequency: 0 }),
    ),
  };
  assert.equal(selectNpuMetric(state), 0);
  assert.equal(selectNpuMetrics(state).frequency, 0);
  state = { metrics: reducer(state.metrics, message({ npu_power: 2 })) };
  assert.equal(selectNpuMetric(state), undefined);
  assert.equal(selectNpuMetrics(state).utilization, undefined);
  assert.equal(selectNpuMetrics(state).frequency, undefined);
  assert.equal(selectNpuMetrics(state).power, 2);
});
