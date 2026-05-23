# Queue Simulation Capability Workflow

Runs a queue simulation DAG that combines browser workers, server reducers,
input artifacts, HTTP fetch, random browser output, priority metadata, and
output artifacts.

## DAG

```text
fetch-public-signal   read-load-profile
        \              /
         \            /
      simulate-baseline   simulate-stress   generate-audit-trace
              \             |                /
               \            |               /
                aggregate-simulations (server, artifact: simulation-report)
                         |
                         v
                    score-policy
                         |
                         v
                final-brief (server, artifact: final-brief)
```

Use `topology_mode: "priority_aware"` during activation if configured
priority values should affect ready-node ordering.

## Features

- Browser HTTP fetch.
- Browser fetch of the `load-profile` CSV input artifact.
- Random browser work under `collect_all`.
- Large worker output for chunked dependency loading.
- Server-side aggregation and output artifact writing.
- Final numeric scoring and server-generated brief.

## Files

- [`queue-simulation-capability.json`](queue-simulation-capability.json) - DAG.
- [`data/load_profile.csv`](data/load_profile.csv) - input load profile.
- [`fetch_signal.cpp`](fetch_signal.cpp) - browser HTTP fetch node.
- [`read_load_profile.cpp`](read_load_profile.cpp) - browser artifact reader.
- [`simulate_queue.cpp`](simulate_queue.cpp) - browser simulation node used by
  baseline and stress branches.
- [`generate_large_trace.cpp`](generate_large_trace.cpp) - browser node that
  emits a large trace payload.
- [`aggregate_runs.cpp`](aggregate_runs.cpp) - server reducer and report writer.
- [`score_policy.cpp`](score_policy.cpp) - browser scoring node.
- [`final_brief.cpp`](final_brief.cpp) - server node that writes the final
  brief artifact.

## Activation

Open the admin panel, select `wf-queue-simulation-capability`, and activate
the workflow. Enable reset state when you want a clean rerun. Select
priority-aware topology if ready-node ordering should follow configured
priority values.

## Notes

- Priority values are metadata unless `topology_mode` is `priority_aware`.
- Random browser output should use `collect_all`; random output with
  `consensus` usually fails quorum.
- Browser-worker output artifacts are not supported in this workflow shape;
  server nodes write the artifacts.
