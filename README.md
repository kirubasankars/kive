<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Kive

Kive is an **agentless** workload orchestrator for Linux clusters. State lives in a local **bucket** (SQLite catalog, KV, secrets). Workers are reached over **SSH** and **rsync** from the CLI host — no kive agent on the nodes.

**For** teams deploying packaged kits on a fixed Linux SSH fleet — configure workers and sizing in a bucket, not custom deploy scripts per service. One project / one bucket. **Not for** baseline OS/package config; a shared platform for many teams; systemd-only single-host supervision; or Kubernetes-native workloads.

## Kive and Ansible

Both reach Linux hosts the same way: SSH from your machine, nothing installed on
the nodes. The difference is scope. Ansible is a general-purpose automation
engine — it will run any task against anything. Kive does one narrow job: keep a
known set of versioned services running on a known fleet.

That narrow job is where generic automation gets expensive. A playbook that
deploys a service tends to grow its own orchestrator: inventory conventions for
which host runs what, `serial:` batches, `wait_for` health checks, version
variables, and a "is this host already updated?" question the playbook cannot
answer because nothing recorded the last run. Kive ships that layer instead of
asking you to write it.

| Concern | Ansible | Kive |
|---------|---------|------|
| Unit of work | Tasks and roles, executed top to bottom | Jobs: a manifest, a `Makefile`, and files under `workspace/jobs/<name>/` |
| Desired state | The playbook, re-interpreted each run | Compiled by `kive build` into a SQLite catalog |
| What is remembered between runs | Nothing about the target's converged state | Per-allocation applied content hash and running version |
| Repeat run with no changes | Replays every task; "changed" is per-module idempotence you write | Skips the job entirely — rollout is already complete |
| Placement | Inventory groups and `host_vars` you maintain by hand | Workers carry labels, jobs declare selectors; allocations are computed at build |
| Ordering across services | Play and role order, by hand | Deploy order computed from declared demands; build rejects an order that would invert a dependency |
| Rolling upgrade | `serial:` plus handlers and health checks you assemble | Batches with a health gate per batch, built in |
| Partial failure | Re-run, usually from the top | Batches that passed health stay promoted; the failed batch is recorded and resumed by the next `kive deploy` |
| Per-host config | Jinja templates per host | `.tpl` templates rendered per allocation, with running and target version, worker identity and labels, plus assigned ports and KV lookups |
| Restart on config change | Handlers you wire to each task | `restart_policy` / `restart_globs` / `reload_globs` in the manifest |
| Secrets and TLS | `ansible-vault` or an external store | Encrypted KV in the bucket; a bucket CA issues per-job certificates for each worker |
| Reach | SSH plus many other connection types: network gear, cloud APIs, Windows | SSH and rsync to Linux hosts, only |

### Keep Ansible

Kive is not a replacement for general automation and does not try to be. Use
Ansible for OS baseline and hardening, package and kernel policy, one-off
remediation, and anything that is not a Linux service on a fixed fleet. Many
teams run both: Ansible builds and maintains the hosts, Kive runs the workloads
on them.

### Replace an Ansible playbook with Kive

Worth the switch when a playbook has grown into a service orchestrator: it
carries `serial:` batches and hand-written readiness checks, its inventory
encodes which node is primary or which is a replica, it must run roles in a
strict cross-service order, or an interrupted upgrade cannot safely resume
without someone reasoning about which hosts already got the new version.

Rough mapping when you move one over:

| Ansible | Kive |
|---------|------|
| Inventory hosts and groups | `workspace/workers.conf` with labels |
| A role per service | A job under `workspace/jobs/<name>/` |
| `group_vars` sizing and limits | `workspace/bucket.jobs.conf` |
| `serial:` and rolling batches | Rollout batches and per-batch health gates |
| `wait_for` / `uri` readiness | `health_check` hooks |
| Handlers | `restart_policy` and `restart_globs` / `reload_globs` |
| `service` / `systemd` / `docker_compose` tasks | The job's `Makefile` targets |
| `ansible-playbook deploy.yml` | `kive build` then `kive deploy` |

See [Rolling deploy](public-docs/guide/rolling-deploy.md) for the batch and
health-gate behavior, and [First deploy](public-docs/tutorial/02-first-deploy.md)
to try the loop on one host.

## Documentation

Docs ship in this repository under [`public-docs/`](public-docs/).

- [Tutorial](public-docs/tutorial/index.md)
- [First deploy](public-docs/tutorial/02-first-deploy.md)
- [Guides](public-docs/guide/index.md)
- [Reference stacks](public-docs/guide/reference-stacks.md) — Compose API, systemd, Prometheus
- [Rolling deploy](public-docs/guide/rolling-deploy.md)
- [CLI reference](public-docs/reference/cli.md)
- [Configuration](public-docs/reference/configuration.md)

## Examples

Copyable job trees live under [`examples/`](examples/) in this repository:

| Stack | Path |
|-------|------|
| Compose API | [`examples/compose-api/`](examples/compose-api/) |
| systemd service | [`examples/systemd-service/`](examples/systemd-service/) |
| Prometheus | [`examples/prometheus/`](examples/prometheus/) |

Walkthrough: [Reference stacks](public-docs/guide/reference-stacks.md).

## Releases

GitHub Releases attach prebuilt binaries (`linux` / `darwin`, `amd64` / `arm64`) as `.tar.gz` archives plus `SHA256SUMS`. Prefer those for install; build from source when you need a custom revision.

## How to build

Kive uses SQLite via CGO (`CGO_ENABLED=1`). Requires Go 1.23+, `gcc`, and a C library (e.g. `musl-dev` / `libc-dev`).

```bash
make build
```

Or manually:

```bash
export CGO_ENABLED=1
go build -o kive .
```

Add the binary to your `PATH`, then follow [Chapter 2 — First deploy](public-docs/tutorial/02-first-deploy.md) or the full [tutorial](public-docs/tutorial/index.md).

## License

Source code is published under the [GNU Affero General Public License v3.0](LICENSE).
