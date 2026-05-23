# Wikipedia API Fetch

Runs a single browser worker node that fetches a CORS-enabled Wikipedia JSON
endpoint and reports basic response metrics.

See [HTTP_GET_GUIDE.md](../HTTP_GET_GUIDE.md) for the browser HTTP fetch
reference.

## DAG

```text
analyze-wikipedia-api (rep=2 consensus)
```

## Features

- Browser-side public HTTP GET from WASM.
- Fetch through `runtime_browser::get` / `workflow::fetch_text`.
- Deterministic response validation with `replication_factor: 2` and
  `consensus`.
- Basic response metadata output: `ok`, `http_status`, `bytes`, and related
  metrics.

## Files

- [`wikipedia-api-fetch.json`](wikipedia-api-fetch.json) - DAG.
- [`analyze_wikipedia_api.cpp`](analyze_wikipedia_api.cpp) - browser node
  that fetches the endpoint and emits response metrics.

## Activation

Open the admin panel, select `wf-wikipedia-api-fetch`, and activate the
workflow. Enable reset state when you want a clean rerun.

## Notes

- Browser fetch is limited to GET requests, a 10 000 ms timeout cap, a 16 MB
  response cap, and CORS-friendly endpoints.
- If the network blocks Wikipedia, the node returns `ok: false`,
  `http_status: 0`, and `error_code: FETCH_REQUEST_FAILED`.
- The endpoint URL is hardcoded in the C++ file. To make it configurable,
  pass a URL through node `args` and read it with `input.string("url")`.
