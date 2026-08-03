<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 5. Build

**Goal:** Run `kive build` after workspace changes and inspect the catalog before deploy.

**Prerequisites:** [Chapter 4 — Project files](04-project-files.md).

## Step 1 — Change something small

Open `workspace/jobs/hello/job.conf` and bump the version:

```text
version("1.0.1");
```

Save the file. No deploy yet — only build.

## Step 2 — Run build

```bash
kive build
```

Build syncs workers and jobs into `data/kive.db`, refreshes allocations, validates resources, and may generate TLS if your manifest declares certs (not needed for `hello`).

## Step 3 — Inspect the catalog

```bash
kive info
kive cat workers
kive cat jobs
kive cat allocations
```

`kive cat jobs` shows **`version`** from your manifest. Allocations pick up the target version at build time.

## Step 4 — Understand build vs deploy

| | **build** | **deploy** |
|---|-----------|------------|
| Reads | `workspace/` | `kive.db` |
| Writes | Local catalog | Worker filesystem + lifecycle |
| SSH to workers | No | Yes |

Typical loop: edit workspace → **build** (validate) → **deploy** (push).

## Common build failures

| Symptom | Likely cause |
|---------|--------------|
| Missing Makefile | Job folder incomplete |
| Worker too small | Job memory/CPU exceeds worker capacity in `workers.conf` |
| Duplicate host | Two entries with same `host` in `workers.conf` |

Details: [Reference: CLI](../reference/cli.md).

## What you learned

- **`kive build`** updates the local catalog from workspace
- Use **`kive cat`** to inspect workers, jobs, and allocations after build
- Build fails fast on validation errors — workers are untouched

## Next

[Chapter 6 — Deploy and changes](06-deploy-and-changes.md): push version `1.0.1` to the worker.
