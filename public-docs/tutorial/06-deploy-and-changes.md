<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 6. Deploy and changes

**Goal:** Roll out a change to `hello`, use dry-run to preview actions, and verify on the worker.

**Prerequisites:** [Chapter 5 — Build](05-build.md) (version bumped to `1.0.1`).

## Step 1 — Preview with dry-run

```bash
kive deploy --dry-run
```

Dry-run shows what deploy **would** do per allocation: **start**, **restart**, **reload**, **sync**, or **skip**. For an upgrade after a version bump, expect **restart** on `hello`.

## Step 2 — Deploy the change

```bash
kive deploy
```

Deploy rsyncs updated files and runs **`make restart`** on upgraded allocations (default **`restart_policy: always`**).

Inspect rollout state:

```bash
kive cat deployments --jobs hello
```

## Step 3 — Verify on the worker

```bash
kive run_command "cat /opt/kive/*/jobs/hello/data/status"
kive run_command "tail -1 /opt/kive/*/jobs/hello/logs/start.log"
```

After restart, `data/status` should still show `running` and `start.log` has a new line.

If the job declares **`health_check`** probes in `job.conf`, confirm them:

```bash
kive health_check --jobs hello --wait
```

See [health checks](../reference/health-check.md).

## Step 4 — Deploy one job only

```bash
kive deploy --jobs hello
```

Useful when the bucket has many jobs but you only changed one.

## When deploy skips an allocation

Deploy compares **content hashes** per allocation. If nothing changed and version matches, the allocation is **skipped** unless you pass **`--force`**.

```bash
kive deploy --dry-run    # see skip vs restart before running
```

If deploy fails partway, fix the issue and run **`kive deploy`** again — kive resumes incomplete work.

Reference: [CLI](../reference/cli.md). Guide: [rolling deploy](../guide/rolling-deploy.md).

## What you learned

- Upgrades run **`make restart`** by default after rsync
- **`--dry-run`** previews per-allocation actions
- **`kive cat deployments`** shows hash and version state

## Next

[Chapter 7 — Placement](07-placement.md): add a second worker and run `hello` on both.
