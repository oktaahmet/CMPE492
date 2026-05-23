# Distributed TSP

Runs a parallel random-search Traveling Salesman workload over 16 Turkish
cities. The workflow reads a city CSV artifact, plans the search, runs eight
independent browser worker shards, verifies the best tour, and writes a final
solution artifact.

## DAG

```text
fetch-tuning-context   read-cities
        \              /
         \            /
          plan-search
              |
  +-- search-shard-0 --+
  +-- search-shard-1 --+
  +-- search-shard-2 --+
  +-- search-shard-3 --+
  +-- search-shard-4 --+--> verify-best (rep=3 consensus)
  +-- search-shard-5 --+
  +-- search-shard-6 --+
  +-- search-shard-7 --+
                         |
                         v
          write-solution (server, artifact: solution)
```

The search phase is manually sharded with `search-shard-0` through
`search-shard-7`. Each shard runs the same program with a different shard
index and `replication_factor: 1`.

## Features

- Browser HTTP fetch for tuning context.
- Server-side input artifact reads for `cities.csv`.
- Browser artifact fetch from each search shard.
- Priority metadata on every node.
- Consensus validation for deterministic verification.
- Server-side output artifact for the final tour.

## Files

- [`distributed-tsp.json`](distributed-tsp.json) - DAG.
- [`data/cities.csv`](data/cities.csv) - city input artifact.
- [`fetch_tuning_context.cpp`](fetch_tuning_context.cpp) - browser fetch node.
- [`read_cities.cpp`](read_cities.cpp) - server node that reads the city CSV.
- [`plan_search.cpp`](plan_search.cpp) - server node that prepares shard
  settings.
- [`search_shard.cpp`](search_shard.cpp) - browser shard program.
- [`verify_best.cpp`](verify_best.cpp) - browser verifier for the best tour.
- [`write_solution.cpp`](write_solution.cpp) - server writer for the final
  solution artifact.

## Activation

Open the admin panel, select `wf-distributed-tsp`, and activate the workflow.
Enable reset state when you want a clean rerun. Select priority-aware topology
if ready-node ordering should follow the configured priority values.

## Notes

- `restarts_per_shard` is set on `plan-search`. Raising it increases work per
  shard and should stay below the browser worker timeout.
- Add more `search-shard-N` nodes to scale the search width. Update the
  downstream `depends_on` arrays when changing the shard list.
- Eight online workers can run the default search phase in parallel. Fewer
  workers process the shards sequentially or in smaller batches.
