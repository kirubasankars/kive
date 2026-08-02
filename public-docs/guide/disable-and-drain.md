<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Disabling workers, jobs, and allocations

Use **`workspace/disabled.conf`** for **maintenance** — drain a node, allocation, or entire job **without** removing workspace files, catalog rows, or worker **`data/`** / **`logs/`**.

A disabled allocation is otherwise the same as an active one: it stays in **`allocations`**, build refreshes job and per-allocation KV and certs, and deploy **stages, hashes, and promotes** content. The **only** difference is runtime: kive **never starts** a disabled allocation (no lifecycle — start, restart, reload — and no rsync on deploy).

## `disabled.conf` format

File path: **`workspace/disabled.conf`** (optional; missing file means nothing disabled).

```text
disabled {
  job api { };
  job vault { allocations("10.0.0.2"); };
  workers("10.0.0.3");
};
```

| Key | Effect |
|-----|--------|
| **`job <name> { }`** (empty body) | Disable **every** allocation for that job (all workers). |
| **`job <name> { allocations(...); }`** | Disable only those worker IPs for that job. |
| **`workers`** | Disable **every** job on those worker IPs. |

Rules:

- Job and worker entries can be combined in one file.
- A worker listed under **`workers`** disables all its allocations, even if not listed under a **`job`** block.
- Edit **`disabled.conf`**, then always run **`kive build`** so `allocations.disabled` updates in `kive.db`.

## How to disable

### One allocation (job on one worker)

Drain **`api`** on **`10.0.0.2`** only:

```text
disabled {
  job api { allocations("10.0.0.2"); };
};
```

```bash
kive build
kive deploy
```

Deploy **stops** the job on that worker on every deploy (reconcile), then may promote catalog hashes without starting it. Other workers keep running **`api`**.

### Entire job (all workers)

```text
disabled {
  job api { };
};
```

```bash
kive build
kive deploy
```

Every **`api`** allocation is stopped; deploy skips lifecycle and rsync for **`api`**.

### Entire worker (all jobs on a host)

```text
disabled {
  workers("10.0.0.3");
};
```

```bash
kive build
kive deploy
```

Use this before host maintenance: drain all jobs on the machine without deleting job definitions.

### Combine job and worker rules

```text
disabled {
  job vault { allocations("10.0.0.1"); };
  workers("10.0.0.4");
};
```

## What happens on build and deploy

| Phase | Disabled allocation |
|-------|---------------------|
| **`kive build`** | Sets `allocations.disabled = 1`. Same job KV, per-allocation KV, and certs as active peers. Re-enable clears the flag when the entry is removed from `disabled.conf`. |
| **`kive deploy` reconcile** | **Stop on every deploy** (`make stop`), including when never promoted. Stop runs **before** any later hash/version promote. **Keep** deploy artifacts, **`data/`**, **`logs/`**, hash row, and KV. |
| **Staging / hash / promote** | Same as active — content and **`version`** stay current. |
| **Lifecycle / rsync** | **Skipped** (no start, restart, reload, or rsync). |
| **Hooks** (`post_build`, `pre_deploy`, `post_deploy`, `cli`) | **Run** on disabled allocations. **`health_check`** commands and probes skip disabled. |
| **`kive job`** (`start`, `stop`, `restart`, `run`, `status`) | **Skipped** on disabled allocations; a fully disabled job errors; **`--allocations <disabled-ip>`** errors (worker drain included). |

Inspect state:

```bash
kive cat allocations --jobs api
kive cat deployments --jobs api
kive deploy --dry-run
```

After disable, **`kive deploy --dry-run`** should show **`stop`** on disabled allocations (including never-promoted rows). If build changed content or version while disabled, dry-run shows **`stop+promote`** (`disabled_restart` in **`cat deployments`**) — deploy stops first, then stages and promotes without starting the allocation.

**`cat deployments` rollout** for disabled rows:

| Rollout | Meaning |
|---------|---------|
| `disabled` | Stopped; hashes and versions match (promoted). |
| `disabled_restart` | Stopped; content or version changed since last promote — deploy updated the plan but did not restart. |

## How to re-enable

1. Remove the job, allocation, or worker entry from **`disabled.conf`** (or delete the file).
2. **`kive build`** — clears `disabled = 0` and marks the allocation for rollout on the next deploy.
3. **`kive deploy`** — runs the planned lifecycle (**`make start`**, **`restart`**, or **`reload`** per **`restart_policy`**) on re-enabled workers.

```bash
# disabled.conf no longer lists api on 10.0.0.2
kive build
kive deploy
kive job status api --allocations 10.0.0.2   # optional check
```

Re-enable does **not** require a workspace content change. Hash and KV from before disable are reused.

## Disabled vs removed

| | **Disabled** | **Removed** |
|--|--------------|-------------|
| Trigger | `disabled.conf` | Drop worker from **`workers.conf`** or delete **`workspace/jobs/<job>/`** |
| Catalog row | Kept (`removed=0`) | Kept until **`kive gc`** (`removed=1`) |
| Deploy artifacts on worker | **Kept** | **Removed** (`data/` / `logs/` kept until GC) |
| Hash row | **Kept** | **Deleted** on deploy reconcile |
| Job KV | **Kept** | Purged when no non-removed allocations remain |
| Typical use | Maintenance, drain, pause | Decommission job or worker |

Workflow for **removal**:

```bash
kive build
kive deploy
kive gc
```

## Operations that skip disabled allocations

These target **active** allocations only (`removed=0`, `disabled=0`):

- Default deploy lifecycle (start / restart / reload) and per-job rsync
- **`kive job`** `start` / `stop` / `restart` / `run` / `status`
- **`kive health_check`** (probes and `health_check` commands; use **`--jobs`** to narrow the job list)

These still see disabled rows where relevant:

- **`kive hooks`** (`cli` event) and build/deploy hooks — non-removed fan-out includes disabled
- **`kive cat allocations`**, **`kive cat deployments`** (use **`--active`** to filter)
- **`kive cat kv --jobs <job>`** — includes namespaces for disabled-but-not-removed allocations
- Deploy **hash refresh** and **promote** for disabled jobs (catalog stays current)

## Examples

### Maintenance window on one worker

```bash
# 1. Drain
cat > workspace/disabled.conf <<'EOF'
disabled {
  workers("10.0.0.3");
};
EOF
kive build && kive deploy

# 2. Patch host (reboot, kernel, etc.) — see worker-reboot

# 3. Re-enable
rm -f workspace/disabled.conf   # or remove the workers entry
kive build && kive deploy
```

### Pause a job globally; keep shipping config

While **`api`** is disabled, you can still edit **`workspace/jobs/api/`**, run **`kive build`**, and **`kive deploy`**. Catalog hashes and versions update; workers stay stopped until you clear **`disabled.conf`** and deploy again.

### Verify disable took effect

```bash
kive cat allocations --jobs api
# disabled=1 on target rows

kive cat deployments --jobs api
# rollout=disabled or disabled_restart

kive deploy --dry-run
# re-enabled rows may show start; disabled rows omitted from active rollout
```

## Related

- [rolling-deploy](rolling-deploy.md)
- [worker-reboot](worker-reboot.md)
