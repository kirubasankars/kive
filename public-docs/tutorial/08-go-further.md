<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 8. Go further

**Goal:** Know which **Guide** or **Reference** page to open for common tasks after the tutorial.

**Prerequisites:** Chapters [1](01-introduction.md) through [7](07-placement.md).

You have a working bucket, a `hello` job, and understand build vs deploy. Use the tables below to continue.

After `hello`, try a [reference stack](../guide/reference-stacks.md) — Compose API, systemd service, or Prometheus — from [`examples/`](../../examples/).

## By task

| I want to… | Start with | Then lookup |
|------------|------------|-------------|
| Copy a Compose / systemd / Prometheus starter | [Guide: reference stacks](../guide/reference-stacks.md) | [CLI: job](../reference/cli.md) |
| Prepare a new worker host | [Guide: prepare a worker](../guide/prepare-worker.md) | [CLI: worker](../reference/cli.md) |
| Roll out one node at a time | [Guide: rolling deploy](../guide/rolling-deploy.md) | [CLI: deploy](../reference/cli.md) |
| Stop a node for maintenance | [Guide: disable and drain](../guide/disable-and-drain.md) | [CLI: build](../reference/cli.md) |
| Reboot a worker host | [Guide: worker reboot](../guide/worker-reboot.md) | [Guide: disable and drain](../guide/disable-and-drain.md) |
| Run `start` / `stop` / `restart` manually | [CLI: job](../reference/cli.md) | — |
| Look up any CLI flag | [Reference: CLI](../reference/cli.md) | — |
| Look up `kive.conf` / `workers.conf` / `job.conf` | [Reference: configuration](../reference/configuration.md) | — |

## Three tiers

| Tier | Use when |
|------|----------|
| **Tutorial** ([index](index.md)) | Learning kive — read in order |
| **Guides** ([guide index](../guide/index.md)) | You know basics; need one workflow |
| **Reference** ([reference index](../reference/index.md)) | Flags and file schemas |

## What you learned

- Day-2 work lives in **Guides**; schemas and flags live in **Reference**
- The tutorial covered: init, workers, jobs, build, deploy, placement
- You can re-read [Chapter 2](02-first-deploy.md) as a minimal recipe anytime

## Next

Pick a row from the task table above, or browse the [Guides index](../guide/index.md) and [Reference index](../reference/index.md).
