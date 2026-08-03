<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# systemd service

Unit install via Makefile — same pattern as `kive job create … --runtime=systemd`. A small shell script under `bin/` is the process systemd supervises.

## Prerequisites

- Bucket with at least one worker labeled `worker`
- Workers: `systemd`, and passwordless `sudo` for `systemctl` / `install` / `rm` (or override `SYSTEMCTL`, `INSTALL`, `REMOVE` in the Makefile)

## Install into your bucket

```bash
cp -R examples/systemd-service/systemd_service workspace/jobs/systemd_service
chmod +x workspace/jobs/systemd_service/bin/systemd_service
kive build
kive deploy --jobs systemd_service
```

## Verify

```bash
kive job status systemd_service
```

On the worker: `systemctl status kive-systemd_service.service` should be active. The script writes `data/status` under the job directory.

## Files

| File | Role |
|------|------|
| `job.conf` | Selectors and light resource reservation |
| `Makefile` | Renders unit, `daemon-reload`, enable/start |
| `bin/systemd_service` | `ExecStart` process (must be executable) |

Scaffold only: `kive job create mysvc --runtime=systemd --selectors worker` (then add `bin/<job>`).
