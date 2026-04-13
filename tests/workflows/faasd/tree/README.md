# Tree Function Workflow (Entry = `A`)

This document summarizes the workflow implemented under `tree/`, a tree-shaped call graph that exercises both synchronous and asynchronous inter-function invocation patterns.

## Functions

- **Workflow steps:** `tree/*/handler.js`
  - `A` — entry point, fans out to `B` (sync) and `C` (async)
  - `B` — intermediate; calls `D` and `E` synchronously
  - `C` — intermediate; calls `F` and `G` asynchronously while running CPU work in worker threads
  - `D`, `E` — leaf functions; simulate I/O latency (500ms sleep)
  - `F`, `G` — leaf functions; simulate CPU-intensive work via worker threads

## High-Level Call Graph

Legend:

- `sync` means the caller awaits a result.
- `async` means fire-and-forget / non-blocking invocation.

```text
A (entry)
  |-sync->  B
  |          |-sync-> D (leaf, 500ms sleep)
  |          |-sync-> E (leaf, 500ms sleep)
  |          |-500ms sleep
  |-async-> C
             |-async-> F (leaf, 2x CPU worker threads, base=7)
             |-async-> G (leaf, 2x CPU worker threads, base=8.8)
             |-cpu work (2x worker threads, base=7)
```

## Function Details

| Fn | Role | Calls | CPU Work | I/O Simulation |
|----|------|-------|----------|----------------|
| **A** | Entry | B (sync), C (async) | - | - |
| **B** | Intermediate | D (sync), E (sync) | - | 500ms sleep |
| **C** | Intermediate | F (async), G (async) | 2 worker threads | - |
| **D** | Leaf | - | - | 500ms sleep |
| **E** | Leaf | - | - | 500ms sleep |
| **F** | Leaf | - | 2 worker threads (base 7) | - |
| **G** | Leaf | - | 2 worker threads (base 8.8) | - |

## Setup

No external dependencies (no database, no secrets). No `.env.yaml` required.

### Deploy

```bash
faas-cli deploy --platform faasd -f ./tests/workflows/faasd/tree/stack.yaml
```

### Invoke

```bash
curl -X POST http://faasd.com/function/tree-a \
  -H 'Content-Type: application/json' \
  -d '{}'
```

## Notes

- All CPU simulation uses `Math.atan(i) * Math.tan(i)` loops in `worker_threads`.
- G performs significantly more work than F due to the higher base (`8.8^7 ≈ 40M` vs `7^7 ≈ 823K` iterations).
- The sync branch (`A → B → D/E`) and async branch (`A → C → F/G`) execute concurrently from A's perspective.
