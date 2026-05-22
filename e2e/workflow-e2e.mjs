#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

function readDotEnvValue(name) {
  for (const path of ["../.env", ".env"]) {
    try {
      const raw = readFileSync(path, "utf8");
      for (const line of raw.split(/\r?\n/)) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) {
          continue;
        }
        const equals = trimmed.indexOf("=");
        if (equals <= 0 || trimmed.slice(0, equals).trim() !== name) {
          continue;
        }
        return trimmed.slice(equals + 1).trim().replace(/^["']|["']$/g, "");
      }
    } catch {
      // The E2E helper can run from the repo root or automated-worker/.
    }
  }
  return "";
}

const defaultOptions = {
  apiUrl: process.env.API_URL || process.env.BACKEND_URL || "http://127.0.0.1:8080",
  baseUrl: process.env.FRONTEND_URL || "http://127.0.0.1:5173/",
  adminToken: process.env.E2E_ADMIN_TOKEN || process.env.ADMIN_API_TOKEN || readDotEnvValue("ADMIN_API_TOKEN"),
  workflowId: process.env.E2E_WORKFLOW_ID || "",
  headless: process.env.HEADED !== "1",
  skipAutoWorker: false,
  requireWorkerProgress: false,
  expectFinalized: process.env.E2E_EXPECT_FINALIZED === "1",
  expectNewFinalizedJobs: Number(process.env.E2E_EXPECT_NEW_FINALIZED_JOBS || 0),
  expectPayments: process.env.E2E_EXPECT_PAYMENTS === "1",
  expectWorkflowComplete: process.env.E2E_EXPECT_WORKFLOW_COMPLETE === "1",
  resetWorkflow: process.env.E2E_RESET_WORKFLOW === "1",
  minFinalizedJobs: Number(process.env.E2E_MIN_FINALIZED_JOBS || 1),
  workerCount: Number(process.env.E2E_WORKER_COUNT || 1),
  workerStaggerMs: Number(process.env.E2E_WORKER_STAGGER_MS || 150),
  timeoutMs: Number(process.env.E2E_TIMEOUT_MS || 30_000),
  workerTimeoutMs: Number(process.env.E2E_WORKER_TIMEOUT_MS || 20_000),
};

function printHelp() {
  console.log(`Usage: npm run smoke -- [options]

Options:
  --api-url <url>                Backend API URL. Default: ${defaultOptions.apiUrl}
  --base-url <url>               Frontend URL. Default: ${defaultOptions.baseUrl}
  --timeout-ms <ms>              Backend/frontend timeout. Default: ${defaultOptions.timeoutMs}
  --worker-timeout-ms <ms>       Auto-worker wait timeout. Default: ${defaultOptions.workerTimeoutMs}
  --workflow-id <id>             Activate a workflow before opening workers. Requires --admin-token.
  --admin-token <token>          Admin bearer token. Defaults to E2E_ADMIN_TOKEN or ADMIN_API_TOKEN.
  --reset-workflow               Reset persisted node state when activating --workflow-id.
  --worker-count <n>             Number of auto-worker pages to open. Default: ${defaultOptions.workerCount}
  --worker-stagger-ms <ms>       Delay between opening worker pages. Default: ${defaultOptions.workerStaggerMs}
  --expect-finalized             Wait until at least --min-finalized-jobs are finalized.
  --expect-new-finalized <n>     Wait for n additional finalized jobs after workers start.
  --expect-workflow-complete     Wait until every node in /api/runtime workflow is completed.
  --expect-payments              Assert at least one opened worker has a payment event.
  --min-finalized-jobs <n>       Finalized job threshold for --expect-finalized. Default: ${defaultOptions.minFinalizedJobs}
  --headed                       Run Chromium with a visible browser window.
  --headless                     Run Chromium headless.
  --skip-auto-worker             Only test backend health and the runtime page.
  --require-worker-progress      Require the auto-worker to pull/finish/no-op after it starts.
  --help                         Show this help message.
`);
}

