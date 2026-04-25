# Example 15: Full System Capability Workflow

This workflow is intentionally larger than the other examples. It is a single DAG that exercises the scheduler features that are easy to forget when reading isolated examples:

- priority-aware ready-node ordering
- browser-worker nodes
- server-side native nodes
- node-specific `replication_factor`
- `consensus` and `collect_all`
- browser HTTP fetch
- browser artifact fetch of a CSV input file
- browser random data
- large worker output that forces chunked dependency loading
- server-side output artifacts
- simple number output
- server reduction after `collect_all`

Use `topology_mode: "priority_aware"` when activating this workflow if you want the priority values to affect ready-node order.

## Boundaries This Example Makes Visible

- Priority values are metadata unless the active topology mode is `priority_aware`.
- Random browser work should use `collect_all`; random output with `consensus` will usually fail to reach quorum.
- Server nodes currently run with `replication_factor: 1`.
- Browser-worker output artifacts are not supported yet; only server nodes can write `output_artifacts`.
- `result_schema` currently validates the top-level payload shape, not deep object fields.
- Server reducers receive full dependency payloads; browser reducers can use chunked dependency loading.
