#!/usr/bin/env node
/**
 * benchmark.mjs — Performance measurement for poster/report
 *
 * Runs 3 scenarios against the monte-carlo-pi workflow:
 *
 *   1. Sequential  — 1 worker handles all 3 simulation shards one by one
 *   2. Parallel    — 3 workers run all 3 shards simultaneously
 *   3. Fault-tolerant — 3 workers start; one crashes mid-job; system
 *                       reassigns after TTL expiry; completes anyway
 *
 * Produces a table ready to paste into a poster or report.
 *
 * Requirements:
 *   docker compose -f docker-compose.yml -f docker-compose.benchmark.yml up --build -d
 *   WORKER_AUTH_DISABLED=1 must be set (docker-compose.benchmark.yml does this)
 *   ASSIGNMENT_TTL_SECONDS should be short for the fault scenario (15 recommended)
 *
 * Usage:
 *   node e2e/benchmark.mjs
 *   node e2e/benchmark.mjs --headed
 *   node e2e/benchmark.mjs --skip-fault   # skip the fault tolerance scenario
 */

import { setTimeout as sleep } from "node:timers/promises";
import { readFileSync } from "node:fs";

// ── helpers ──────────────────────────────────────────────────────────────────

function readEnvValue(name) {
  for (const path of ["../.env", ".env"]) {
    try {
      for (const line of readFileSync(path, "utf8").split(/\r?\n/)) {
        const t = line.trim();
        if (!t || t.startsWith("#")) continue;
        const eq = t.indexOf("=");
        if (eq > 0 && t.slice(0, eq).trim() === name)
          return t.slice(eq + 1).trim().replace(/^["']|["']$/g, "");
      }
    } catch {}
  }
  return "";
}

function normalizeBase(raw) {
  const u = new URL(raw);
  if (!u.pathname.endsWith("/")) u.pathname += "/";
  return u.toString();
}

function apiUrl(base, path) { return new URL(path, base).toString(); }

async function fetchOk(url, init) {
  const r = await fetch(url, init);
  if (!r.ok) throw new Error(`${init?.method ?? "GET"} ${url} → ${r.status} ${await r.text()}`);
  return r.json();
}

async function waitHealthy(apiBase, timeoutMs = 30_000) {
  const url = apiUrl(apiBase, "/healthz");
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try { if ((await fetch(url)).ok) return; } catch {}
    await sleep(400);
  }
  throw new Error("Backend not healthy within timeout");
}

async function activateWorkflow(apiBase, workflowId, adminToken) {
  return fetchOk(apiUrl(apiBase, "/api/admin/workflows/activate"), {
    method: "POST",
    headers: { Authorization: `Bearer ${adminToken}`, "Content-Type": "application/json" },
    body: JSON.stringify({ workflow_id: workflowId, reset_state: true }),
  });
}

async function runtimeSnapshot(apiBase) {
  return fetchOk(apiUrl(apiBase, "/api/runtime"));
}

async function pollUntilComplete(apiBase, pollMs, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    await sleep(pollMs);
    const snap = await runtimeSnapshot(apiBase);
    const nodes = snap?.workflow?.nodes ?? [];
    if (nodes.length > 0 && nodes.every((n) => n.completed)) return snap;
  }
  throw new Error("Workflow did not complete within timeout");
}

// Returns when at least one job has an assigned worker.
async function waitForAnyAssignment(apiBase, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    await sleep(400);
    const snap = await runtimeSnapshot(apiBase);
    const jobs = snap?.jobs ?? [];
    if (jobs.some((j) => (j.assigned_workers?.length ?? 0) > 0)) return snap;
  }
  throw new Error("No assignment appeared within timeout");
}

async function openWorkerPages(browser, baseUrl, count, staggerMs = 200) {
  const context = await browser.newContext();
  const workerUrl = (() => {
    const u = new URL(baseUrl);
    u.searchParams.set("auto_worker", "1");
    u.hash = "#/";
    return u.toString();
  })();

  const pages = [];
  for (let i = 0; i < count; i++) {
    const page = await context.newPage();
    await page.goto(workerUrl, { waitUntil: "domcontentloaded", timeout: 30_000 });
    pages.push(page);
    if (staggerMs > 0 && i < count - 1) await sleep(staggerMs);
  }
  return { context, pages };
}

