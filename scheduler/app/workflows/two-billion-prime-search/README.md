# Two-Billion Prime Search

Counts primes from 1 to 2 000 000 000 by splitting the range into 200
browser worker shards. Each shard reports its own prime count as text.

## DAG

```text
prime-shard (shard_count: 200, rep=3 consensus)
```

`shard_count: 200` expands into `prime-shard-0` through
`prime-shard-199`. Each shard scans a 10 000 000-integer slice.

## Features

- High-width `shard_count` workflow without a reducer.
- Deterministic browser computation with `replication_factor: 3`.
- String output for each shard count.
- Segmented sieve implementation per shard.

## Files

- [`two-billion-prime-search.json`](two-billion-prime-search.json) - DAG.
- [`prime_shard.cpp`](prime_shard.cpp) - browser shard. Counts primes in
  `[1 + shard_index * range_size, 1 + (shard_index + 1) * range_size)`.

## Activation

Open the admin panel, select `wf-two-billion-prime-search`, and activate the
workflow. Enable reset state when you want a clean rerun.

Inspect an individual shard output:

```bash
curl "http://localhost:8080/api/workflow/node-output?workflow_id=wf-two-billion-prime-search&node_id=prime-shard-0"
```

Sum all 200 shard counts to get the total count.

## Notes

- Without a reducer, the final result is split across 200 node outputs.
- Raising `range_size` can push slower browser workers over the WASM timeout.
- Consensus rep=3 means total compute is three times the configured range.
