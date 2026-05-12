#!/usr/bin/env node
/**
 * load-test.mjs — API throughput and latency under concurrent worker load
 *
 * Measures scheduler performance as worker count scales from 1 to N:
 *   - Workflow completion time vs worker count
 *   - API endpoint latencies (register, pull, result)
 *   - Jobs-per-second throughput
 *
 * Requires the Docker stack running with WORKER_AUTH_DISABLED=1:
 *   docker compose -f docker-compose.yml -f docker-compose.e2e.yml up --build -d
 *
 * Usage:
 *   node e2e/load-test.mjs
 *   node e2e/load-test.mjs --max-workers 8 --workflow-id wf-monte-carlo-pi
 *   node e2e/load-test.mjs --workflow-id wf-synthetic-load-500 --worker-counts 500 --api-workers --sla-ms 10000
 *   node e2e/load-test.mjs --headed
 */

import { setTimeout as sleep } from "node:timers/promises";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";

function readEnvValue(name) {
  for (const path of ["../.env", ".env"]) {
    try {
      for (const line of readFileSync(path, "utf8").split(/\r?\n/)) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) continue;
        const eq = trimmed.indexOf("=");
        if (eq > 0 && trimmed.slice(0, eq).trim() === name) {
          return trimmed.slice(eq + 1).trim().replace(/^["']|["']$/g, "");
        }
      }
    } catch {}
  }
  return "";
}

const defaults = {
  apiUrl: "http://127.0.0.1:8080",
  baseUrl: "http://127.0.0.1:5173/",
  adminToken: process.env.ADMIN_API_TOKEN || readEnvValue("ADMIN_API_TOKEN"),
  workflowId: "wf-monte-carlo-pi",
  workerCounts: [1, 2, 3, 4],
  headless: true,
  pollMs: 500,
  timeoutMs: 8 * 60 * 1000,
  trialGapMs: 2000,
  staggerMs: 150,
  slaMs: 0,
  apiWorkers: false,
  syntheticDelayMs: 5000,
  syntheticPayloadBytes: 8192,
};

function parseArgs(argv) {
  const opts = { ...defaults, workerCounts: [...defaults.workerCounts] };
  for (let i = 0; i < argv.length; i++) {
    const next = () => argv[++i];
    switch (argv[i]) {
      case "--api-url": opts.apiUrl = next(); break;
      case "--base-url": opts.baseUrl = next(); break;
      case "--admin-token": opts.adminToken = next(); break;
      case "--workflow-id": opts.workflowId = next(); break;
      case "--max-workers": {
        const max = Math.max(1, Number(next()));
        opts.workerCounts = Array.from({ length: max }, (_, i) => i + 1);
        break;
      }
      case "--worker-counts": opts.workerCounts = next().split(",").map(Number).filter((n) => n > 0); break;
      case "--headed": opts.headless = false; break;
      case "--headless": opts.headless = true; break;
      case "--timeout-ms": opts.timeoutMs = Number(next()); break;
      case "--poll-ms": opts.pollMs = Number(next()); break;
      case "--trial-gap-ms": opts.trialGapMs = Number(next()); break;
      case "--stagger-ms": opts.staggerMs = Number(next()); break;
      case "--sla-ms": opts.slaMs = Number(next()); break;
      case "--api-workers": opts.apiWorkers = true; break;
      case "--synthetic-delay-ms": opts.syntheticDelayMs = Number(next()); break;
      case "--synthetic-payload-bytes": opts.syntheticPayloadBytes = Number(next()); break;
    }
  }
  if (!opts.adminToken) throw new Error("ADMIN_API_TOKEN is required (set in .env or pass --admin-token)");
  return opts;
}

function normalizeBase(raw) {
  const u = new URL(raw);
  if (!u.pathname.endsWith("/")) u.pathname += "/";
  return u.toString();
}

function apiUrl(base, path) {
  return new URL(path, base).toString();
}

