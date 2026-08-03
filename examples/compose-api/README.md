<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Compose API

Docker Compose–backed HTTP service using the same Makefile pattern as `kive job create … --runtime=compose`, with a real image and health check.

## Prerequisites

- Bucket with at least one worker labeled `worker` (see [First deploy](../../public-docs/tutorial/02-first-deploy.md))
- Workers: Docker Engine + Compose plugin (`docker compose version`)

## Install into your bucket

```bash
cp -R examples/compose-api/compose_api workspace/jobs/compose_api
kive build
kive deploy --jobs compose_api
```

## Verify

```bash
kive health_check --jobs compose_api --wait --verbose
kive job status compose_api
```

On the worker, Compose should show the `app` service up. HTTP probe hits `/` on the reserved `compose_api_http_port`.

## Files

| File | Role |
|------|------|
| `job.conf` | Selectors, port reservation, HTTP readiness |
| `Makefile` | `docker compose` lifecycle |
| `docker-compose.yml.tpl` | `hashicorp/http-echo` bound to the reserved port |
| `.dockerignore` | Default ignore list |

Scaffold only (placeholder image): `kive job create myapi --runtime=compose --selectors worker`.
