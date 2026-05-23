# Synthetic Load - 500 Shards

Runs a load-testing workflow with 500 independent browser worker shards. Each
shard sleeps for `args.simulated_ms`, fills a payload buffer, and submits a
small result.

## DAG

```text
synthetic-shard (shard_count: 500, rep=1 consensus)
```

`shard_count: 500` expands into `synthetic-shard-0` through
`synthetic-shard-499`.

## Features

- High-width scheduler load with minimal computation.
- Tunable simulated delay and payload size.
- `replication_factor: 1` for predictable job counts.
- No reducer; each shard finalizes independently.

## Files

- [`synthetic-load-500.json`](synthetic-load-500.json) - DAG.
- [`synthetic_noop.cpp`](synthetic_noop.cpp) - browser worker program. Reads
  `args.simulated_ms` and `args.payload_bytes`, waits, fills a payload, and
  emits a small object.

## Activation

Open the admin panel, select `wf-synthetic-load-500`, and activate the
workflow. Enable reset state when you want a clean rerun.

## Notes

- `reward_usdc: "0.00"` keeps load tests from spending payer funds.
- The synthetic delay is approximate inside WASM. For API-level latency
  testing, use `--api-workers` in [`e2e/load-test.mjs`](../../../../e2e/load-test.mjs).
