<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Rolling deploy

Roll out job changes **one batch of workers at a time** using **`kive deploy`**, manifest batch fields, and optional health checks between batches.

## When to use

Use this guide when a job runs on **more than one worker** and you want upgrades (or first-time starts) to happen in **controlled batches** instead of all allocations at once.

Kive handles rolling during **`kive deploy`**. For host OS reboots, see [worker reboot](worker-reboot.md).

At least one successful deploy of the job is assumed.

## How kive batches rollouts

During **`kive deploy`**, kive groups allocations into batches, runs lifecycle on each batch, **promotes** that batch, then runs **health checks** before the next batch (when configured).

| Field | Default | Meaning |
|-------|---------|---------|
| **`max_concurrent_starts`** | `0` (all) | First deploy (`make start`): at most N new allocations per batch |
| **`max_concurrent_restarts`** | `1` | Upgrades (`make restart` / `reload`): at most N allocations per batch |
| **`max_concurrent_stops`** | `0` (all) | Stop reconcile (`make stop`): at most N allocations per batch |

`0` for starts/stops still means all-at-once **subject to** bucket **`deploy.max_concurrent_syncs`** (default **16**), which hard-caps parallel SSH for start/stop/upgrade batches the same way fleet sync is capped.

Within each batch, worker order comes from **`rollout_order`** in job KV (`kive/job/<job>/rollout_order`), set at build from the catalog. Override per deploy in a **`pre_deploy`** hook with **`put_rollout_order()`**.

## Step 1 — Set batch size in the manifest

Example job on four workers — upgrade two at a time:

```text
  version("2.0.0");
  selectors("worker");
  max_concurrent_starts(2);
  max_concurrent_restarts(2);
  max_concurrent_stops(1);
  restart_policy("always");
```

| Value | Typical use |
|-------|-------------|
| **`max_concurrent_restarts: 1`** | One worker at a time during upgrades |
| **`max_concurrent_restarts: 2`** or higher | Parallel upgrades when the job tolerates it |
| **`max_concurrent_starts: 1`** | Bring up new allocations one by one on first deploy |
| **`max_concurrent_stops: 1`** | Drain / undeploy one allocation at a time |

After editing: **`kive build`**.

## Step 2 — Choose how upgrades apply files

Set **`restart_policy`** in the same manifest:

| Policy | After rsync on upgrade |
|--------|-------------------------|
| **`always`** (default) | **`make restart`** |
| **`reload`** | **`make reload`**, or **`make restart`** when a changed file matches **`restart_globs`** |
| **`never`** | Rsync and promote only — no Makefile lifecycle |

Your Makefile must define the targets deploy calls (`start`, `restart`, `reload` as needed).

## Step 3 — Plan the rollout

```bash
kive build
kive deploy --dry-run
```

Dry-run lists each allocation action: **start**, **restart**, **reload**, **sync**, or **skip**.

Inspect version and hash state:

```bash
kive cat deployments --jobs hello
kive cat allocations --jobs hello
```

## Step 4 — Deploy

```bash
kive deploy --jobs hello
```

Example with **`max_concurrent_restarts: 2`** on workers `10.0.0.1` … `10.0.0.4`:

```text
Batch 1: restart 10.0.0.1 and 10.0.0.2 → promote → health_check
Batch 2: restart 10.0.0.3 and 10.0.0.4 → promote → health_check
```

Health gates run when the manifest defines probes or a **`health_check`** command.

## Step 5 — Control order (optional)

To restart followers before a specific worker in one deploy, set order in **`pre_deploy`**:

```python
put_rollout_order(["10.0.0.2", "10.0.0.3", "10.0.0.1"])
```

Register the hook in **`job.conf`** under **`hooks`**.

## Makefile version variables

On **`start`**, **`restart`**, and **`reload`**, kive passes:

```text
CURRENT_VERSION=<running on this allocation>
NEW_VERSION=<target from manifest after build>
```

Use them in the Makefile when upgrade scripts need the release id:

```makefile
restart:
	./bin/upgrade.sh "$(CURRENT_VERSION)" "$(NEW_VERSION)"
	$(MAKE) start
```

## Multi-job order

When job A **depends** on job B (manifest **demands**), **`kive build`** assigns **`deployment_seq`**. Deploy runs lower sequence values first; each job still uses its own batch fields.

```bash
kive cat jobs
kive deploy
```

## Unhealthy recovery

A cancelled or failed post-batch health gate marks the in-flight batch **`health_failed`**. The next **`kive deploy`** retries those allocations (restart) without **`--force`**; pre-batch health skips them so the still-unhealthy containers do not block the retry.

If you need a clean **start** instead of restart (or pre-batch health on *other* already-promoted allocations blocks the roll), clear promoted state then redeploy:

```bash
kive job stop hello --forget --allocations 10.0.0.2
kive deploy --jobs hello
```

**`--forget`** makes the next deploy treat those allocations as **start**-pending and skips **pre-batch** health on them; **post-batch** health still runs. **`kive deploy --force`** also skips **pre-batch** health (without stopping) while still requiring **post-batch** health.

## Manual restart (no workspace change)

When the catalog is already current but processes need a bounce:

```bash
kive job restart hello --allocations 10.0.0.1 --health_check
kive job restart hello --allocations 10.0.0.2 --health_check
```

**`kive job`** does not batch across allocations — use **`--allocations`** per worker, or use **`kive deploy`** with **`max_concurrent_restarts`** when rolling out file or version changes.

```bash
kive job run hello --target reload --allocations 10.0.0.1
```

Requires a prior deploy (`generation` in sync on workers).

## Verify

```bash
kive cat deployments --jobs hello
kive health_check --jobs hello --wait
kive job status hello
```

During deploy, **`applied_version`** may differ across workers until each batch promotes.

## Related

- [Guide: disable and drain](disable-and-drain.md) — remove a worker from batches temporarily
- [Worker reboot](worker-reboot.md) — host OS reboot pattern
