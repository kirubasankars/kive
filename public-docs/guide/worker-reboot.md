<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Rolling worker reboot (host OS)

Kive has no built-in **`reboot`** command. Use **disable → stop → reboot → re-enable**.

Related: [disable-and-drain](disable-and-drain.md) · [rolling-deploy](rolling-deploy.md)

## Pattern A — disable worker, reboot, re-enable

```bash
# 1. Drain all jobs on the host
cat > workspace/disabled.conf <<'EOF'
disabled {
  workers("10.0.0.3");
};
EOF
kive build && kive deploy

# 2. Reboot (SSH from kive host)
kive run_command "sudo shutdown -r now" --workers 10.0.0.3

# 3. Wait for SSH (manual or scripted)
#
# Example wait loop from the CLI host:
# until ssh -o ConnectTimeout=5 agent@10.0.0.3 true; do sleep 5; done

# 4. Re-enable — remove 10.0.0.3 from disabled.conf
kive build && kive deploy

# 5. Optional: verify all jobs after re-enable
kive health_check --jobs api --wait
```

Disabled allocations **keep** deploy artifacts and KV; after reboot, **`deploy`** **starts** jobs again on re-enable.

If **`kive job`** fails with **`worker.json` / `generation` mismatch** after reboot (catalog moved while the host was down), run **`kive deploy`** for that worker’s jobs before **`kive job`** — deploy refreshes the worker sync files.

## Pattern B — rolling reboot across a fleet

```bash
kive run_command "sudo shutdown -r now" --workers 10.0.0.1,10.0.0.2,10.0.0.3 --concurrency 1

# Optional: verify all jobs after the rolling reboot
kive health_check --wait
```

No shell loop is required for the reboot command itself; **`kive run_command`** batches workers and honors **`--concurrency`**.

If you want strict **disable → deploy → reboot → re-enable → deploy** per worker, you still need per-worker `disabled.conf` edits (scripted or manual).

## Pattern C — parallel SSH only (no catalog drain)

For workers where a hard reboot without drain is acceptable:

```bash
kive run_command "sudo shutdown -r now" --workers 10.0.0.1,10.0.0.2 --concurrency 1
```

You can add **`--health_check`** to **`kive run_command`** to run job health checks after each command batch:

```bash
kive run_command "systemctl restart docker" --workers 10.0.0.1,10.0.0.2 --concurrency 1 --health_check
```

For **reboot** specifically, prefer explicit **`kive health_check --wait`** after the host returns, because health checks triggered immediately after `shutdown -r now` may run while the worker is still booting.

**`--concurrency 1`** reboots one host at a time. After reboot, processes may be down until **`kive job start`** or **`kive deploy`**. Prefer Pattern A for production.

## After reboot: sync check

```bash
kive job status api --allocations 10.0.0.3
kive worker uptime --workers 10.0.0.3
```

If **`worker.json` / `generation` mismatch**, run **`kive deploy`** before **`kive job`**. Prefer **`kive health_check --wait`** after SSH returns rather than relying on health checks that fired during reboot. Rolling health gates on the next deploy still apply — see [rolling-deploy](rolling-deploy.md).

## Related

- [disable-and-drain](disable-and-drain.md)
- [rolling-deploy](rolling-deploy.md)