async function timedFetch(url, init) {
  const t0 = Date.now();
  const r = await fetch(url, init);
  const latencyMs = Date.now() - t0;
  if (!r.ok) throw new Error(`${init?.method ?? "GET"} ${url} → ${r.status}`);
  return { data: await r.json(), latencyMs };
}

async function waitHealthy(apiBase, timeoutMs) {
  const url = apiUrl(apiBase, "/healthz");
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try { if ((await fetch(url)).ok) return; } catch {}
    await sleep(500);
  }
  throw new Error("Backend not healthy");
}

async function activateWorkflow(apiBase, workflowId, adminToken) {
  const r = await fetch(apiUrl(apiBase, "/api/admin/workflows/activate"), {
    method: "POST",
    headers: { Authorization: `Bearer ${adminToken}`, "Content-Type": "application/json" },
    body: JSON.stringify({ workflow_id: workflowId, reset_state: true }),
  });
  if (!r.ok) throw new Error(`activate failed: ${r.status} ${await r.text()}`);
  return r.json();
}

async function registerSyntheticWorker(apiBase, workerID) {
  const r = await fetch(apiUrl(apiBase, "/api/workers/register"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ worker_id: workerID }),
  });
  if (!r.ok) throw new Error(`register failed for ${workerID}: ${r.status} ${await r.text()}`);
}

async function pullSyntheticAssignment(apiBase, workerID) {
  const r = await fetch(apiUrl(apiBase, `/api/pull?worker_id=${encodeURIComponent(workerID)}`));
  if (r.status === 204) return null;
  if (!r.ok) throw new Error(`pull failed for ${workerID}: ${r.status} ${await r.text()}`);
  return r.json();
}

async function submitSyntheticResult(apiBase, workerID, assignment, payload) {
  const resultSig = createHash("sha256")
    .update(JSON.stringify({ job_id: assignment.job_id, result_payload: payload }))
    .digest("hex");
  const r = await fetch(apiUrl(apiBase, "/api/result"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      job_id: assignment.job_id,
      worker_id: workerID,
      result_sig: resultSig,
      result_payload: payload,
    }),
  });
  if (!r.ok) throw new Error(`submit failed for ${workerID}: ${r.status} ${await r.text()}`);
  return r.json();
}

async function pollUntilComplete(apiBase, pollMs, timeoutMs, onTick) {
  const url = apiUrl(apiBase, "/api/runtime");
  const deadline = Date.now() + timeoutMs;
  let lastFinalized = 0;

  while (Date.now() < deadline) {
    await sleep(pollMs);
    const { data: snap, latencyMs } = await timedFetch(url);
    const nodes = snap?.workflow?.nodes ?? [];
    const stats = snap?.stats ?? {};
    if (onTick) onTick({ latencyMs, stats });
    if (stats.finalized_jobs > lastFinalized) {
      lastFinalized = stats.finalized_jobs;
    }
    if (nodes.length > 0 && nodes.every((n) => n.completed)) {
      return { snap, finalizedJobs: lastFinalized };
    }
  }
  throw new Error("Timeout waiting for workflow completion");
}

function syntheticWorkerID(index) {
  return `0x${(index + 1).toString(16).padStart(40, "0")}`;
}

function makeDeterministicPayload(seed, bytes) {
  const alphabet = "0123456789abcdef";
  let state = 2166136261;
  for (let i = 0; i < seed.length; i += 1) {
    state ^= seed.charCodeAt(i);
    state = Math.imul(state, 16777619) >>> 0;
  }

  let out = "";
  while (out.length < bytes) {
    state ^= state << 13;
    state ^= state >>> 17;
    state ^= state << 5;
    out += alphabet[state & 15];
  }
  return out.slice(0, bytes);
}

function syntheticResultPayload(assignment, opts) {
  const payloadBytes = Math.max(128, Math.min(256_000, Math.trunc(opts.syntheticPayloadBytes)));
  return {
    output: {
      node_id: assignment.node_id,
      simulated_ms: Math.max(0, Math.trunc(opts.syntheticDelayMs)),
      payload_bytes: payloadBytes,
      payload: makeDeterministicPayload(`${assignment.workflow_id}:${assignment.node_id}`, payloadBytes),
    },
    mode: "api-synthetic-delay-payload",
  };
}

