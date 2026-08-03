<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Kive examples

Minimal job trees you can copy into a bucket’s `workspace/jobs/`. Each stack assumes you already completed [First deploy](../public-docs/tutorial/02-first-deploy.md) (bucket init, SSH worker, `kive build` / `kive deploy`).

| Stack | Directory | Job name(s) | Worker needs |
|-------|-----------|-------------|--------------|
| Compose API | [`compose-api/`](compose-api/) | `compose_api` | Docker + Compose plugin |
| systemd service | [`systemd-service/`](systemd-service/) | `systemd_service` | Passwordless `sudo` for systemctl/install |
| Prometheus | [`prometheus/`](prometheus/) | `node_exporter`, `prometheus` | Docker + Compose plugin |

Full walkthrough: [Reference stacks](../public-docs/guide/reference-stacks.md).

Job folder names must be lowercase with optional underscores (no hyphens). Copy each example’s job directory as shown in its README.
