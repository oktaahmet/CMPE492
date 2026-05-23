# Prime Gap - 30 Billion

Searches for the largest prime gap inside the first 30 billion integers. The
range is split into 30 browser worker shards of 1 billion numbers each, then
a server reducer selects the global maximum.

## DAG

```text
prime-gap-shard (shard_count: 30, rep=1 consensus)
    |
    v
prime-gap-result (server)
```

`shard_count: 30` expands into `prime-gap-shard-0` through
`prime-gap-shard-29`. Each shard scans from `shard_index * range_size`.

## Features

- `shard_count: 30` for large parallel range scanning.
- `replication_factor: 1` for lower compute cost in local runs.
- Segmented sieve logic inside the browser worker.
- Server reducer validates shard coverage and selects the maximum gap.

## Files

- [`prime-gap-30b.json`](prime-gap-30b.json) - DAG with `shard_count: 30`.
- [`prime_gap_shard.cpp`](prime_gap_shard.cpp) - browser shard. Runs a
  segmented sieve and emits
  `{ shard_index, max_gap, gap_low, gap_high, primes_seen }`.
- [`prime_gap_reduce.cpp`](prime_gap_reduce.cpp) - server reducer. Walks all
  configured shards and emits the global maximum gap.

## Activation

Open the admin panel, select `wf-prime-gap-30b`, and activate the workflow.
Enable reset state when you want a clean rerun.

## Notes

- At 1 B integers per shard, this is a heavy browser compute workload.
  Lower `range_size` if workers exceed the WASM timeout.
- `args.limit` should equal `shard_count * range_size` for a complete range.
- `replication_factor: 1` provides no Byzantine protection. Raise it for
  stronger validation at the cost of more compute.
