<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 3. The model

**Goal:** Name the four core ideas from your first deploy and map each to a file or command.

**Prerequisites:** [Chapter 2 — First deploy](02-first-deploy.md).

## Four terms

| Term | One line | Your `hello` example |
|------|----------|----------------------|
| **Bucket** | One kive project directory | `my-cluster/` where you ran `kive init` |
| **Worker** | A cluster node — SSH target | `10.0.0.1` in `workspace/workers.conf` |
| **Job** | A deployable unit | `hello` under `workspace/jobs/hello/` |
| **Allocation** | One job on one worker | `hello` @ `10.0.0.1` (created at build) |

```text
workers.conf ──┐
               ├── labels match selectors? ──yes──► allocation: hello @ 10.0.0.1
hello/job.conf─┘                         └──no───► no allocation
```

Kive creates **allocations** automatically during **`kive build`** when a job's **selectors** match a worker's **labels**. Your `hello` job used `selectors("worker")`; every worker gets the `worker` label automatically.

## Bucket vs worker

| | Bucket (CLI host) | Worker |
|---|-------------------|--------|
| **You edit** | `workspace/`, `kive.conf` | Nothing in git — files arrive via deploy |
| **Kive writes** | `data/kive.db`, `tmp/` (scratch, removed after each command) | `/opt/kive/<bucket_id>/jobs/hello/` |
| **Commands** | `kive build`, `kive deploy` | `make start` (called by deploy over SSH) |

The bucket is the **source of truth** for what *should* run. Workers hold **runtime** copies and `data/` that persists across deploys.

## Job lifecycle (simple view)

| Event | What kive does on the worker |
|-------|------------------------------|
| First deploy to allocation | rsync files → **`make start`** |
| You change files and deploy again | rsync → **`make restart`** (default) |
| You remove job from workspace + build + deploy | stop + remove deployed files |

Your Makefile defines what `start`, `stop`, and `restart` mean.

## Inspect allocations

```bash
kive cat allocations
kive cat allocations --jobs hello --workers 10.0.0.1
```

Columns **`disabled`** and **`removed`** matter later for drain and cleanup. Both `0` means **active**.

## What you learned

- **Bucket**, **worker**, **job**, and **allocation** are the core model
- Build creates allocations by matching job selectors to worker labels
- Deploy rsyncs a job tree and runs Makefile targets on each allocation

## Next

[Chapter 4 — Project files](04-project-files.md): which paths you edit in git.
