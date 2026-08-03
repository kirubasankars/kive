<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# 7. Placement

**Goal:** Add a second worker and see how labels and selectors create two `hello` allocations.

**Prerequisites:** [Chapter 6 — Deploy and changes](06-deploy-and-changes.md).

## Step 1 — Add a second worker

Edit **`workspace/workers.conf`**:

```text
worker {
  host("10.0.0.1");
  labels("worker");
  memory("4096 mb");
  cpu("2000 mhz");
};

worker {
  host("10.0.0.2");
  labels("worker");
  memory("4096 mb");
  cpu("2000 mhz");
};
```

Test SSH and trust the new host:

```bash
ssh -i secrets/worker.key agent@10.0.0.2 echo ok
kive worker trust
```

## Step 2 — Build and inspect allocations

```bash
kive build
kive cat allocations --jobs hello
```

You should see two rows: `hello` on `10.0.0.1` and `hello` on `10.0.0.2`. Build matched job **selectors** `worker` to each worker's **labels**.

```text
hello job  →  hello @ 10.0.0.1
          →  hello @ 10.0.0.2
```

To run a job on only some workers, add labels (e.g. `"web"`) to those workers and set `selectors("worker", "web")` in the manifest.

## Step 3 — Deploy to both workers

```bash
kive deploy --jobs hello
```

Verify both:

```bash
kive run_command "hostname && cat /opt/kive/*/jobs/hello/data/status"
```

## Step 4 — Drain one allocation (preview)

To stop `hello` on one worker without deleting the job definition, use **`workspace/disabled.conf`**:

```text
disabled {
  job hello { allocations("10.0.0.2"); };
};
```

Then **`kive build`** and **`kive deploy`**. The disabled allocation is stopped and not restarted.

Full walkthrough: [Guide: disable and drain](../guide/disable-and-drain.md).

## What you learned

- **Labels** on workers and **selectors** in the manifest control placement
- One job can have **many allocations** (one per matching worker)
- **`disabled.conf`** drains allocations without removing workspace files

## Next

[Chapter 8 — Go further](08-go-further.md): pick Guides and Reference for your next task.
