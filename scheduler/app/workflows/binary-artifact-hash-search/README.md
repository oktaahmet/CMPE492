# Binary Artifact Hash Search

Searches a binary input artifact for matching hash candidates. Four browser
worker shards scan non-overlapping ranges, then a server reducer deduplicates
the matches and writes a downloadable artifact.

## DAG

```text
numbers.bin
    |
    +-- hash-shard-0 --+
    +-- hash-shard-1 --+--> merge-hash-matches (server, artifact: matches)
    +-- hash-shard-2 --+
    +-- hash-shard-3 --+
```

Each `hash-shard-N` reads the same `numbers.bin` artifact by URL on the
browser side and processes a non-overlapping range.

## Features

- Workflow-level binary input artifact: `numbers-bin`.
- Browser artifact fetch with `fetch_bytes`.
- Four explicit shard nodes with separate `range_start` / `range_count`
  arguments.
- Server-side reduction with `output_artifacts.matches`.

## Files

- [`binary-artifact-hash-search.json`](binary-artifact-hash-search.json) - DAG.
- [`data/numbers.bin`](data/numbers.bin) - binary input artifact.
- [`hash_shard.cpp`](hash_shard.cpp) - browser shard. Fetches the binary
  artifact, scans its assigned range, and emits matches.
- [`merge_matches.cpp`](merge_matches.cpp) - server reducer. Walks every
  `hash-shard-*` output, deduplicates matches, and writes `matches.txt`.

## Activation

Open the admin panel, select `wf-binary-artifact-hash-search`, and activate
the workflow. Enable reset state when you want a clean rerun.

When the workflow finalizes, the `matches.txt` file is available at:

```text
GET /api/workflow/artifact?workflow_id=wf-binary-artifact-hash-search&node_id=merge-hash-matches&artifact_id=matches
```

The exact URL also appears in `merge-hash-matches` finalized output under
`artifacts.matches.url`.

## Notes

- `fetch_bytes` needs a buffer large enough for the whole artifact body.
  If `numbers.bin` grows beyond about 16 MB, the browser fetch fails with
  `FETCH_RESPONSE_TOO_LARGE`.
- The shards are explicit JSON nodes. A templated version can use
  `shard_count: 4` and read `shard_index` in the shard program.
