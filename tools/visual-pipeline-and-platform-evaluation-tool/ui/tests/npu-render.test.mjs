import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { after, before, test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Provider } from "react-redux";
import { configureStore } from "@reduxjs/toolkit";
import { createServer } from "vite";
import metricsReducer, {
  messageReceived,
} from "../src/store/reducers/metrics.ts";

let server;
let useMetrics;
let createMetricHistoryPoint;
let MetricsDashboard;
let NpuUsageProgress;
let NpuUsageChart;

before(async () => {
  server = await createServer({
    server: { middlewareMode: true },
    appType: "custom",
  });
  ({ useMetrics } = await server.ssrLoadModule(
    "/src/features/metrics/useMetrics.ts",
  ));
  ({ createMetricHistoryPoint } = await server.ssrLoadModule(
    "/src/hooks/useMetricHistory.ts",
  ));
  ({ MetricsDashboard } = await server.ssrLoadModule(
    "/src/features/metrics/MetricsDashboard.tsx",
  ));
  ({ NpuUsageProgress } = await server.ssrLoadModule(
    "/src/features/metrics/NpuUsageProgress.tsx",
  ));
  ({ NpuUsageChart } = await server.ssrLoadModule(
    "/src/features/metrics/charts/NpuUsageChart.tsx",
  ));
});

after(async () => {
  await server?.close();
});

test("API proxy identifies SSE from the request URL without response.req", () => {
  const proxy = new EventEmitter();
  server.config.server.proxy["/api"].configure(proxy);
  for (const url of ["/api/v1/jobs/test/stream", "/api/v1/health", undefined]) {
    const response = { headers: { "content-type": "text/event-stream" } };
    proxy.emit("proxyRes", response, { url });
    assert.deepEqual(response.headers, {
      "content-type": "text/event-stream",
      ...(url?.includes("/stream")
        ? { "cache-control": "no-cache", "x-accel-buffering": "no" }
        : {}),
    });
  }
});

test("NPU hooks, history and indicators preserve zero and missing measurements", () => {
  const store = configureStore({
    reducer: {
      metrics: metricsReducer,
      devices: () => ({
        items: [
          {
            device_family: "NPU",
            device_name: "NPU",
            full_device_name: "Test NPU",
          },
        ],
      }),
    },
  });
  const history = [];
  let latest;
  function CaptureMetrics() {
    latest = useMetrics();
    return null;
  }
  const render = (component, props = {}) =>
    renderToStaticMarkup(
      createElement(
        Provider,
        { store, stabilityCheck: "never" },
        createElement(component, props),
      ),
    );
  for (const utilization of [50, 0, undefined, 25]) {
    const metrics = [
      { name: "npu_frequency_hz", value: 1_600_000_000, labels: {}, timestamp: 1 },
    ];
    if (utilization !== undefined)
      metrics.push({
        name: "npu_utilization",
        value: utilization,
        labels: {},
        timestamp: 1,
      });
    store.dispatch(messageReceived(JSON.stringify({ timestamp: 1, metrics })));
    render(CaptureMetrics);
    assert.equal(latest.npu, utilization);
    history.push(createMetricHistoryPoint(latest, (history.length + 1) * 1000));
    assert.equal(history.at(-1).npuUsage, utilization);
    assert.equal(history.at(-1).npuFrequency, 1600);
    const progress = render(NpuUsageProgress);
    const dashboard = render(MetricsDashboard, { historyOverride: history });
    if (utilization === undefined) {
      assert.match(progress, /N\/A/);
      assert.doesNotMatch(progress, /data-slot="progress-indicator"/);
      assert.match(dashboard, /NPU Usage<\/h3><p[^>]*>N\/A<\/p>/);
    } else {
      assert.match(progress, new RegExp(`${utilization.toFixed(2)}%`));
      assert.match(progress, /data-slot="progress-indicator"/);
      assert.match(
        dashboard,
        new RegExp(`NPU Usage</h3><p[^>]*>${utilization.toFixed(2)}`),
      );
    }
  }
  const data = history.map((point) => ({
    timestamp: point.timestamp,
    usage: point.npuUsage,
  }));
  assert.deepEqual(
    NpuUsageChart({ data }).props.data.map((point) => point.usage),
    [50, 0, undefined, 25],
  );
});
