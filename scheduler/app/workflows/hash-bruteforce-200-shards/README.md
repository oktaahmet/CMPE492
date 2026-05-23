# Hash Bruteforce - 200 Shards

Searches for an integer `n` whose ASCII-decimal SHA-256 hash equals a fixed
target. The workflow expands one browser worker node into 200 shards, then a
server reducer returns the matching value.

## DAG

```text
bruteforce-shard (shard_count: 200, rep=3 consensus)
    |
    v
bruteforce-result (server)
```

The `shard_count: 200` field expands `bruteforce-shard` at load time into
`bruteforce-shard-0` through `bruteforce-shard-199`, each running the same
program with a different `shard_index` injected into `args[0]`.

## Features

- `shard_count: 200` expands one node definition into parallel work.
- Each shard derives its scan range from `shard_index` and `range_size`.
- `replication_factor: 3` with `consensus` validates deterministic shard
  results before acceptance.
- The server reducer walks expanded shard ids and returns the winning nonce.

## Files

- [`hash-bruteforce-200-shards.json`](hash-bruteforce-200-shards.json) -
  DAG with `shard_count: 200`.
- [`bruteforce_shard.cpp`](bruteforce_shard.cpp) - browser shard. Reads
  `shard_index`, `range_size`, and `target_hash`; scans its slice; reports
  `{ matched, value, hash }`.
- [`bruteforce_reduce.cpp`](bruteforce_reduce.cpp) - server reducer. Walks
  every expanded shard, picks the matching one, and emits the final answer.

## Activation

Open the admin panel, select `wf-hash-bruteforce-200-shards`, and activate
the workflow. Enable reset state when you want a clean rerun.

Each shard scans 1 544 579 integers. The configured total search space is
about 308 M integers.

## Notes

- `range_size * shard_count` defines the total search space. If the match is
  outside that range, every shard finalizes with `matched: 0` and the reducer
  reports "not found".
- Consensus rep=3 means three workers must independently compute the same
  hash result for the same shard range.
- The reducer walks `bruteforce-shard-i` until it hits a gap, so `shard_count`
  can change without updating the reducer.
