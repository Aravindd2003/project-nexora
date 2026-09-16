/**
 * Dummy Customer Server ("Acme Support" simulator).
 *
 * Plays the role of a Nexora customer application: holds an API key,
 * submits operations, and exposes a local webhook endpoint that Nexora
 * delivers events to. The /demo/* routes trigger the exact scenarios the
 * assessment's evaluation guidelines ask reviewers to exercise (normal
 * flow, burst/rate-limiting, idempotency, webhook failure+recovery,
 * quota exhaustion), so a reviewer can hit one endpoint per scenario
 * instead of scripting curl calls by hand.
 */
import express from "express";
import fetch from "node-fetch";
import { v4 as uuidv4 } from "uuid";

const PORT = process.env.PORT || 4000;
const GATEWAY_URL = process.env.NEXORA_GATEWAY_URL || "http://gateway:8080";
const SELF_URL = process.env.SELF_URL || `http://dummy-customer:${PORT}`;

const app = express();
app.use(express.json());

// ---- in-memory "customer application" state ----
let state = {
  apiKey: process.env.NEXORA_API_KEY || null,
  webhookMode: "ok", // "ok" | "fail" — toggled by /demo/webhook/toggle
  receivedWebhooks: [],
  log: [],
};

function log(entry) {
  const line = { ts: new Date().toISOString(), ...entry };
  state.log.unshift(line);
  state.log = state.log.slice(0, 200);
  console.log(JSON.stringify(line));
}

function authHeaders() {
  if (!state.apiKey) throw new Error("no API key configured — call POST /configure first");
  return { Authorization: `Bearer ${state.apiKey}`, "Content-Type": "application/json" };
}

// ---- setup ----
app.post("/configure", (req, res) => {
  const { api_key } = req.body;
  if (!api_key) return res.status(400).json({ error: "api_key is required" });
  state.apiKey = api_key;
  log({ event: "configured", note: "api key set" });
  res.json({ ok: true });
});

app.get("/status", (_req, res) => {
  res.json({
    configured: !!state.apiKey,
    webhook_mode: state.webhookMode,
    received_webhooks: state.receivedWebhooks.length,
    recent_log: state.log.slice(0, 20),
  });
});

// ---- the webhook endpoint Nexora delivers events to ----
app.post("/webhooks/nexora", (req, res) => {
  if (state.webhookMode === "fail") {
    log({ event: "webhook_received_but_rejected", body: req.body });
    return res.status(503).json({ error: "simulated endpoint outage" });
  }
  state.receivedWebhooks.unshift({ receivedAt: new Date().toISOString(), ...req.body });
  log({ event: "webhook_delivered", event_type: req.body.event_type, operation_id: req.body.operation_id });
  res.status(200).json({ ok: true });
});

// ---- Scenario 2: normal API usage ----
app.post("/demo/normal", async (_req, res) => {
  try {
    const results = [];
    for (let i = 0; i < 5; i++) {
      const r = await fetch(`${GATEWAY_URL}/v1/operations`, {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify({ op_type: "classify", input: { text: `Sample support ticket #${i + 1}: my invoice looks wrong.` } }),
      });
      const body = await r.json();
      results.push({ status: r.status, body });
    }
    log({ event: "demo_normal_submitted", count: results.length });
    res.json({ submitted: results.length, results });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ---- Scenario 3: rate limiting (burst) ----
app.post("/demo/burst", async (_req, res) => {
  try {
    const promises = Array.from({ length: 100 }).map(() =>
      fetch(`${GATEWAY_URL}/v1/operations`, {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify({ op_type: "classify", input: { text: "burst test" } }),
      }).then((r) => r.status)
    );
    const statuses = await Promise.all(promises);
    const summary = statuses.reduce((acc, s) => ({ ...acc, [s]: (acc[s] || 0) + 1 }), {});
    log({ event: "demo_burst_completed", summary });
    res.json({ fired: statuses.length, status_summary: summary });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ---- Scenario 8: duplicate request (idempotency) ----
app.post("/demo/idempotent", async (_req, res) => {
  try {
    const idempotencyKey = uuidv4();
    const payload = { op_type: "summarize", input: { text: "Idempotency demo payload." } };

    const first = await fetch(`${GATEWAY_URL}/v1/operations`, {
      method: "POST",
      headers: { ...authHeaders(), "Idempotency-Key": idempotencyKey },
      body: JSON.stringify(payload),
    });
    const firstBody = await first.json();

    const second = await fetch(`${GATEWAY_URL}/v1/operations`, {
      method: "POST",
      headers: { ...authHeaders(), "Idempotency-Key": idempotencyKey },
      body: JSON.stringify(payload),
    });
    const secondBody = await second.json();

    const sameOperation = firstBody.id && firstBody.id === secondBody.id;
    log({ event: "demo_idempotent_completed", sameOperation, operationId: firstBody.id });
    res.json({
      idempotency_key: idempotencyKey,
      first: { status: first.status, body: firstBody },
      second: { status: second.status, body: secondBody },
      same_operation_confirmed: sameOperation,
    });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ---- Scenario 7: webhook failure + recovery ----
app.post("/demo/webhook/toggle", (req, res) => {
  const { status } = req.body; // "ok" | "fail"
  state.webhookMode = status === "fail" ? "fail" : "ok";
  log({ event: "webhook_mode_toggled", mode: state.webhookMode });
  res.json({ webhook_mode: state.webhookMode });
});

// ---- quota exhaustion demo ----
app.post("/demo/quota-exhaust", async (_req, res) => {
  try {
    const results = [];
    for (let i = 0; i < 50; i++) {
      const r = await fetch(`${GATEWAY_URL}/v1/operations`, {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify({ op_type: "classify", input: { text: `quota burn ${i}` } }),
      });
      results.push(r.status);
      if (r.status === 429) break;
    }
    log({ event: "demo_quota_exhaust_completed", attempts: results.length });
    res.json({ attempts: results.length, final_status: results[results.length - 1] });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

app.listen(PORT, () => {
  console.log(`dummy-customer: listening on :${PORT} (self url: ${SELF_URL})`);
});
