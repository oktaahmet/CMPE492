export function createImportObject(wasiState) {
  const noop = () => 0;
  const textDecoder = new TextDecoder();
  const textEncoder = new TextEncoder();
  const fetchState = {
    lastHTTPStatus: 0,
    lastErrorCode: 0,
  };
  const FETCHX_MAX_RESPONSE_BYTES = 16 * 1024 * 1024;
  const FETCHX_DEFAULT_TIMEOUT_MS = 3000;
  const FETCHX_MAX_TIMEOUT_MS = 10000;
  const FETCHX_ERROR_INVALID_ARGS = 1;
  const FETCHX_ERROR_INVALID_MEMORY = 2;
  const FETCHX_ERROR_INVALID_URL = 3;
  const FETCHX_ERROR_BAD_PROTOCOL = 4;
  const FETCHX_ERROR_REQUEST_FAILED = 5;
  const FETCHX_ERROR_TIMEOUT = 6;
  const FETCHX_ERROR_HTTP_STATUS = 7;
  const FETCHX_ERROR_RESPONSE_TOO_LARGE = 8;

  const getMemoryView = () => {
    const memory = wasiState?.memory;
    return memory instanceof WebAssembly.Memory ? new DataView(memory.buffer) : null;
  };

  const getMemoryBytes = () => {
    const memory = wasiState?.memory;
    return memory instanceof WebAssembly.Memory ? new Uint8Array(memory.buffer) : null;
  };

  const readUTF8 = (ptr, len) => {
    const bytes = getMemoryBytes();
    const offset = toInt(ptr, -1);
    const size = toInt(len, -1);
    if (!bytes || offset < 0 || size < 0 || offset + size > bytes.length) {
      return null;
    }
    return textDecoder.decode(bytes.slice(offset, offset + size));
  };

  const writeBytes = (ptr, payload) => {
    const bytes = getMemoryBytes();
    const offset = toInt(ptr, -1);
    if (!bytes || offset < 0 || offset + payload.length > bytes.length) {
      return false;
    }
    bytes.set(payload, offset);
    return true;
  };

  const setFetchError = (errorCode, httpStatus = 0) => {
    fetchState.lastErrorCode = errorCode;
    fetchState.lastHTTPStatus = httpStatus;
    return -1;
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

  const fillRandomBytes = (bufPtr, bufLen) => {
    const bytes = getMemoryBytes();
    const offset = toInt(bufPtr, -1);
    const size = toInt(bufLen, -1);
    if (!bytes || offset < 0 || size < 0 || offset + size > bytes.length) {
      return 21;
    }

    const cryptoObject =
      (typeof globalThis !== "undefined" && globalThis.crypto) ||
      (typeof self !== "undefined" && self.crypto) ||
      null;
    if (cryptoObject && typeof cryptoObject.getRandomValues === "function") {
      let cursor = 0;
      while (cursor < size) {
        const chunkSize = Math.min(65536, size - cursor);
        cryptoObject.getRandomValues(bytes.subarray(offset + cursor, offset + cursor + chunkSize));
        cursor += chunkSize;
      }
      return 0;
    }

    for (let i = 0; i < size; i += 1) {
      bytes[offset + i] = Math.floor(Math.random() * 256);
    }
    return 0;
  };

  const env = {
    abort: noop,
    emscripten_notify_memory_growth: noop,
    emscripten_memcpy_big: noop,
    fetchx_bytes: (urlPtr, urlLen, outPtr, outCap, timeoutMs) => {
      fetchState.lastHTTPStatus = 0;
      fetchState.lastErrorCode = 0;

      const outputCap = toInt(outCap, 0);
      if (outputCap <= 0) {
        return setFetchError(FETCHX_ERROR_INVALID_ARGS);
      }

      const rawURL = readUTF8(urlPtr, urlLen);
      if (typeof rawURL !== "string" || rawURL.trim() === "") {
        return setFetchError(FETCHX_ERROR_INVALID_URL);
      }

      let resolvedURL;
      try {
        resolvedURL = new URL(rawURL, self.location?.href || "http://localhost/");
      } catch {
        return setFetchError(FETCHX_ERROR_INVALID_URL);
      }
      if (resolvedURL.protocol !== "http:" && resolvedURL.protocol !== "https:") {
        return setFetchError(FETCHX_ERROR_BAD_PROTOCOL);
      }

      const xhr = new XMLHttpRequest();
      const boundedTimeoutMs = Math.max(1, Math.min(toInt(timeoutMs, FETCHX_DEFAULT_TIMEOUT_MS), FETCHX_MAX_TIMEOUT_MS));
      try {
        xhr.open("GET", resolvedURL.toString(), false);
        xhr.responseType = "arraybuffer";
      } catch {
        return setFetchError(FETCHX_ERROR_REQUEST_FAILED);
      }

      try {
        xhr.timeout = boundedTimeoutMs;
      } catch {
        // Best-effort only: some runtimes may reject timeout on sync XHR.
      }

      try {
        xhr.send();
      } catch (error) {
        const text = String(error);
        if (text.includes("timeout")) {
          return setFetchError(FETCHX_ERROR_TIMEOUT);
        }
        return setFetchError(FETCHX_ERROR_REQUEST_FAILED);
      }

      fetchState.lastHTTPStatus = toInt(xhr.status, 0);
      if (fetchState.lastHTTPStatus < 200 || fetchState.lastHTTPStatus >= 300) {
        return setFetchError(FETCHX_ERROR_HTTP_STATUS, fetchState.lastHTTPStatus);
      }

      const response = xhr.response;
      const payload = response instanceof ArrayBuffer ? new Uint8Array(response) : new Uint8Array();
      const maxWritable = Math.min(outputCap, FETCHX_MAX_RESPONSE_BYTES);
      if (payload.length > maxWritable) {
        return setFetchError(FETCHX_ERROR_RESPONSE_TOO_LARGE, fetchState.lastHTTPStatus);
      }
      if (!writeBytes(outPtr, payload)) {
        return setFetchError(FETCHX_ERROR_INVALID_MEMORY, fetchState.lastHTTPStatus);
      }
      return payload.length;
    },
    fetchx_text: (urlPtr, urlLen, outPtr, outCap, timeoutMs) => {
      fetchState.lastHTTPStatus = 0;
      fetchState.lastErrorCode = 0;

      const outputCap = toInt(outCap, 0);
      if (outputCap <= 0) {
        return setFetchError(FETCHX_ERROR_INVALID_ARGS);
      }

      const rawURL = readUTF8(urlPtr, urlLen);
      if (typeof rawURL !== "string" || rawURL.trim() === "") {
        return setFetchError(FETCHX_ERROR_INVALID_URL);
      }

      let resolvedURL;
      try {
        resolvedURL = new URL(rawURL, self.location?.href || "http://localhost/");
      } catch {
        return setFetchError(FETCHX_ERROR_INVALID_URL);
      }
      if (resolvedURL.protocol !== "http:" && resolvedURL.protocol !== "https:") {
        return setFetchError(FETCHX_ERROR_BAD_PROTOCOL);
      }

      const xhr = new XMLHttpRequest();
      const boundedTimeoutMs = Math.max(1, Math.min(toInt(timeoutMs, FETCHX_DEFAULT_TIMEOUT_MS), FETCHX_MAX_TIMEOUT_MS));
      try {
        xhr.open("GET", resolvedURL.toString(), false);
      } catch {
        return setFetchError(FETCHX_ERROR_REQUEST_FAILED);
      }

      try {
        xhr.timeout = boundedTimeoutMs;
      } catch {
        // Best-effort only: some runtimes may reject timeout on sync XHR.
      }

      try {
        xhr.send();
      } catch (error) {
        const text = String(error);
        if (text.includes("timeout")) {
          return setFetchError(FETCHX_ERROR_TIMEOUT);
        }
        return setFetchError(FETCHX_ERROR_REQUEST_FAILED);
      }

      fetchState.lastHTTPStatus = toInt(xhr.status, 0);
      if (fetchState.lastHTTPStatus < 200 || fetchState.lastHTTPStatus >= 300) {
        return setFetchError(FETCHX_ERROR_HTTP_STATUS, fetchState.lastHTTPStatus);
      }

      const encoded = textEncoder.encode(xhr.responseText ?? "");
      const maxWritable = Math.min(outputCap, FETCHX_MAX_RESPONSE_BYTES);
      if (encoded.length > maxWritable) {
        return setFetchError(FETCHX_ERROR_RESPONSE_TOO_LARGE, fetchState.lastHTTPStatus);
      }
      if (!writeBytes(outPtr, encoded)) {
        return setFetchError(FETCHX_ERROR_INVALID_MEMORY, fetchState.lastHTTPStatus);
      }
      return encoded.length;
    },
    fetchx_last_http_status: () => fetchState.lastHTTPStatus,
    fetchx_last_error_code: () => fetchState.lastErrorCode,
    fetchx_max_response_bytes: () => FETCHX_MAX_RESPONSE_BYTES,
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
    random_get: fillRandomBytes,
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

function toInt(value, fallback) {
  const num = Number(value);
  return Number.isFinite(num) ? Math.trunc(num) : fallback;
}
