<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Command reference

Kive resolves the **bucket root** automatically: if **`BUCKET_ROOT`** is set, that absolute path is used; otherwise kive walks from the current directory upward until it finds **`kive.conf`** or **`data/kive.db`**. You can run commands from a subdirectory of the bucket. Use `kive <command> --help` for flags.

Configuration files: [configuration.md](configuration.md).

**Exclusive bucket lock:** Bucket-scoped CLI commands take a non-blocking exclusive flock on **`data/kive.lock`**. A second CLI on the same bucket fails with **`bucket busy: another kive operation is in progress`**. **`kive init`** and **`kive version`** do not hold this lock.

## Workflow commands

| Command | Summary |
|---------|---------|
| `kive init` | Create bucket (DB, workspace layout, CA, secrets) |
| `kive build` | Read workspace → update `kive.db`, KV, certs; run `post_build` hooks |
| `kive deploy` | Push jobs to workers, roll out, run deploy hooks |
| `kive health_check` | Worker SSH probe plus per-job health (manifest probes or commands) |
| `kive gc` | Purge removed allocations, worker data, old KV history |

## Inspect commands

| Command | Summary |
|---------|---------|
| `kive version` | Release tag, git hash, and platform (no bucket required) |
| `kive info` | Binary/build hashes, bucket ID, generation, counts |
| `kive cat workers` | Worker catalog |
| `kive cat jobs` | Job catalog |
| `kive cat allocations` | Job × worker rows (`--jobs`, `--workers`) |
| `kive cat deployments` | Allocation hashes, versions, rollout state |
| `kive cat hooks` | Commands from manifests |
| `kive cat ports` | Declared ports per job; `--listeners` for allocation endpoints |
| `kive cat certs` | TLS CA and leaf certs with expiry |
| `kive cat prometheus` | `_prometheus/` participation; `get`, `scrape` subcommands |
| `kive cat kv` | List KV keys (`--jobs`, `--active`, `--deleted`; or `kive cat kv get <ns> <key>`) |
| `kive logs show` | Filter structured bucket logs |

## Job control

| Command | Summary |
|---------|---------|
| `kive job start <job>` | `make start` via `runner.py` |
| `kive job stop <job>` | `make stop` |
| `kive job restart <job>` | `make restart` |
| `kive job run <job> --target <name>` | Arbitrary Makefile target |
| `kive job status <job>` | `make status` |
| `kive job create <job>` | Scaffold `workspace/jobs/<job>/` |

Common flags: `--allocations ip,...`, `--health_check` (start/stop/restart/run), `--forget` (stop only).

## Hooks and workers

| Command | Summary |
|---------|---------|
| `kive hooks <hook> [job] [-- <args...>]` | Run one manifest hook with event **`cli`** |
| `kive worker trust` | Pin SSH host keys for workers |
| `kive worker facts` | Probe workers; `--generate-workers` refreshes `workers.conf` |
| `kive worker uptime` | Print boot time / uptime per worker |
| `kive worker sysstat` | Short sar/mpstat/iostat/pidstat snapshot |
| `kive run_command "<shell>"` | Run a command on workers over SSH |

## `kive init`

```bash
kive init
```

Creates (first run) or upgrades (later runs):

- `data/kive.db` with the current schema
- `workspace/workers.conf`, `workspace/jobs/`, `workspace/bucket.conf`, `workspace/bucket.jobs.conf`
- `kive.conf` defaults — see [configuration.md](configuration.md)
- Bucket CA in `secrets/ca.crt` / `ca.key`
- KV encryption key `secrets/kv.key`
- `logs/` directory

Does not contact workers. Re-running **`kive init`** on a current schema refreshes layout without changing **`bucket_id`** or the CA.

## `kive build`

```bash
kive build [--delete-secrets-kv]
```

| Flag | Description |
|------|-------------|
| `--delete-secrets-kv` | Force delete `vars/job/<job>` and `secrets/job/<job>` for workspace jobs with **no active allocations** |

Reconciles the entire workspace. No filters. Does not SSH to workers (except hook scripts on the CLI host).

## `kive deploy`

```bash
kive deploy [--build] [--jobs j1,j2] [--dry-run] [--force]
```

| Flag | Description |
|------|-------------|
| `-b`, `--build` | Run `kive build` first |
| `--jobs` | Limit to named jobs (still respects deployment sequence) |
| `-n`, `--dry-run` | Stage locally and compare hashes; prints per-allocation actions without worker changes |
| `--force` | Redeploy even when all allocations are already promoted (skips pre-batch health; post-batch still required) |

