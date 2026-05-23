# Mini Text Consensus

Two browser worker nodes with one dependency edge. `emit-text` produces a
small text payload from its own `args`; `summarize-text` reads that output
and reports byte and token counts.

Both nodes use `replication_factor: 2` with `acceptance_policy: "consensus"`
so the engine waits until both replicas agree on the same `result_sig`.

This workflow is used by [`docker-compose.e2e.yml`](../../../../docker-compose.e2e.yml)
and `npm run workflow` in [`e2e/`](../../../../e2e/).

## DAG

```text
emit-text (rep=2, consensus)
    |
    v
summarize-text (rep=2, consensus)
```

## Features

- Simple browser-worker node authoring with `WORKFLOW_NODE(input, output)`.
- Node argument access with `input.int_("repeat", 8)`.
- Text output with `output.text(...)`.
- Upstream dependency access with `input.node("emit-text")`.

## Files

- [`mini-text-consensus.json`](mini-text-consensus.json) - DAG.
- [`emit_text.cpp`](emit_text.cpp) - producer. Reads `args.repeat` and emits
  a generated text string.
- [`summarize_text.cpp`](summarize_text.cpp) - consumer. Reads the upstream
  string and reports byte count plus token count.

## Activation

Open the admin panel, select `wf-mini-text-consensus`, and activate the
workflow. Enable reset state when you want a clean rerun.

Open the frontend and start two workers. Both nodes should finalize in a few
seconds.
