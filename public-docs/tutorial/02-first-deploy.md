<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 2. First deploy

**Goal:** Build the CLI from source, create a bucket, register one worker, deploy a `hello` job, and verify it on the worker.

**Prerequisites:** [Chapter 1 — Introduction](01-introduction.md).

You need:

- **CLI host:** Go 1.23+, `gcc`, a C library (`musl-dev` / `libc-dev`), `git`, `bash`, `ssh`, `rsync`, `python3`
- **One worker:** Linux with SSH, `python3`, `make`, `rsync`, `bash`, `timeout` on `PATH`
- **SSH key:** private key authorized for your worker user

For a fuller host checklist (packages, sudo, trust), see [Prepare a worker](../guide/prepare-worker.md).

## Step 1 — Build the CLI

From a checkout of this repository:

```bash
make build
```

Or:

```bash
export CGO_ENABLED=1
go build -o kive .
```

SQLite requires CGO (`CGO_ENABLED=1`). Add the `kive` binary to your `PATH`. `make build` embeds the current Git commit; `kive info` reports it.

## Step 2 — Create the bucket

```bash
mkdir my-cluster && cd my-cluster
kive init
```

You should see `kive bucket initialized`. Layout:

```text
kive.conf  data/  workspace/  secrets/  logs/
```

## Step 3 — Add your SSH key

```bash
cp ~/.ssh/id_ed25519 secrets/worker.key
chmod 600 secrets/worker.key
```

Edit **`kive.conf`** if your worker user is not `agent`:

```text
ssh {
  user("agent");
  key("worker.key");
};
ssh {
  use_sudo(true);
};
```

The matching public key must be in `~/.ssh/authorized_keys` on the worker.

## Step 4 — Register one worker

Edit **`workspace/workers.conf`**:

```text
worker {
  host("10.0.0.1");
  labels("worker");
  memory("4096 mb");
  cpu("2000 mhz");
};
```

Replace `10.0.0.1` with your worker address.

Test SSH:

```bash
ssh -i secrets/worker.key agent@10.0.0.1 echo ok
```

Pin the host key:

```bash
kive worker trust
```

## Step 5 — Create the hello job

```bash
kive job create hello --selectors worker
```

This creates `workspace/jobs/hello/` with a starter `job.conf` and Makefile.

Replace the Makefile with something minimal:

```bash
cat > workspace/jobs/hello/Makefile <<'EOF'
.PHONY: start stop restart status

start:
	mkdir -p data logs
	echo running > data/status
	date >> logs/start.log

stop:
	echo stopped > data/status

restart: stop start

status:
	@cat data/status 2>/dev/null || echo not running
EOF
```

Ensure **`job.conf`** has at least:

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

Do not put `data/`, `logs/`, or `bin/` in the workspace job folder — those live on the worker at runtime.

## Step 6 — Build the catalog

```bash
kive build
```

Build reads workspace and updates `data/kive.db`. It does **not** SSH to the worker.

Inspect:

```bash
kive cat workers
kive cat jobs
kive cat allocations
```

You should see one allocation: `hello` on `10.0.0.1`.

## Step 7 — Deploy to the worker

```bash
kive deploy
```

Deploy rsyncs files to `/opt/kive/<bucket_id>/` on the worker and runs **`make start`**.

Optional — build and deploy in one command:

```bash
kive deploy --build
```

## Step 8 — Verify on the worker

```bash
kive run_command "cat /opt/kive/*/jobs/hello/data/status"
```

Expected output includes `running`.

Or SSH directly:

```bash
ssh -i secrets/worker.key agent@10.0.0.1 "cat /opt/kive/*/jobs/hello/data/status"
```

## Step 9 — Inspect from the CLI host

```bash
kive info
kive cat deployments --jobs hello
```

You now have a bucket, one worker, one job, and one running allocation.

## What you learned

- Build the CLI from source; **`kive init`** creates a bucket; **`workers.conf`** lists SSH targets
- A **job** lives under `workspace/jobs/<name>/` with `job.conf` and a **Makefile**
- **`kive build`** updates the local catalog; **`kive deploy`** pushes to workers
- Deploy calls **`make start`** on first deploy to a worker

## Next

[Chapter 3 — The model](03-the-model.md): name bucket, worker, job, and allocation.