After rsync, deploy applies **`restart_policy`** (`always` → `make restart`, `reload` → `make reload`, `never` → rsync + promote only). See [rolling deploy](../guide/rolling-deploy.md).

## `kive health_check`

```bash
kive health_check [--jobs j1,j2] [--wait] [--verbose]
```

| Flag | Description |
|------|-------------|
| `--jobs` | Limit to named jobs |
| `--wait` | Retry until pass. Manifest `health_check` with no `wait` uses **60** attempts at **2s**; otherwise `health.wait_seconds` (usually 180). See [health-check.md](health-check.md). |
| `--verbose` | Per-worker pass lines and stream health_check hook output |

Job.conf probes and wait defaults: [health-check.md](health-check.md).

## `kive job`

```bash
kive job start|stop|restart|status <job> [--allocations ip,...] [--health_check]
kive job stop <job> [--allocations ip,...] [--health_check] [--forget]
kive job run <job> --target start|stop|restart|reload|<make-target> [--allocations ip,...] [--health_check]
kive job create <job> [--selectors s1,s2]
kive job create <job> --runtime=compose|systemd [--selectors s1,s2]
kive job create <job> --job-template=<template>
```

**`--forget`** (stop only): after stop, clear promoted deploy state so the next deploy starts without pre-batch health; post-health still runs.

**`--runtime`** (`create` only) generates operator-owned lifecycle files. `compose` writes a Compose Makefile, `docker-compose.yml.tpl`, and `.dockerignore`; `systemd` writes a Makefile that renders and manages a system unit. `--runtime` and `--job-template` are mutually exclusive.

## `kive hooks`

```bash
kive hooks <hook_name> [job] [--verbose|--quiet] [-- <args...>]
```

Omit **job** to run on every catalog job that defines the command for **`cli`**. Batch width follows each job’s **`max_concurrent_restarts`**. Arguments after `--` are passed to the script.

Command must include **`cli`** in manifest `executed_on`. Script: `workspace/jobs/<job>/_hooks/hook_<name>.py` (or `.ts`/`.js`/`.rb`/`.sh`).

## `kive run_command`

```bash
kive run_command "<shell>" [-w ip,...] [-l label,...] [-c N] [--health_check]
```

Runs a shell command on selected workers over SSH.

## `kive worker`

```bash
kive worker trust [-w ip,...] [-l label,...] [-c N] [--force]
kive worker facts [-w ip,...] [-l label,...] [-c N] [--generate-workers] [--ignore-failure]
kive worker uptime [-w ip,...] [-l label,...] [-c N] [--ignore-failure]
kive worker sysstat [-w ip,...] [-l label,...] [-c N] [--ignore-failure]
```

Run **`worker trust`** after editing **`workers.conf`** to pin host keys in **`.ssh/known_hosts`** under the bucket. Use **`worker facts --generate-workers`** to update **`memory`** / **`cpu`** in **`workspace/workers.conf`**, then **`kive build`**.

## `kive gc`

```bash
kive gc [--retain-days N]
```

| Flag | Description |
|------|-------------|
| `--retain-days` | Override **`kv_retain_days`** from **`workspace/bucket.conf`** for this run. Use `0` for immediate purge. |

Run **`kive build`** after removing workers or jobs so allocations are marked `removed = 1`. **`kive deploy`** stops them and removes deployed job files. **`kive gc`** deletes remaining worker **`data/`** and **`logs/`** and the full **`jobs/<job>/`** tree.

## `kive info` and `kive cat`

```bash
kive version
kive info
kive cat workers
kive cat jobs
kive cat allocations [--jobs api] [--workers 10.0.0.1]
kive cat deployments [--jobs vault] [--workers 10.0.0.1]
kive cat hooks
kive cat ports [--jobs j1,j2]
kive cat ports --listeners [--jobs j1,j2] [--workers ip,...]
kive cat certs [--jobs api] [--workers 10.0.0.1]
kive cat prometheus [--jobs j1,j2]
kive cat kv
kive cat kv get kive/job/api job_name
kive logs show
```

## Recommended command order

```text
kive init
# hand-edit workspace
kive build
kive deploy              # or: kive deploy -b
kive health_check        # optional
# day-2: kive job *, kive hooks, kive worker facts, kive run_command, kive gc
```

See also: [health checks](health-check.md), [disable and drain](../guide/disable-and-drain.md), [rolling deploy](../guide/rolling-deploy.md), [First deploy](../tutorial/02-first-deploy.md).