function buildWorkerUrl(baseUrl, autoWorker) {
  const u = new URL(baseUrl);
  if (autoWorker) {
    u.searchParams.set("auto_worker", "1");
  } else {
    u.searchParams.delete("auto_worker");
  }
  u.hash = "#/";
  return u.toString();
}

async function openWorkerPages(browser, baseUrl, count, staggerMs) {
  const context = await browser.newContext();
  const idleUrl = buildWorkerUrl(baseUrl, false);

  const pages = [];
  for (let i = 0; i < count; i++) {
    const page = await context.newPage();
    await page.goto(idleUrl, { waitUntil: "domcontentloaded", timeout: 30_000 });
    pages.push(page);
    if (staggerMs > 0 && i < count - 1) await sleep(staggerMs);
  }
  return { context, pages };
}

async function startWorkerPages(pages, baseUrl) {
  const workerUrl = buildWorkerUrl(baseUrl, true);
  await Promise.all(
    pages.map((page) => page.goto(workerUrl, { waitUntil: "domcontentloaded", timeout: 60_000 })),
  );
}

class Metrics {
  constructor() {
    this.runtimeLatencies = [];
  }

  record(latencyMs) {
    this.runtimeLatencies.push(latencyMs);
  }

  summary() {
    if (this.runtimeLatencies.length === 0) return { p50: 0, p95: 0, p99: 0, count: 0 };
    const sorted = [...this.runtimeLatencies].sort((a, b) => a - b);
    const pct = (p) => sorted[Math.min(Math.floor(sorted.length * p / 100), sorted.length - 1)];
    return {
      count: sorted.length,
      p50: pct(50),
      p95: pct(95),
      p99: pct(99),
      avg: Math.round(sorted.reduce((s, x) => s + x, 0) / sorted.length),
    };
  }
}

async function runLoadTrial(opts, workerCount, browser) {
  if (opts.apiWorkers) {
    return runAPISyntheticTrial(opts, workerCount);
  }

  console.log(`\n  Opening ${workerCount} idle worker page(s)...`);
  const { context, pages } = await openWorkerPages(browser, opts.baseUrl, workerCount, opts.staggerMs);

  const metrics = new Metrics();
  let firstFinalizedMs = 0;
  let peakQueued = 0;
  let peakWorkersOnline = 0;

  console.log(`  Activating + resetting workflow...`);
  await activateWorkflow(opts.apiUrl, opts.workflowId, opts.adminToken);
  await sleep(600);

  console.log(`  Starting ${workerCount} worker page(s)...`);
  const startMs = Date.now();
  await startWorkerPages(pages, opts.baseUrl);

  const { finalizedJobs } = await pollUntilComplete(
    opts.apiUrl,
    opts.pollMs,
    opts.timeoutMs,
    ({ latencyMs, stats }) => {
      metrics.record(latencyMs);
      const finalized = Number(stats.finalized_jobs ?? 0);
      const queued = Number(stats.queued_jobs ?? 0);
      const online = Number(stats.workers_online ?? 0);
      if (finalized > 0 && firstFinalizedMs === 0) {
        firstFinalizedMs = Date.now() - startMs;
      }
      if (Number.isFinite(queued)) {
        peakQueued = Math.max(peakQueued, queued);
      }
      if (Number.isFinite(online)) {
        peakWorkersOnline = Math.max(peakWorkersOnline, online);
      }
    },
  );

  const elapsedMs = Date.now() - startMs;
  const lat = metrics.summary();
  const throughput = elapsedMs > 0 ? (finalizedJobs / (elapsedMs / 1000)).toFixed(2) : "0";

  console.log([
    `  ✓ ${workerCount} worker(s)`,
    `wall=${(elapsedMs / 1000).toFixed(2)}s`,
    `jobs=${finalizedJobs}`,
    `throughput=${throughput}j/s`,
    `first_finalized=${firstFinalizedMs > 0 ? `${(firstFinalizedMs / 1000).toFixed(2)}s` : "-"}`,
    `peak_queued=${peakQueued}`,
    `peak_online=${peakWorkersOnline}`,
    `api_p50=${lat.p50}ms`,
    `api_p95=${lat.p95}ms`,
  ].join("  "));

  await Promise.allSettled(pages.map((p) => p.close()));
  await context.close();

  return { workerCount, elapsedMs, finalizedJobs, throughput: Number(throughput), firstFinalizedMs, peakQueued, peakWorkersOnline, lat };
}

