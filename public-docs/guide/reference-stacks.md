<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Reference stacks

**Goal:** Deploy one of three copyable stacks — Compose API, systemd service, or Prometheus — from [`examples/`](../../examples/).

**Prerequisites:** [First deploy](../tutorial/02-first-deploy.md) (bucket, SSH worker labeled `worker`, successful `hello` deploy).

Source trees live under [`examples/`](../../examples/) in this repository. Paths below assume the repo root is on disk next to your bucket (adjust as needed).

## Choose a stack

| Stack | Copy from | Job name(s) | Worker needs |
|-------|-----------|-------------|--------------|
| [Compose API](#compose-api) | `examples/compose-api/compose_api/` | `compose_api` | Docker + Compose plugin |
| [systemd service](#systemd-service) | `examples/systemd-service/systemd_service/` | `systemd_service` | systemd + passwordless sudo for unit install |
| [Prometheus](#prometheus) | `examples/prometheus/{node_exporter,prometheus}/` | `node_exporter`, `prometheus` | Docker + Compose plugin |

Scaffolds without full trees: `kive job create <name> --runtime=compose|systemd` — [job CLI](../reference/cli.md).

## Compose API

HTTP service via Docker Compose, port reservation, and HTTP readiness.

### Steps

1. Copy the job into the bucket:

   ```bash
   cp -R examples/compose-api/compose_api workspace/jobs/compose_api
   ```

2. Build and deploy:

   ```bash
   kive build
   kive deploy --jobs compose_api
   ```

3. Verify:

   ```bash
   kive health_check --jobs compose_api --wait --verbose
   kive job status compose_api
   ```

### What you get

- `hashicorp/http-echo` listening on the reserved `compose_api_http_port`
- Makefile targets matching the Compose scaffold (`start` / `stop` / `status` / `logs`)
- Manifest `health_check` HTTP probe on `/`

Details in [`examples/compose-api/README.md`](../../examples/compose-api/README.md).

## systemd service

Makefile renders a unit under `/etc/systemd/system`, enables it, and runs `bin/systemd_service`.

### Steps

1. Copy and ensure the binary is executable:

   ```bash
   cp -R examples/systemd-service/systemd_service workspace/jobs/systemd_service
   chmod +x workspace/jobs/systemd_service/bin/systemd_service
   ```

2. Build and deploy:

   ```bash
   kive build
   kive deploy --jobs systemd_service
   ```

3. Verify:

   ```bash
   kive health_check --jobs systemd_service --wait --verbose
   kive job status systemd_service
   ```

### What you get

- Unit `kive-systemd_service.service` (name from Makefile `JOB_NAME`)
- SSH liveness probe: `systemctl is-active …`
- Override `SYSTEMCTL` / `INSTALL` / `REMOVE` if sudo is unavailable

Details in [`examples/systemd-service/README.md`](../../examples/systemd-service/README.md).

## Prometheus

`node_exporter` publishes metrics and a `_prometheus/` scrape/alert pack; the `prometheus` job assembles scrape configs and rules at deploy.

### Steps

1. Copy both jobs:

   ```bash
   cp -R examples/prometheus/node_exporter workspace/jobs/node_exporter
   cp -R examples/prometheus/prometheus workspace/jobs/prometheus
   ```

2. Build and deploy together (so scrape targets expand with live allocations):

   ```bash
   kive build
   kive deploy --jobs node_exporter,prometheus
   ```

3. Verify:

   ```bash
   kive health_check --jobs node_exporter,prometheus --wait --verbose
   kive cat prometheus
   kive job run node_exporter --target test
   kive job run prometheus --target test
   ```

### What you get

- Compose-based Prometheus with scrape configs and rule files rendered at deploy
- `node_exporter` `_prometheus/scrape.yaml` using the reserved metrics port
- A sample alert when the scrape target is down

Example README: [`examples/prometheus/README.md`](../../examples/prometheus/README.md).

## Related

- [Prepare a worker](prepare-worker.md) — Docker, sudo, packages
- [Job create](../reference/cli.md) — scaffold a job from a runtime template
