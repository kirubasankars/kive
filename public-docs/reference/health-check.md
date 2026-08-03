<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Health checks

Declare probes in **`workspace/jobs/<job>/job.conf`**. Deploy runs them after each rollout batch. **`kive health_check`** runs the same probes on demand.

Probes must sit under **`liveness { … }`** and/or **`readiness { … }`**. Liveness runs first; readiness runs only if liveness passes or is absent.

```text
health_check {
  liveness {
    ssh { command("systemctl is-active kive-hello.service"); }
  }
  readiness {
    http {
      port("hello_http_port");
      path("/health");
    }
  }
}
```

Omit `wait` unless you need something other than the built-in default (**60** attempts, **2s** apart). Omit `timeout_seconds` to keep the 5s probe timeout. Kind-local `wait` inside `liveness` or `readiness` overrides the outer wait.

| Probe | Fields |
|-------|--------|
| **`tcp`** | `port` — name a key in `resources.ports` |
| **`http`** | `port`, `path` (default `/`), `expect_status` (default `200`), `scheme` (default `http`) |
| **`ssh`** | `command` — one shell line on the worker |

**Wait resolution** when retrying (`--wait` / deploy gates): kind-local `wait` → outer `health_check.wait` (omitted → 60 at 2s) → `health.wait_seconds` in `kive.conf` (default 180 at 1s) for jobs that have health hooks but no manifest wait.

```bash
kive health_check
kive health_check --jobs hello --wait --verbose
```

Related: [CLI](cli.md) · [configuration](configuration.md) · [rolling deploy](../guide/rolling-deploy.md)
