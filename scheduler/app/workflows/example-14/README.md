# Example 14: Composed Metrics Review

This workflow is a compact end-to-end recipe that combines the platform pieces already used elsewhere in the repo:

- browser-worker execution
- dependency passing through `inputs`
- consensus replication
- `collect_all` aggregation
- server-side reduction

Flow:

1. `collect-metrics` produces a deterministic metrics object in the browser.
2. `score-metrics` consumes that object, runs three browser replicas, and keeps all accepted samples.
3. `build-review` runs on the scheduler server and turns the collected samples into a final review decision.

It is intentionally small enough to read in one sitting, but it exercises the same path a real multi-step workflow uses.