function parseArgs(argv) {
  const options = { ...defaultOptions };

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    const next = () => {
      index += 1;
      if (index >= argv.length) {
        throw new Error(`${arg} requires a value`);
      }
      return argv[index];
    };

    switch (arg) {
      case "--api-url":
        options.apiUrl = next();
        break;
      case "--base-url":
        options.baseUrl = next();
        break;
      case "--timeout-ms":
        options.timeoutMs = Number(next());
        break;
      case "--worker-timeout-ms":
        options.workerTimeoutMs = Number(next());
        break;
      case "--workflow-id":
        options.workflowId = next();
        break;
      case "--admin-token":
        options.adminToken = next();
        break;
      case "--reset-workflow":
        options.resetWorkflow = true;
        break;
      case "--worker-count":
        options.workerCount = Number(next());
        break;
      case "--worker-stagger-ms":
        options.workerStaggerMs = Number(next());
        break;
      case "--expect-finalized":
        options.expectFinalized = true;
        break;
      case "--expect-new-finalized":
        options.expectNewFinalizedJobs = Number(next());
        break;
      case "--expect-workflow-complete":
        options.expectWorkflowComplete = true;
        options.expectFinalized = true;
        break;
      case "--expect-payments":
        options.expectPayments = true;
        break;
      case "--min-finalized-jobs":
        options.minFinalizedJobs = Number(next());
        break;
      case "--headed":
        options.headless = false;
        break;
      case "--headless":
        options.headless = true;
        break;
      case "--skip-auto-worker":
        options.skipAutoWorker = true;
        break;
      case "--require-worker-progress":
        options.requireWorkerProgress = true;
        break;
      case "--help":
      case "-h":
        printHelp();
        process.exit(0);
      default:
        throw new Error(`Unknown option: ${arg}`);
    }
  }

  if (!Number.isFinite(options.timeoutMs) || options.timeoutMs <= 0) {
    throw new Error("--timeout-ms must be a positive number");
  }
  if (!Number.isFinite(options.workerTimeoutMs) || options.workerTimeoutMs <= 0) {
    throw new Error("--worker-timeout-ms must be a positive number");
  }
  if (!Number.isFinite(options.workerCount) || options.workerCount <= 0) {
    throw new Error("--worker-count must be a positive number");
  }
  if (!Number.isFinite(options.workerStaggerMs) || options.workerStaggerMs < 0) {
    throw new Error("--worker-stagger-ms must be zero or a positive number");
  }
  if (!Number.isFinite(options.minFinalizedJobs) || options.minFinalizedJobs <= 0) {
    throw new Error("--min-finalized-jobs must be a positive number");
  }
  if (!Number.isFinite(options.expectNewFinalizedJobs) || options.expectNewFinalizedJobs < 0) {
    throw new Error("--expect-new-finalized must be zero or a positive number");
  }
  if (options.workflowId && !options.adminToken) {
    throw new Error("--workflow-id requires --admin-token, E2E_ADMIN_TOKEN, or ADMIN_API_TOKEN");
  }
  if (options.skipAutoWorker && (options.expectFinalized || options.expectNewFinalizedJobs > 0 || options.expectPayments)) {
    throw new Error("--skip-auto-worker cannot be combined with workflow/payment progress assertions");
  }

  return {
    ...options,
    apiUrl: normalizeBaseUrl(options.apiUrl),
    baseUrl: normalizeBaseUrl(options.baseUrl),
    workflowId: String(options.workflowId || "").trim(),
    adminToken: String(options.adminToken || "").trim(),
    workerCount: Math.max(1, Math.floor(options.workerCount)),
    workerStaggerMs: Math.max(0, Math.floor(options.workerStaggerMs)),
    minFinalizedJobs: Math.max(1, Math.floor(options.minFinalizedJobs)),
    expectNewFinalizedJobs: Math.max(0, Math.floor(options.expectNewFinalizedJobs)),
  };
}

