self.onmessage = async (event) => {
  const req = event.data || {};
  const reqID = req.req_id;

  try {
    const wasmURL = req.wasm_url;
    const args = Array.isArray(req.args) ? req.args : [];
    const inputContext = req.input_context && typeof req.input_context === "object" ? req.input_context : {};
    if (!wasmURL) {
      throw new Error("wasm_url missing");
    }

    const bytes = await fetchWasmBytes(wasmURL);
    const { output, mode } = await runWasm(bytes, args, inputContext);
    const resultSig = await sha256Hex(JSON.stringify({ output, args, inputContext, mode }));

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

async function runWasm(wasmBytes, args, inputContext) {
  const wasiState = { memory: null };
  let mod;
  try {
    mod = await WebAssembly.instantiate(wasmBytes, createImportObject(wasiState));
  } catch (err) {
    throw new Error(`wasm instantiate failed: ${String(err)}`);
  }

  const exports = mod.instance.exports || {};
  if (exports.memory instanceof WebAssembly.Memory) {
    wasiState.memory = exports.memory;
  }
  if (canRunJSON(exports)) {
    return executeRunJSON(exports, inputContext);
  }
  return executeRun(exports, args);
}

function createImportObject(wasiState) {
  const noop = () => 0;
  const getMemoryView = () => {
    const memory = wasiState?.memory;
    return memory instanceof WebAssembly.Memory ? new DataView(memory.buffer) : null;
  };
  const writeClockValue = (ptr, valueNs) => {
    const view = getMemoryView();
    const offset = toInt(ptr, -1);
    if (!view || offset < 0 || offset + 8 > view.byteLength) {
      return 21;
    }
    view.setBigUint64(offset, valueNs, true);
    return 0;
  };
  const nowMonotonicNs = () => {
    if (typeof performance !== "undefined" && typeof performance.now === "function") {
      return BigInt(Math.floor(performance.now() * 1_000_000));
    }
    return BigInt(Date.now()) * 1_000_000n;
  };
  const nowRealtimeNs = () => BigInt(Date.now()) * 1_000_000n;
  const env = {
    abort: noop,
    emscripten_notify_memory_growth: noop,
    emscripten_memcpy_big: noop,
  };
  const wasi = {
    args_get: noop,
    args_sizes_get: noop,
    environ_get: noop,
    environ_sizes_get: noop,
    fd_close: noop,
    fd_fdstat_get: noop,
    fd_seek: noop,
    fd_write: noop,
    clock_res_get: (_clockID, resolutionPtr) => writeClockValue(resolutionPtr, 1_000_000n),
    clock_time_get: (clockID, _precision, timePtr) => {
      const id = toInt(clockID, 0);
      const nowNs = id === 0 ? nowRealtimeNs() : nowMonotonicNs();
      return writeClockValue(timePtr, nowNs);
    },
    random_get: noop,
    proc_exit: (code) => {
      throw new Error(`wasi proc_exit(${Number(code) || 0})`);
    },
  };

  const proxy = new Proxy(wasi, {
    get(target, prop) {
      if (typeof prop === "string" && !(prop in target)) {
        return noop;
      }
      return target[prop];
    },
  });

  const envProxy = new Proxy(env, {
    get(target, prop) {
      if (typeof prop === "string" && !(prop in target)) {
        return noop;
      }
      return target[prop];
    },
  });

  return {
    env: envProxy,
    wasi_snapshot_preview1: proxy,
    wasi_unstable: proxy,
  };
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

function canRunJSON(exports) {
  return (
    typeof exports.run_json === "function" &&
    typeof exports.get_input_ptr === "function" &&
    typeof exports.get_input_capacity === "function" &&
    typeof exports.get_output_ptr === "function" &&
    typeof exports.get_output_len === "function" &&
    exports.memory instanceof WebAssembly.Memory
  );
}

function executeRunJSON(exports, inputContext) {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  const jsonText = JSON.stringify(inputContext || {});
  const inputBytes = encoder.encode(jsonText);

  let mem = new Uint8Array(exports.memory.buffer);
  const inputPtr = toInt(exports.get_input_ptr(), 0);
  const inputCap = toInt(exports.get_input_capacity(), 0);
  if (inputPtr <= 0 || inputCap <= 0) {
    throw new Error("run_json ABI invalid input buffer");
  }
  if (inputBytes.length > inputCap) {
    throw new Error(`run_json input too large: ${inputBytes.length} > ${inputCap}`);
  }

  mem.set(inputBytes, inputPtr);
  const rc = toInt(exports.run_json(inputBytes.length), -1);
  if (rc !== 0) {
    throw new Error(`run_json returned non-zero status: ${rc}`);
  }

  mem = new Uint8Array(exports.memory.buffer);
  const outPtr = toInt(exports.get_output_ptr(), 0);
  const outLen = toInt(exports.get_output_len(), 0);
  if (outPtr <= 0 || outLen < 0 || outPtr + outLen > mem.length) {
    throw new Error("run_json ABI invalid output buffer");
  }

  const outBytes = mem.slice(outPtr, outPtr + outLen);
  const outText = decoder.decode(outBytes);
  let output;
  try {
    output = JSON.parse(outText);
  } catch {
    output = outText;
  }
  return { output, mode: `run_json(${inputBytes.length}B)` };
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