// ── scenarios ─────────────────────────────────────────────────────────────────

async function runSequential(opts, browser) {
  console.log("\n[1/3] Sequential — 1 worker, all shards in series");
  await activateWorkflow(opts.apiUrl, opts.workflowId, opts.adminToken);
  await sleep(300);

  const { context, pages } = await openWorkerPages(browser, opts.baseUrl, 1, 0);
  const t0 = Date.now();
  const snap = await pollUntilComplete(opts.apiUrl, opts.pollMs, opts.timeoutMs);
  const elapsedMs = Date.now() - t0;

  await Promise.allSettled(pages.map((p) => p.close()));
  await context.close();

  const stats = snap.stats ?? {};
  console.log(`  Done: ${fmt(elapsedMs)}  jobs_finalized=${stats.finalized_jobs}`);
  return { label: "Sequential (1 worker)", workers: 1, failedWorkers: 0, elapsedMs, stats };
}

async function runParallel(opts, browser) {
  console.log("\n[2/3] Parallel — 3 workers, all shards simultaneously");
  await activateWorkflow(opts.apiUrl, opts.workflowId, opts.adminToken);
  await sleep(300);

  const { context, pages } = await openWorkerPages(browser, opts.baseUrl, 3, opts.staggerMs);
  const t0 = Date.now();
  const snap = await pollUntilComplete(opts.apiUrl, opts.pollMs, opts.timeoutMs);
  const elapsedMs = Date.now() - t0;

  await Promise.allSettled(pages.map((p) => p.close()));
  await context.close();

  const stats = snap.stats ?? {};
  console.log(`  Done: ${fmt(elapsedMs)}  jobs_finalized=${stats.finalized_jobs}`);
  return { label: "Parallel (3 workers)", workers: 3, failedWorkers: 0, elapsedMs, stats };
}

async function runFaultTolerant(opts, browser) {
  console.log("\n[3/3] Fault-tolerant — 3 workers start; 1 crashes mid-assignment");
  await activateWorkflow(opts.apiUrl, opts.workflowId, opts.adminToken);
  await sleep(300);

  // Open 3 workers with a stagger so they don't all pull at exactly the same time.
  const { context, pages } = await openWorkerPages(browser, opts.baseUrl, 3, opts.staggerMs);
  const t0 = Date.now();

  // Wait until the scheduler has handed out at least one assignment, then kill
  // one of the browser worker pages to simulate a crash.
  console.log("  Waiting for first assignment...");
  await waitForAnyAssignment(opts.apiUrl, 30_000);
  console.log("  Crashing one worker (closing page)...");
  await pages[0].close();
  console.log(`  Worker crashed at t=${fmt(Date.now() - t0)}. Waiting for TTL + reassignment...`);

  const snap = await pollUntilComplete(opts.apiUrl, opts.pollMs, opts.timeoutMs);
  const elapsedMs = Date.now() - t0;

  await Promise.allSettled(pages.slice(1).map((p) => p.close()));
  await context.close();

  const stats = snap.stats ?? {};
  console.log(`  Done: ${fmt(elapsedMs)}  jobs_finalized=${stats.finalized_jobs}`);
  return { label: "Fault-tolerant (1 crash)", workers: 3, failedWorkers: 1, elapsedMs, stats };
}

// ── reporting ──────────────────────────────────────────────────────────────────

function fmt(ms) { return `${(ms / 1000).toFixed(2)}s`; }