function normalizeBaseUrl(value) {
  const url = new URL(value);
  if (!url.pathname.endsWith("/")) {
    url.pathname = `${url.pathname}/`;
  }
  return url.toString();
}

function urlFor(baseUrl, path) {
  return new URL(path, baseUrl).toString();
}

function frontendRoute(baseUrl, path, params = {}) {
  const url = new URL(baseUrl);
  url.pathname = path;
  for (const [key, value] of Object.entries(params)) {
    url.searchParams.set(key, value);
  }
  url.hash = "";
  return url.toString();
}

async function fetchWithTimeout(url, timeoutMs, init = undefined) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(url, { ...init, signal: controller.signal });
  } finally {
    clearTimeout(timeout);
  }
}

async function waitForHealthyBackend(apiUrl, timeoutMs) {
  const healthURL = urlFor(apiUrl, "/healthz");
  const deadline = Date.now() + timeoutMs;
  let lastError = "";

  while (Date.now() < deadline) {
    try {
      const response = await fetchWithTimeout(healthURL, 2_000);
      if (response.ok) {
        return;
      }
      lastError = `HTTP ${response.status}`;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    await sleep(500);
  }

  throw new Error(`Backend did not become healthy at ${healthURL}: ${lastError}`);
}

async function fetchRuntimeSnapshot(apiUrl, timeoutMs) {
  const runtimeURL = urlFor(apiUrl, "/api/runtime");
  const response = await fetchWithTimeout(runtimeURL, timeoutMs);
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`/api/runtime failed: HTTP ${response.status} ${body}`);
  }
  const snapshot = await response.json();
  assertRuntimeShape(snapshot);
  return snapshot;
}

async function activateWorkflow(apiUrl, options) {
  if (!options.workflowId) {
    return null;
  }

  const activateURL = urlFor(apiUrl, "/api/admin/workflows/activate");
  const response = await fetchWithTimeout(activateURL, options.timeoutMs, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${options.adminToken}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      workflow_id: options.workflowId,
      reset_state: options.resetWorkflow,
    }),
  });
  const body = await response.text();
  if (!response.ok) {
    throw new Error(`/api/admin/workflows/activate failed: HTTP ${response.status} ${body}`);
  }
  return body ? JSON.parse(body) : {};
}

function assertRuntimeShape(snapshot) {
  if (!snapshot || typeof snapshot !== "object") {
    throw new Error("/api/runtime did not return an object");
  }
  if (!snapshot.stats || typeof snapshot.stats !== "object") {
    throw new Error("/api/runtime response is missing stats");
  }

  for (const key of ["queued_jobs", "finalized_jobs", "workers_online", "pending_payments", "total_jobs"]) {
    if (typeof snapshot.stats[key] !== "number") {
      throw new Error(`/api/runtime stats.${key} must be a number`);
    }
  }
}

async function createCheckedPage(context, label) {
  const page = await context.newPage();
  const errors = [];

  page.on("pageerror", (error) => {
    errors.push(`${label} page error: ${error.message}`);
  });
  page.on("console", (message) => {
    if (message.type() === "error") {
      errors.push(`${label} console error: ${message.text()}`);
    }
  });

  return {
    page,
    assertClean() {
      if (errors.length > 0) {
        throw new Error(errors.join("\n"));
      }
    },
  };
}

async function assertRuntimePage(context, baseUrl, timeoutMs) {
  const checked = await createCheckedPage(context, "runtime");
  const { page } = checked;

  await page.goto(frontendRoute(baseUrl, "/runtime"), {
    waitUntil: "networkidle",
    timeout: timeoutMs,
  });
  await page.getByText("Live Workflow").waitFor({ timeout: timeoutMs });
  await page.getByText("Workflow Graph").waitFor({ timeout: timeoutMs });
  await page.getByText("Queued Jobs").waitFor({ timeout: timeoutMs });

  checked.assertClean();
  await page.close();
}