async function runAPISyntheticWorker(opts, workerIndex) {
  const workerID = syntheticWorkerID(workerIndex);
  await registerSyntheticWorker(opts.apiUrl, workerID);
  const assignment = await pullSyntheticAssignment(opts.apiUrl, workerID);
  if (!assignment) {
    return { workerID, assigned: false };
  }
  await sleep(Math.max(0, Math.trunc(opts.syntheticDelayMs)));
  const payload = syntheticResultPayload(assignment, opts);
  await submitSyntheticResult(opts.apiUrl, workerID, assignment, payload);
  return { workerID, assigned: true, jobID: assignment.job_id };
}

async function runAPISyntheticTrial(opts, workerCount) {
  console.log(`\n  Activating + resetting workflow...`);
  await activateWorkflow(opts.apiUrl, opts.workflowId, opts.adminToken);
  await sleep(600);

  const metrics = new Metrics();
  let firstFinalizedMs = 0;
  let peakQueued = 0;
  let peakWorkersOnline = 0;

  console.log(
    `  Starting ${workerCount} API synthetic worker(s), delay=${opts.syntheticDelayMs}ms, payload=${opts.syntheticPayloadBytes}B...`,
  );
  const startMs = Date.now();
  const pollPromise = pollUntilComplete(
    opts.apiUrl,
    opts.pollMs,
    opts.timeoutMs,
    ({ latencyMs, stats }) => {
      metrics.record(latencyMs);
      const finalized = Number(stats.finalized_jobs ?? 0);
      const queued = Number(stats.queued_jobs ?? 0);
      const online = Number(stats.workers_online ?? 0);
      if (finalized > 0 && firstFinalizedMs === 0) {
        firstFinalizedMs = Date.now() - startMs;
      }
      if (Number.isFinite(queued)) {
        peakQueued = Math.max(peakQueued, queued);
      }
      if (Number.isFinite(online)) {
        peakWorkersOnline = Math.max(peakWorkersOnline, online);
      }
    },
  );
  const workerPromise = Promise.all(
    Array.from({ length: workerCount }, (_, index) => runAPISyntheticWorker(opts, index)),
  );

  const [{ finalizedJobs }, workerResults] = await Promise.all([pollPromise, workerPromise]);
  const assignedCount = workerResults.filter((item) => item.assigned).length;
  const elapsedMs = Date.now() - startMs;
  const lat = metrics.summary();
  const throughput = elapsedMs > 0 ? (finalizedJobs / (elapsedMs / 1000)).toFixed(2) : "0";

  console.log([
    `  ✓ ${workerCount} API worker(s)`,
    `wall=${(elapsedMs / 1000).toFixed(2)}s`,
    `jobs=${finalizedJobs}`,
    `assigned=${assignedCount}`,
    `throughput=${throughput}j/s`,
    `first_finalized=${firstFinalizedMs > 0 ? `${(firstFinalizedMs / 1000).toFixed(2)}s` : "-"}`,
    `peak_queued=${peakQueued}`,
    `peak_online=${peakWorkersOnline}`,
    `api_p50=${lat.p50}ms`,
    `api_p95=${lat.p95}ms`,
  ].join("  "));

  return { workerCount, elapsedMs, finalizedJobs, throughput: Number(throughput), firstFinalizedMs, peakQueued, peakWorkersOnline, lat };
}

