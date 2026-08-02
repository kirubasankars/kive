<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Prepare a worker

Provision a Linux SSH host so kive can deploy to it. Use this before your first deploy, or when adding hosts later.

For host OS reboot after a worker is already in service, see [worker-reboot](worker-reboot.md).

## When to use

- You have a new Linux machine (VM or bare metal) and want it ready for **`kive deploy`** / **`kive run_command`**.
- A tutorial or day-2 flow told you to “add a worker” and you need the host-side checklist.

**Prerequisites:** an initialized bucket (`kive init`) on the CLI host, or follow [First deploy](../tutorial/02-first-deploy.md) in parallel.

## What a worker is

A worker is an **agentless** SSH target listed in **`workspace/workers.conf`**. Kive does not install a daemon on the host. Deploy rsyncs job trees under **`/opt/kive/<bucket_id>/`** and runs Makefile targets over SSH.

Default SSH settings in **`kive.conf`**: user **`agent`**, key file **`secrets/worker.key`**, port **`22`**, **`use_sudo(true)`**.

## Prerequisites on the worker

| Requirement | Detail |
|-------------|--------|
| **OS** | Linux |
| **On `PATH`** | `python3`, `make`, `rsync`, `bash`, `timeout` |
| **Sudo (default)** | When **`ssh.use_sudo(true)`**: `sudo` on `PATH`, and **`sudo rsync`** must work (passwordless for the deploy user) |
| **Install root** | Write access under **`/opt/kive`** (normally via that sudo) |

Deploy checks these over SSH before syncing. Missing tools surface as a **Worker prerequisite error**.

### Optional tools

| Tool | When needed |
|------|-------------|
| `iptables`, `iptables-restore` | **`iptables(true)`** in `kive.conf` |
| Docker + `docker compose` | Compose job scaffolds |
| `lsblk`, `df` | Disk volumes in **`kive worker facts`** output |
| `sar` / `mpstat` / `iostat` / `pidstat` | Richer **`kive worker sysstat`** (missing tools report `STATUS=missing`) |

## Steps

Replace `10.0.0.1` with your worker address.

### 1 — Install packages

Debian / Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y python3 make rsync bash coreutils sudo openssh-server
```

(`timeout` is part of **coreutils** on Debian/Ubuntu.)

RHEL / Rocky / Alma:

```bash
sudo dnf install -y python3 make rsync bash coreutils sudo openssh-server
```

Confirm:

```bash
command -v python3 make rsync bash timeout sudo
```

### 2 — Create the deploy user

Default user is **`agent`**. With **`use_sudo(true)`**, grant passwordless sudo so rsync and remote commands can write under **`/opt/kive`**:

```bash
sudo useradd -m -s /bin/bash agent
echo 'agent ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/agent
sudo chmod 440 /etc/sudoers.d/agent
```

Narrow the sudoers rule in production if your policy requires it; kive still needs **`sudo rsync`** (and sudo for other remote steps) when sudo is enabled.

If you set **`use_sudo(false)`**, the SSH user must own or otherwise write **`/opt/kive`** without sudo.

### 3 — Authorize the CLI host key

On the worker, as `agent` (or your `ssh.user`):

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
# paste the CLI host public key:
echo 'ssh-ed25519 AAAA... comment' >> ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
```

### 4 — Configure the bucket SSH client

On the CLI host, inside the bucket:

```bash
cp ~/.ssh/id_ed25519 secrets/worker.key
chmod 600 secrets/worker.key
```

Edit **`kive.conf`** if the user or key name differ from the defaults:

```text
ssh {
  user("agent");
  key("worker.key");
};
ssh {
  use_sudo(true);
};
```

### 5 — Register the worker

Edit **`workspace/workers.conf`**:

```text
worker {
  host("10.0.0.1");
  labels("worker");
  memory("4096 mb");
  cpu("2000 mhz");
};
```

`memory` and `cpu` are required when the host will run allocated jobs. You can fill them from the live host in step 8.

### 6 — Smoke-test SSH

```bash
ssh -i secrets/worker.key agent@10.0.0.1 echo ok
```

### 7 — Pin the host key

```bash
kive worker trust
```

Run from a trusted network. After a worker reinstall or host-key rotation, re-pin only after verifying the new key out-of-band:

```bash
kive worker trust -w 10.0.0.1 --force
```

### 8 — Optional: fill capacity from the host

```bash
kive worker facts --generate-workers > workspace/workers.conf
```

This merges probed **`memory`** / **`cpu`** into `workers.conf`. Then **`kive build`**.

## Verify

```bash
kive worker uptime --workers 10.0.0.1
kive run_command "python3 --version && make --version | head -1 && rsync --version | head -1" --workers 10.0.0.1
```

Then build and deploy as usual ([First deploy](../tutorial/02-first-deploy.md)). If deploy fails with a **Worker prerequisite error**, install the missing tools or fix sudo.

To add more hosts later, repeat steps 1–7 (or trust only the new IP).

## Related

- [First deploy (tutorial)](../tutorial/02-first-deploy.md)
- [Worker reboot](worker-reboot.md)
