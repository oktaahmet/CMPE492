# Prime Range Analysis DAG

Runs a browser-only multi-stage prime analysis. Four range nodes fan out into
per-range gap and statistics nodes, then two reducers feed a final browser
summary node.

## DAG

```text
range-a  range-b  range-c  range-d
   |        |        |        |
   +--------+--------+--------+
   |        |        |        |
gaps-a   gaps-b   gaps-c   gaps-d
stats-a  stats-b  stats-c  stats-d
   |        |        |        |
   +--------+--------+--------+
            |
      reduce-gaps   reduce-stats
              \       /
               v     v
             final-report
```

All nodes run on browser workers with `replication_factor: 2` and
`acceptance_policy: "consensus"`.

## Features

- Browser-only fan-out and fan-in DAG.
- Deterministic consensus at every stage.
- Numeric node outputs through `output.number(...)`.
- Separate reducers for gap and statistics branches.

## Files

- [`prime-range-analysis-dag.json`](prime-range-analysis-dag.json) - DAG.
- [`prime.cpp`](prime.cpp) - counts primes in `[args.start, args.end]`.
- [`gaps.cpp`](gaps.cpp) - computes the max prime gap in the same range.
- [`stats.cpp`](stats.cpp) - computes a statistical summary for the range.
- [`reduce_gaps.cpp`](reduce_gaps.cpp) - combines all `gaps-*` outputs.
- [`reduce_stats.cpp`](reduce_stats.cpp) - combines all `stats-*` outputs.
- [`finalize.cpp`](finalize.cpp) - reads both reducers and emits the final
  number.

## Activation

Open the admin panel, select `wf-prime-range-analysis-dag`, and activate the
workflow. Enable reset state when you want a clean rerun.

Plan for at least two browser workers so consensus nodes can reach quorum.
With four or more workers, the parallel layers run at full width.

## Notes

- The per-range computations are deterministic, so `consensus` is suitable.
- Every accepted worker receives the configured node reward. Check the total
  reward budget before increasing replication or adding nodes.
