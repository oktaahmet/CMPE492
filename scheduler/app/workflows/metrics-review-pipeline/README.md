# Metrics Review Pipeline

Collects synthetic metrics in the browser, scores them with multiple browser
replicas, and builds a final server-side review decision.

## DAG

```text
collect-metrics (rep=2 consensus)
    |
    v
score-metrics (rep=3 collect_all)
    |
    v
build-review (server)
```

## Features

- Browser-worker execution for collection and scoring.
- Dependency passing through finalized node outputs.
- Deterministic consensus on `collect-metrics`.
- `collect_all` aggregation for multiple score samples.
- Server-side reduction for the final review.

## Files

- [`metrics-review-pipeline.json`](metrics-review-pipeline.json) - DAG.
- [`collect_metrics.cpp`](collect_metrics.cpp) - browser metrics producer.
- [`score_metrics.cpp`](score_metrics.cpp) - browser scorer run with
  `collect_all`.
- [`build_review.cpp`](build_review.cpp) - server reducer that emits the
  review decision.

## Activation

Open the admin panel, select `wf-metrics-review-pipeline`, and activate the
workflow. Enable reset state when you want a clean rerun.
