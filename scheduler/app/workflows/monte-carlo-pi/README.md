# Monte Carlo Pi

Estimates pi by Monte Carlo sampling. Three browser replicas each draw 20 M
random points; a server node averages their independent estimates.

[`e2e/benchmark.mjs`](../../../../e2e/benchmark.mjs) and
[`e2e/load-test.mjs`](../../../../e2e/load-test.mjs) use this workflow by
default.

## DAG

```text
pi-worker (rep=3, collect_all)
    |
    v
pi-average (server, output artifact: pi-report)
```

## Features

- `collect_all` keeps independent random replica outputs for aggregation.
- The reducer reads all replica values with `obj.numbers(...)`.
- Browser WASM random generation uses `workflow::random_seed()` and
  `workflow::random_u32()`.
- The server reducer writes a text report through `output_artifacts`.

## Files

- [`monte-carlo-pi.json`](monte-carlo-pi.json) - DAG.
- [`monte_carlo_pi.cpp`](monte_carlo_pi.cpp) - browser worker. Seeds a PRNG,
  runs the inside-the-circle count, and emits
  `{ samples_inside, samples_total, estimate_micro }`.
- [`pi_average.cpp`](pi_average.cpp) - server reducer. Reads every replica,
  computes the aggregate estimate, and writes the report text.

## Activation

Open the admin panel, select `wf-monte-carlo-pi`, and activate the workflow.
Enable reset state when you want a clean rerun.

## Notes

- The default 20 M samples per replica runs under the 45 s WASM worker
  ceiling on modern laptops. If you raise it, monitor the worker Logs panel.
- With one browser worker online, the engine runs all three replicas
  sequentially.