function printTable(rows) {
  const cols = [
    { label: "Workers",    key: "workerCount",  w: 9 },
    { label: "Time (s)",   key: (r) => (r.elapsedMs / 1000).toFixed(2), w: 10 },
    { label: "Jobs",       key: "finalizedJobs",w: 6 },
    { label: "Throughput", key: (r) => `${r.throughput}j/s`, w: 12 },
    { label: "First",      key: (r) => r.firstFinalizedMs > 0 ? `${(r.firstFinalizedMs / 1000).toFixed(2)}s` : "-", w: 9 },
    { label: "Peak Q",     key: "peakQueued", w: 8 },
    { label: "Peak On",    key: "peakWorkersOnline", w: 8 },
    { label: "API p50",    key: (r) => `${r.lat.p50}ms`, w: 9 },
    { label: "API p95",    key: (r) => `${r.lat.p95}ms`, w: 9 },
    { label: "Speedup",    key: (r, i) => i === 0 ? "1.00x" : `${(rows[0].elapsedMs / r.elapsedMs).toFixed(2)}x`, w: 9 },
  ];

  const header = cols.map((c) => c.label.padEnd(c.w)).join("  ");
  const divider = cols.map((c) => "-".repeat(c.w)).join("  ");
  console.log("\n" + header);
  console.log(divider);
  rows.forEach((row, i) => {
    const line = cols.map((c) => {
      const val = typeof c.key === "function" ? c.key(row, i) : String(row[c.key]);
      return val.padEnd(c.w);
    }).join("  ");
    console.log(line);
  });
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  opts.apiUrl = normalizeBase(opts.apiUrl).replace(/\/$/, "");
  opts.baseUrl = normalizeBase(opts.baseUrl);

  console.log("=".repeat(66));
  console.log("  X402 Scheduler — Load Test");
  console.log("=".repeat(66));
  console.log(`  Workflow   : ${opts.workflowId}`);
  console.log(`  API        : ${opts.apiUrl}`);
  console.log(`  Frontend   : ${opts.baseUrl}`);
  console.log(`  Worker run : ${opts.workerCounts.join(", ")}`);
  console.log(`  Mode       : ${opts.apiWorkers ? "api synthetic workers" : opts.headless ? "headless" : "headed"}`);
  if (opts.slaMs > 0) {
    console.log(`  SLA        : ${(opts.slaMs / 1000).toFixed(2)}s`);
  }
  console.log();

  await waitHealthy(opts.apiUrl, 30_000);
  console.log("Backend healthy.\n");

  let browser = null;
  if (!opts.apiWorkers) {
    const { chromium } = await import("playwright");
    browser = await chromium.launch({ headless: opts.headless });
  }

  const results = [];
  try {
    for (let i = 0; i < opts.workerCounts.length; i++) {
      const count = opts.workerCounts[i];
      console.log(`[Trial ${i + 1}/${opts.workerCounts.length}] ${count} worker(s)`);
      const result = await runLoadTrial(opts, count, browser);
      results.push(result);
      if (opts.slaMs > 0 && result.elapsedMs > opts.slaMs) {
        console.log(
          `  SLA missed: ${(result.elapsedMs / 1000).toFixed(2)}s > ${(opts.slaMs / 1000).toFixed(2)}s`,
        );
      }
      if (i < opts.workerCounts.length - 1) await sleep(opts.trialGapMs);
    }
  } finally {
    await browser?.close();
  }

  console.log("\n" + "=".repeat(66));
  console.log("  Load Test Results");
  console.log("=".repeat(66));
  printTable(results);
  console.log();

  if (results.length >= 2) {
    const base = results[0];
    const best = results.reduce((a, b) => (a.elapsedMs < b.elapsedMs ? a : b));
    console.log(`  Best result   : ${best.workerCount} workers in ${(best.elapsedMs / 1000).toFixed(2)}s`);
    console.log(`  Max speedup   : ${(base.elapsedMs / best.elapsedMs).toFixed(2)}x vs single worker`);
  }
  if (opts.slaMs > 0 && results.some((r) => r.elapsedMs > opts.slaMs)) {
    process.exitCode = 1;
  }
  console.log("=".repeat(66));
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exitCode = 1;
});