function printResults(results) {
  const base = results[0].elapsedMs;

  const COL = [
    { h: "Scenario",          w: 28, v: (r) => r.label },
    { h: "Workers",           w: 9,  v: (r) => String(r.workers) },
    { h: "Failures",          w: 9,  v: (r) => String(r.failedWorkers) },
    { h: "Wall-clock",        w: 11, v: (r) => fmt(r.elapsedMs) },
    { h: "Speedup",           w: 9,  v: (r, i) => i === 0 ? "1.00×" : `${(base / r.elapsedMs).toFixed(2)}×` },
    { h: "vs Sequential",     w: 13, v: (r, i) => i === 0 ? "baseline" : `-${((1 - r.elapsedMs / base) * 100).toFixed(0)}%` },
  ];

  const header  = COL.map((c) => c.h.padEnd(c.w)).join("  ");
  const divider = COL.map((c) => "─".repeat(c.w)).join("  ");

  console.log("\n" + "═".repeat(header.length));
  console.log("  Benchmark Results — X402 Browser-Worker Scheduler");
  console.log("═".repeat(header.length));
  console.log("\n" + header);
  console.log(divider);
  results.forEach((r, i) => {
    console.log(COL.map((c) => String(c.v(r, i)).padEnd(c.w)).join("  "));
  });
  console.log(divider);
  console.log();

  const par = results.find((r) => r.workers === 3 && r.failedWorkers === 0);
  const ft  = results.find((r) => r.failedWorkers === 1);
  if (par) console.log(`  Parallel speedup     : ${(base / par.elapsedMs).toFixed(2)}× faster than sequential`);
  if (ft)  console.log(`  Fault recovery cost  : +${fmt(ft.elapsedMs - (par?.elapsedMs ?? base))} vs fault-free parallel`);
  console.log();
  console.log("  Note: All times include HTTP round-trips, WASM startup,");
  console.log("  JSON serialization, and Postgres persistence overhead.");
  console.log("  The fault-tolerant case additionally includes the assignment");
  console.log(`  TTL wait (ASSIGNMENT_TTL_SECONDS) before reassignment fires.`);
  console.log("═".repeat(header.length));
}

// ── main ──────────────────────────────────────────────────────────────────────

function parseArgs(argv) {
  const opts = {
    apiUrl:     "http://127.0.0.1:8080",
    baseUrl:    "http://127.0.0.1:5173/",
    adminToken: process.env.ADMIN_API_TOKEN || readEnvValue("ADMIN_API_TOKEN"),
    workflowId: "wf-monte-carlo-pi",
    headless:   true,
    pollMs:     600,
    timeoutMs:  8 * 60 * 1000,
    staggerMs:  250,
    skipFault:  false,
  };
  for (let i = 0; i < argv.length; i++) {
    const next = () => argv[++i];
    switch (argv[i]) {
      case "--api-url":     opts.apiUrl     = next(); break;
      case "--base-url":    opts.baseUrl    = next(); break;
      case "--admin-token": opts.adminToken = next(); break;
      case "--workflow-id": opts.workflowId = next(); break;
      case "--headed":      opts.headless   = false; break;
      case "--headless":    opts.headless   = true;  break;
      case "--timeout-ms":  opts.timeoutMs  = Number(next()); break;
      case "--skip-fault":  opts.skipFault  = true;  break;
    }
  }
  if (!opts.adminToken) throw new Error("ADMIN_API_TOKEN required (set in .env or --admin-token)");
  return opts;
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  opts.apiUrl  = normalizeBase(opts.apiUrl).replace(/\/$/, "");
  opts.baseUrl = normalizeBase(opts.baseUrl);

  console.log("Waiting for backend...");
  await waitHealthy(opts.apiUrl);
  console.log(`Backend healthy.`);
  console.log(`Workflow: ${opts.workflowId}  |  Mode: ${opts.headless ? "headless" : "headed"}`);

  const { chromium } = await import("playwright");
  const browser = await chromium.launch({ headless: opts.headless });

  const results = [];
  try {
    results.push(await runSequential(opts, browser));
    await sleep(1500);
    results.push(await runParallel(opts, browser));
    if (!opts.skipFault) {
      await sleep(1500);
      results.push(await runFaultTolerant(opts, browser));
    }
  } finally {
    await browser.close();
  }

  printResults(results);
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exitCode = 1;
});
