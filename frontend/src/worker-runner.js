self.onmessage = async (event) => {
  const req = event.data || {};
  const reqID = req.req_id;

  try {
    const wasmURL = req.wasm_url;
    const args = Array.isArray(req.args) ? req.args : [];
    if (!wasmURL) {
      throw new Error("wasm_url missing");
    }

    const bytes = await fetchWasmBytes(wasmURL);
    const { output, mode } = await runWasm(bytes, args);
    const resultSig = await sha256Hex(JSON.stringify({ output, args, mode }));

    self.postMessage({
      req_id: reqID,
      output,
      mode,
      result_sig: resultSig,
    });
  } catch (err) {
    self.postMessage({
      req_id: reqID,
      error: String(err),
    });
  }
};

async function fetchWasmBytes(url) {
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`failed to fetch wasm: ${res.status}`);
  }
  return res.arrayBuffer();
}

async function runWasm(wasmBytes, args) {
  let mod;
  try {
    mod = await WebAssembly.instantiate(wasmBytes, {});
  } catch (err) {
    throw new Error(`wasm instantiate failed: ${String(err)}`);
  }

  return executeRun(mod.instance.exports || {}, args);
}

function executeRun(exports, args) {
  if (typeof exports.run !== "function") {
    throw new Error("wasm does not export run");
  }
  const fn = exports.run;
  const arity = typeof fn.length === "number" ? fn.length : 0;
  const callArgs = new Array(arity);
  for (let i = 0; i < arity; i += 1) {
    callArgs[i] = toInt(args[i], 0);
  }
  return { output: fn(...callArgs), mode: `run(${callArgs.join(",")})` };
}

function toInt(value, fallback) {
  const num = Number(value);
  return Number.isFinite(num) ? Math.trunc(num) : fallback;
}

async function sha256Hex(text) {
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const view = new Uint8Array(digest);
  let hex = "";
  for (let i = 0; i < view.length; i += 1) {
    hex += view[i].toString(16).padStart(2, "0");
  }
  return hex;
}
