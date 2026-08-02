<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Prometheus + node_exporter

Two Compose jobs: **node_exporter** publishes host metrics and a `_prometheus/` scrape/alert pack; **prometheus** is the server job that assembles scrape configs and rules at deploy.

## Prerequisites

- Bucket with at least one worker labeled `worker`
- Workers: Docker Engine + Compose plugin

## Install into your bucket

```bash
cp -R examples/prometheus/node_exporter workspace/jobs/node_exporter
cp -R examples/prometheus/prometheus workspace/jobs/prometheus
kive build
kive deploy --jobs node_exporter,prometheus
```

Deploy both in one pass so allocation ports exist when Prometheus config is rendered. If you deploy Prometheus alone first, redeploy it after `node_exporter` is allocated.

## Verify

```bash
kive health_check --jobs node_exporter,prometheus --wait --verbose
kive cat prometheus
kive job run node_exporter --target test
kive job run prometheus --target test
```

Open the Prometheus UI on the worker at the reserved `prometheus_http_port` and confirm a `node_exporter` target is **up**.

## Memory budget

The prometheus job pins Compose `mem_limit` to the kive reservation `$M` (`kive/job/prometheus/memory`, default **1024 MB** = manifest max). `GOMEMLIMIT` is 80% of `$M` so Go GC runs before the cgroup OOM killer; `--storage.tsdb.retention.size` is half of `$M` to cap mmap RSS. Query and scrape caps (`query.max-samples`, `sample_limit`, `body_size_limit`) bound PromQL and a runaway target.

| Knob | At default `$M=1024` |
|------|----------------------|
| `mem_limit` | 1024m |
| `GOMEMLIMIT` | 819MiB (must stay below `mem_limit`) |
| `retention.size` | 512MB |
| `query.max-samples` | 5,120,000 |

If the container is OOM-killed, confirm `$M` covers the head block plus queries. To raise RAM, increase `resources.memory.max` in `job.conf`, set `job prometheus { memory("…"); }` in `bucket.jobs.conf`, then `kive build && kive deploy --jobs prometheus`.

## Layout

```text
examples/prometheus/
├── node_exporter/
│   ├── job.conf
│   ├── Makefile
│   ├── config.env.tpl
│   ├── docker-compose.yml.tpl
│   └── _prometheus/
│       ├── scrape.yaml
│       └── alerts/alerts.yaml
└── prometheus/
    ├── job.conf
    ├── Makefile
    ├── config.env.tpl
    ├── docker-compose.yml.tpl
    └── prometheus.yml.tpl
```

Walkthrough: [Reference stacks](../../public-docs/guide/reference-stacks.md).
