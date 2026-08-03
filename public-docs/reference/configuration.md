<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Configuration reference

Kive reads configuration from the **bucket root** and **`workspace/`**.

Operator settings use a **block dialect** (syslog-ng-style) with `#` or `//` line comments. Edit these files, then run **`kive build`** (and usually **`kive deploy`**) for changes to take effect.

**Bucket discovery:** if **`BUCKET_ROOT`** is set, kive uses that absolute path. Otherwise it walks from the current working directory upward until it finds **`kive.conf`** or **`data/kive.db`**.

Related: [CLI](cli.md) · [Project files (tutorial)](../tutorial/04-project-files.md)

## Prerequisites

| Where | Required tools |
|-------|----------------|
| **CLI host** | `kive` (built with `CGO_ENABLED=1`), `bash`, `ssh`, `ssh-keyscan`, `rsync`, `python3` |
| **Workers** | `python3`, `make`, `rsync`, `bash`, `timeout`; `sudo` when `ssh.use_sudo(true)` |

SSH: private key in **`secrets/<ssh.key>`** (filename only; mode **`0600`**) authorized for **`ssh.user`** on every worker. Pin each worker host key with **`kive worker trust`**. See [Prepare a worker](../guide/prepare-worker.md).

## Block dialect

Every statement is either a **call** or a **block**:

| Kind | Shape | Examples |
|------|--------|----------|
| **Section** | `name { … }` | `ssh`, `resources`, `health_check` |
| **Named instance** | `name <id> { … }` | `job api`, `worker` |
| **Setting** | `name(value[, …])` | `port(22)`, `selectors("a","b")` |

A trailing `;` after either form is optional.

```text
ssh {
  user("agent");
  port(22);
};
resources {
  memory { min("256 mb"); max("1 gb"); };
};
```

## `kive.conf` (bucket root)

SSH, logging, and hook interpreter settings. Created on first **`kive init`**; not overwritten on later inits.

```text
timezone("UTC")
ssh {
  use_sudo(true)
  user("agent")
  key("worker.key")
  port(22)
}

# health {
#   wait_seconds(180);
# };

# deploy {
#   max_concurrent_syncs(16);
# };

port_range(30000, 39999)
certs_ttl(60)
certs_renewal_buffer(10)
iptables(false)
```

| Block / field | Default | Purpose |
|---------------|---------|---------|
| `ssh.user` | `agent` | Remote user for deploy, job control, and `run_command` |
| `ssh.key` | `worker.key` | Private key **filename** under **`secrets/`** (must be mode `0600`) |
| `ssh.port` | `22` | SSH port |
| `ssh.use_sudo` | `true` | Prefix remote rsync and some commands with `sudo` |
| `health.wait_seconds` | `180` | Fallback retry budget for jobs with health hooks but no `health_check.wait`. Manifest probes that omit `wait` use **60** attempts at **2s**. See [health-check.md](health-check.md). |
| `deploy.max_concurrent_syncs` | `16` | Max parallel SSH/rsync during fleet-wide deploy sync |
| `port_range` | `30000, 39999` | Inclusive pool for kive-assigned job ports |

## `workspace/workers.conf`

```text
worker {
  host("10.0.0.1");
  hostname("worker-a.example.com");
  labels("worker", "gpu");
  memory("4096 mb");
  cpu("2000 mhz");
  tags {
    zone("a");
  };
};
```

| Field | Purpose |
|-------|---------|
| **`host`** | Worker IP or hostname (required, unique) |
| **`hostname`** | Optional display name |
| **`labels`** | Placement labels; **`worker`** is always added automatically at build |
| **`memory`** / **`cpu`** | Capacity strings — required when the worker hosts allocated jobs |
| **`tags`** | Arbitrary key/value (for example **`zone`**) |

To fill **`memory`** and **`cpu`** from live hosts: **`kive worker facts --generate-workers > workspace/workers.conf`**, then **`kive build`**.

## `workspace/jobs/<job>/job.conf`

Minimum for a job that should allocate:

```text
version("1.0.0")
selectors("worker")
resources {
  memory {
    min("64 mb")
    max("256 mb")
  }
  cpu {
    min("100 mhz")
    max("500 mhz")
  }
}
```

| Field | Purpose |
|-------|---------|
| **`version`** | Job version recorded at build and compared on deploy |
| **`selectors`** | Must match worker labels for an allocation to exist |
| **`resources`** | Memory/CPU bounds vs worker capacity |
| **`max_concurrent_starts` / `restarts` / `stops`** | Rolling batch sizes — [rolling deploy](../guide/rolling-deploy.md) |
| **`restart_policy`** | `always` (default), `reload`, or `never` |
| **`health_check`** | Liveness/readiness probes (`tcp` / `http` / `ssh`) for deploy gates and `kive health_check` — [health-check.md](health-check.md) |

Job folder names must be lowercase with optional underscores (no hyphens).

### `health_check`

Probes go under **`liveness`** and/or **`readiness`**. Omit `wait` to get **60** attempts at **2s**. `timeout_seconds` is optional (runtime default 5s).

```text
resources {
  ports {
    hello_http_port { };
  };
};
health_check {
  liveness {
    ssh { command("systemctl is-active kive-hello.service"); };
  };
  readiness {
    http {
      port("hello_http_port");
      path("/health");
    };
  };
};
```

Full probe fields and wait resolution: [health-check.md](health-check.md).

## Makefile contract

Each job directory needs a **Makefile**. Deploy calls these targets over SSH:

| Target | When |
|--------|------|
| **`start`** | First deploy to an allocation |
| **`stop`** | Reconcile (removed or disabled) |
| **`restart`** | Upgrade when `restart_policy` is `always` |
| **`reload`** | Upgrade when `restart_policy` is `reload` |
| **`status`** | `kive job status` |

On **`start`**, **`restart`**, and **`reload`**, kive passes `CURRENT_VERSION` and `NEW_VERSION`.

Do not put `data/`, `logs/`, or `bin/` in the workspace job folder — those live on the worker at runtime.

## `workspace/disabled.conf`

Optional. Missing file means nothing disabled. See [disable and drain](../guide/disable-and-drain.md).

```text
disabled {
  job api { };
  job vault { allocations("10.0.0.2"); };
  workers("10.0.0.3");
};
```

## Other workspace files

| Path | Purpose |
|------|---------|
| `workspace/bucket.conf` | Port pool, cert TTL, iptables, bucket vars |
| `workspace/bucket.jobs.conf` | Per-job CPU/memory reservations |
| `workspace/jobs/<job>/vars.conf` | Stable job app config |
| `workspace/jobs/<job>/_prometheus/` | Optional scrape, alerts, runbooks |

## Schema after a kive binary upgrade

The CLI checks the database schema on every command except **`init`** and **`version`**. Additive changes migrate in place via **`kive init`**. If init cannot migrate, commands print `database schema upgrade required`.

```bash
kive init
kive build
```
