<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 1. Introduction

**Goal:** Understand what kive does and where each piece runs before you type any commands.

**Prerequisites:** Linux basics (SSH, `make`). No prior kive experience.

## The idea

You have a fixed set of Linux machines — a small cluster. You want to:

- declare which services run on which machines
- copy files and run `make start` / `make restart` in a controlled order
- see what is deployed and what changed

Kive is an **agentless workload orchestrator** for that. You run one CLI on a **control machine** (laptop, bastion, or CI runner). That machine holds a **bucket** — project files and a local catalog. **Workers** are ordinary hosts you reach with **SSH** and **rsync**. Nothing kive-specific is installed on workers until you **deploy** job files there.

The tutorial builds a hand-written `hello` job so you see the full loop: edit workspace → build catalog → deploy.

If you already use shell scripts, Makefiles, and SSH to manage hosts, kive formalizes that into a catalog, a build step, and rolling deploys.

## What runs where

```text
CLI host                         Worker 10.0.0.1
────────                         ───────────────
workspace/  (you edit)
data/kive.db  (catalog)  ──deploy: rsync + ssh──►  /opt/kive/.../jobs/hello/
secrets/  (SSH key)
```

| Location | What happens there |
|----------|-------------------|
| **CLI host** | `kive build`, `kive deploy`, hook scripts |
| **Workers** | Your app files, Makefile targets, runtime `data/` on disk |

Kive does **not** replace Docker or systemd. Your job **Makefile** calls them; kive calls the Makefile during deploy.

## The usual loop

```text
hand-edit workspace → kive build → kive deploy → operate → kive gc
```

| Step | Touches workers? |
|------|------------------|
| **edit files** | No — files under `workspace/` |
| **build** | No — updates local catalog only |
| **deploy** | Yes — rsync + `make start` / `restart` |
| **gc** | Yes — cleanup after you remove jobs or workers from workspace |

## What you learned

- Kive orchestrates a **fixed worker pool** over **SSH**, with no agent on workers
- The **CLI host** holds the bucket; **workers** run what you deploy
- Work flows: **hand-edit workspace → build → deploy**

## Next

[Chapter 2 — First deploy](02-first-deploy.md): build the CLI and deploy a simple `hello` job step by step.
