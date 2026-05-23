# Wikipedia Letter Prime Scan

Fetches three Wikipedia REST pages, counts configured letters, generates a
number artifact, scans odd and even lines for primes, and writes a final
artifact with the merged prime list.

## DAG

```text
A: count_letter_page(Turkey, a)
B: count_letter_page(Russia, e)  --> D: random_threshold
C: count_letter_page(USA, i)             |
                                         v
                              E: write_numbers (server, artifact: numbers)
                                         |
                              +----------+----------+
                              |                     |
                     G: scan_prime_lines(odd)   H: scan_prime_lines(even)
                              |                     |
                              +----------+----------+
                                         |
                                         v
                              K: merge_primes (server, artifact: final)
```

## Features

- Parameterized browser HTTP fetch nodes using the same C++ program.
- Random threshold generation under `collect_all`.
- Server-side artifact writing with `null` node output.
- Browser artifact consumers that split work by line parity.
- Final server reducer that writes the merged output artifact.

## Files

- [`wikipedia-letter-prime-scan.json`](wikipedia-letter-prime-scan.json) - DAG.
- [`count_letter_page.cpp`](count_letter_page.cpp) - browser fetch and letter
  count program used by A, B, and C.
- [`d_random_threshold.cpp`](d_random_threshold.cpp) - random threshold node.
- [`e_write_numbers.cpp`](e_write_numbers.cpp) - server writer for
  `numbers.txt`.
- [`scan_prime_lines.cpp`](scan_prime_lines.cpp) - browser scanner for odd or
  even artifact lines.
- [`k_merge_primes.cpp`](k_merge_primes.cpp) - server reducer and final
  artifact writer.

## Activation

Open the admin panel, select `wf-wikipedia-letter-prime-scan`, and activate
the workflow. Enable reset state when you want a clean rerun.

## Notes

- Node D uses `collect_all` because its output is random.
- Nodes E and K return JSON `null`; their useful payload is written as an
  output artifact.
- Browser fetch depends on public endpoint availability and CORS behavior.