async function waitForControlValue(page, pattern, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastValues = [];

  while (Date.now() < deadline) {
    lastValues = await page.locator("input, textarea").evaluateAll((elements) =>
      elements.map((element) => element.value ?? ""),
    );
    if (lastValues.some((value) => pattern.test(value))) {
      return;
    }
    await sleep(250);
  }

  throw new Error(`timed out waiting for control value ${pattern}; last values=${JSON.stringify(lastValues)}`);
}

async function openAutoWorkerPages(context, baseUrl, options) {
  const checkedPages = [];
  const workerIDs = [];

  for (let index = 0; index < options.workerCount; index += 1) {
    const workerNumber = index + 1;
    const checked = await createCheckedPage(context, `auto-worker:${workerNumber}`);
    const { page } = checked;

    await page.goto(frontendRoute(baseUrl, "/", { auto_worker: "1" }), {
      waitUntil: "domcontentloaded",
      timeout: options.timeoutMs,
    });
    await page.getByText("Browser Worker Interface").waitFor({ timeout: options.timeoutMs });
    await waitForControlValue(page, /Auto worker mode enabled/, options.workerTimeoutMs);

    const workerID = await page.locator("#workerId").inputValue({ timeout: options.workerTimeoutMs });
    if (!/^0x[a-fA-F0-9]{40}$/.test(workerID)) {
      throw new Error(`auto-worker:${workerNumber} did not expose a wallet-like worker id: ${workerID}`);
    }
    workerIDs.push(workerID);

    if (options.requireWorkerProgress) {
      await waitForControlValue(
        page,
        /No job available|Job assigned|WASM executed|Result submitted|finalized|pull failed|register failed/,
        options.workerTimeoutMs,
      );
    }

    checked.assertClean();
    checkedPages.push(checked);

    if (options.workerStaggerMs > 0 && workerNumber < options.workerCount) {
      await sleep(options.workerStaggerMs);
    }
  }

  return {
    workerIDs,
    assertClean() {
      for (const checked of checkedPages) {
        checked.assertClean();
      }
    },
    async close() {
      await Promise.allSettled(checkedPages.map(({ page }) => page.close()));
    },
  };
}

function runtimeSummary(snapshot) {
  const stats = snapshot.stats ?? {};
  const completedNodes = Array.isArray(snapshot.workflow?.nodes)
    ? snapshot.workflow.nodes.filter((node) => node.completed).length
    : 0;
  const totalNodes = Array.isArray(snapshot.workflow?.nodes) ? snapshot.workflow.nodes.length : 0;
  return `workflow=${snapshot.active_workflow_id || snapshot.loaded_workflow_id || "-"} finalized=${stats.finalized_jobs} queued=${stats.queued_jobs} pending_payments=${stats.pending_payments} completed_nodes=${completedNodes}/${totalNodes}`;
}

function workflowComplete(snapshot) {
  const nodes = snapshot.workflow?.nodes;
  return Array.isArray(nodes) && nodes.length > 0 && nodes.every((node) => node.completed === true);
}

async function waitForRuntimeAssertions(apiUrl, options, baselineSnapshot) {
  if (!options.expectFinalized && options.expectNewFinalizedJobs === 0 && !options.expectWorkflowComplete) {
    return fetchRuntimeSnapshot(apiUrl, options.timeoutMs);
  }

  const deadline = Date.now() + options.workerTimeoutMs;
  const baselineFinalized = baselineSnapshot.stats.finalized_jobs;
  let lastSummary = runtimeSummary(baselineSnapshot);

  while (Date.now() < deadline) {
    const snapshot = await fetchRuntimeSnapshot(apiUrl, options.timeoutMs);
    lastSummary = runtimeSummary(snapshot);

    const absoluteFinalizedOK = !options.expectFinalized || snapshot.stats.finalized_jobs >= options.minFinalizedJobs;
    const newFinalizedOK =
      options.expectNewFinalizedJobs === 0 ||
      snapshot.stats.finalized_jobs >= baselineFinalized + options.expectNewFinalizedJobs;
    const workflowCompleteOK = !options.expectWorkflowComplete || workflowComplete(snapshot);

    if (absoluteFinalizedOK && newFinalizedOK && workflowCompleteOK) {
      return snapshot;
    }

    await sleep(1_000);
  }

  throw new Error(`runtime assertions timed out: ${lastSummary}`);
}

