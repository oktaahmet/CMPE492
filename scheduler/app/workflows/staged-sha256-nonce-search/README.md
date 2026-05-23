# Staged SHA-256 Nonce Search

Runs three sequential rounds of partial-collision SHA-256 nonce search. Each
round starts three browser search nodes, then a server verifier selects the
best candidate and passes it to the next round.

The final verifier writes a cumulative nonce report artifact.

## DAG

```text
round-1-a  round-1-b  round-1-c
     \        |        /
      \       |       /
       verify-round-1
             |
round-2-a  round-2-b  round-2-c
     \        |        /
      \       |       /
       verify-round-2
             |
round-3-a  round-3-b  round-3-c
     \        |        /
      \       |       /
       verify-round-3 (server, artifact: nonce-report)
```

## Features

- Three-stage browser search with server verification between stages.
- `collect_all` for stochastic search branches.
- Shared browser program for all nine search nodes.
- Per-round difficulty through `target_bits`.
- Final server-side output artifact.

## Files

- [`staged-sha256-nonce-search.json`](staged-sha256-nonce-search.json) -
  three-stage DAG.
- [`sha256_nonce_search.cpp`](sha256_nonce_search.cpp) - browser search
  program reused by every `round-N-*` node.
- [`verify_nonce_round.cpp`](verify_nonce_round.cpp) - server verifier used by
  all `verify-round-N` nodes. It selects the best candidate and writes the
  report when `args.write_report == 1`.

## Activation

Open the admin panel, select `wf-staged-sha256-nonce-search`, and activate
the workflow. Enable reset state when you want a clean rerun.

Plan for at least three browser workers so each round can run its search
nodes in parallel.

## Notes

- Search nodes use `replication_factor: 1`; parallelism comes from sibling
  nodes with different start offsets.
- Round 3 uses `target_bits: 18`. Increase `attempts` if strict matches are
  required instead of accepting the best candidate found within the budget.
