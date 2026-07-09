import { chromium } from "playwright";
import crypto from "node:crypto";
import os from "node:os";

function parseArgs(argv) {
  const options = {
    count: 30,
    baseUrl: "http://127.0.0.1:5173/",
    headless: false,
    staggerMs: 150,
    startupTimeoutMs: 30_000,
    workerId: process.env.X402_WORKER_ID || "",
    workerIdSeed: process.env.X402_WORKER_ID_SEED || os.hostname(),
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = argv[i + 1];

    if (arg === "--count" && next) {
      options.count = Math.max(1, Number.parseInt(next, 10) || options.count);
      i += 1;
      continue;
    }
    if (arg === "--base-url" && next) {
      options.baseUrl = next;
      i += 1;
      continue;
    }
    if (arg === "--stagger-ms" && next) {
      options.staggerMs = Math.max(0, Number.parseInt(next, 10) || options.staggerMs);
      i += 1;
      continue;
    }
    if (arg === "--startup-timeout-ms" && next) {
      options.startupTimeoutMs = Math.max(1000, Number.parseInt(next, 10) || options.startupTimeoutMs);
      i += 1;
      continue;
    }
    if (arg === "--worker-id" && next) {
      options.workerId = next;
      i += 1;
      continue;
    }
    if (arg === "--worker-id-seed" && next) {
      options.workerIdSeed = next;
      i += 1;
      continue;
    }
    if (arg === "--headless") {
      options.headless = true;
      continue;
    }
    if (arg === "--headed") {
      options.headless = false;
      continue;
    }
  }

  if (options.workerId && options.count !== 1) {
    throw new Error("--worker-id can only be used with --count 1; use --worker-id-seed for multiple workers");
  }

  return options;
}

function isWorkerAddress(value) {
  return /^0x[0-9a-fA-F]{40}$/.test(value);
}

function deterministicWorkerAddress(seed, workerIndex) {
  const hash = crypto
    .createHash("sha256")
    .update(`x402-browser-worker:${seed}:${workerIndex}`)
    .digest("hex");
  return `0x${hash.slice(0, 40)}`;
}

function workerAddressFor(options, workerIndex) {
  if (options.workerId) {
    if (!isWorkerAddress(options.workerId)) {
      throw new Error(`invalid --worker-id: ${options.workerId}`);
    }
    return options.workerId;
  }
  return deterministicWorkerAddress(options.workerIdSeed, workerIndex);
}

function buildWorkerUrl(baseUrl, workerID) {
  const url = new URL(baseUrl);
  url.searchParams.set("auto_worker", "1");
  url.searchParams.set("auto_worker_id", workerID);
  url.pathname = "/";
  url.hash = "";
  return url.toString();
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function main() {
  const options = parseArgs(process.argv.slice(2));

  console.log(
    `[browser-workers] launching count=${options.count} headless=${options.headless} base=${options.baseUrl} worker_id_seed=${options.workerIdSeed}`,
  );

  const browser = await chromium.launch({
    headless: options.headless,
  });
  const context = await browser.newContext();

  const shutdown = async (signal) => {
    console.log(`[browser-workers] shutting down (${signal})`);
    await context.close().catch(() => {});
    await browser.close().catch(() => {});
    process.exit(0);
  };

  process.on("SIGINT", () => {
    void shutdown("SIGINT");
  });
  process.on("SIGTERM", () => {
    void shutdown("SIGTERM");
  });

  for (let i = 0; i < options.count; i += 1) {
    const page = await context.newPage();
    const workerIndex = i + 1;
    const workerID = workerAddressFor(options, workerIndex);
    const targetUrl = buildWorkerUrl(options.baseUrl, workerID);

    page.on("console", (message) => {
      if (workerIndex <= 3) {
        console.log(`[worker:${workerIndex}] console ${message.type()}: ${message.text()}`);
      }
    });
    page.on("pageerror", (error) => {
      console.log(`[worker:${workerIndex}] pageerror: ${String(error)}`);
    });

    await page.goto(targetUrl, {
      timeout: options.startupTimeoutMs,
      waitUntil: "domcontentloaded",
    });
    console.log(`[browser-workers] opened worker page ${workerIndex}/${options.count} worker_id=${workerID}`);

    if (options.staggerMs > 0 && workerIndex < options.count) {
      await delay(options.staggerMs);
    }
  }

  console.log("[browser-workers] all worker pages opened; press Ctrl+C to stop");
  await new Promise(() => {});
}

main().catch((error) => {
  console.error("[browser-workers] failed:", error);
  process.exit(1);
});