async function fetchPaymentsForWorker(apiUrl, workerID, timeoutMs) {
  const paymentsURL = new URL(urlFor(apiUrl, "/api/payments"));
  paymentsURL.searchParams.set("worker_id", workerID);
  const response = await fetchWithTimeout(paymentsURL.toString(), timeoutMs);
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`/api/payments failed for ${workerID}: HTTP ${response.status} ${body}`);
  }
  return response.json();
}

async function waitForPaymentEvents(apiUrl, workerIDs, options) {
  if (!options.expectPayments) {
    return [];
  }

  const deadline = Date.now() + options.workerTimeoutMs;
  let lastError = "";

  while (Date.now() < deadline) {
    const batches = [];
    for (const workerID of workerIDs) {
      try {
        const events = await fetchPaymentsForWorker(apiUrl, workerID, options.timeoutMs);
        batches.push(...events);
      } catch (error) {
        lastError = error instanceof Error ? error.message : String(error);
      }
    }
    if (batches.length > 0) {
      return batches;
    }
    await sleep(1_000);
  }

  throw new Error(`payment assertion timed out for ${workerIDs.length} workers${lastError ? `: ${lastError}` : ""}`);
}

async function loadPlaywright() {
  try {
    return await import("playwright");
  } catch (error) {
    if (error?.code === "ERR_MODULE_NOT_FOUND") {
      throw new Error(
        "Playwright is not installed. Run `cd automated-worker && npm install && npm run playwright:install` first.",
      );
    }
    throw error;
  }
}

async function main() {
  const options = parseArgs(process.argv.slice(2));

  console.log(`Backend:  ${options.apiUrl}`);
  console.log(`Frontend: ${options.baseUrl}`);

  const { chromium } = await loadPlaywright();
  await waitForHealthyBackend(options.apiUrl, options.timeoutMs);
  const activation = await activateWorkflow(options.apiUrl, options);
  if (activation) {
    console.log(
      `Activated workflow: ${activation.workflow_id || options.workflowId} reset=${Boolean(activation.reset_state)}`,
    );
  }
  const snapshot = await fetchRuntimeSnapshot(options.apiUrl, options.timeoutMs);
  console.log(
    `Runtime ok: ${runtimeSummary(snapshot)}`,
  );

  const browser = await chromium.launch({ headless: options.headless });
  let autoWorkers = null;
  try {
    const context = await browser.newContext();
    await assertRuntimePage(context, options.baseUrl, options.timeoutMs);
    console.log("Runtime page ok");

    if (!options.skipAutoWorker) {
      autoWorkers = await openAutoWorkerPages(context, options.baseUrl, options);
      console.log(`Auto-worker pages ok: ${autoWorkers.workerIDs.length}`);

      const finalSnapshot = await waitForRuntimeAssertions(options.apiUrl, options, snapshot);
      console.log(`Runtime assertions ok: ${runtimeSummary(finalSnapshot)}`);

      const payments = await waitForPaymentEvents(options.apiUrl, autoWorkers.workerIDs, options);
      if (options.expectPayments) {
        console.log(`Payment assertions ok: events=${payments.length}`);
      }

      autoWorkers.assertClean();
    }
  } finally {
    if (autoWorkers) {
      await autoWorkers.close();
    }
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
});
