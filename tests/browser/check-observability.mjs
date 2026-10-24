import assert from "node:assert/strict";

// Catch a running Grafana whose Prometheus plugin or datasource cannot serve
// dashboard queries. A 200 health endpoint alone does not prove this works.
const base = process.env.GRAFANA_URL || "http://127.0.0.1:13000";
async function json(path, body) {
  const response = await fetch(base + path, {
    method: body ? "POST" : "GET",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(15000),
  });
  const result = await response.json();
  assert(
    response.ok,
    `${path}: HTTP ${response.status}: ${JSON.stringify(result)}`,
  );
  return result;
}

const { dashboard } = await json("/api/dashboards/uid/marketplace");
assert(dashboard.panels.length > 0, "Dashboard has no panels");
for (const panel of dashboard.panels) {
  const result = await json("/api/ds/query", {
    from: String(Date.now() - 5 * 60 * 1000),
    to: String(Date.now()),
    queries: panel.targets.map((target) => ({
      ...target,
      datasource: target.datasource || panel.datasource,
      range: true,
      intervalMs: 15000,
      maxDataPoints: 300,
    })),
  });
  for (const target of panel.targets) {
    const data = result.results?.[target.refId];
    assert(
      data && !data.error && data.status === 200,
      `${panel.title}: ${JSON.stringify(data)}`,
    );
  }
  console.log(`${panel.title}: query OK`);
}

// This gauge exists even when nobody is using the marketplace and the queue
// is empty. Verify actual numeric data, rather than accepting empty results.
const probe = await json("/api/ds/query", {
  from: String(Date.now() - 5 * 60 * 1000),
  to: String(Date.now()),
  queries: [
    {
      refId: "A",
      datasource: { type: "prometheus", uid: "prometheus" },
      expr: "marketplace_outbox_pending",
      range: true,
      intervalMs: 15000,
      maxDataPoints: 300,
    },
  ],
});
assert(
  probe.results?.A?.frames?.some((frame) =>
    frame.schema.fields.some(
      (field, index) =>
        field.type === "number" &&
        frame.data.values[index].some(
          (value) => typeof value === "number" && Number.isFinite(value),
        ),
    ),
  ),
  "No numeric outbox samples returned through Grafana; wait for a Prometheus scrape",
);
console.log("Grafana returns real Prometheus samples");
