<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# Guides

Task-oriented documentation. Pick a guide by what you need to do.

**Format:** when to use → prerequisites → numbered steps → verify → related links.

For schemas and flags, follow links into [Reference](../reference/index.md). Guides do not duplicate Reference tables.

**Prerequisites:** complete the [tutorial](../tutorial/index.md) (at least chapters 1–2) unless noted below.

| Guide | When to read | Prerequisites |
|-------|--------------|---------------|
| [prepare-worker](prepare-worker.md) | Packages, SSH user, trust, and verify a new worker host | Tutorial ch. 2 |
| [reference-stacks](reference-stacks.md) | Copy Compose API, systemd, or Prometheus from `examples/` | Tutorial ch. 2 |
| [rolling-deploy](rolling-deploy.md) | Batch sizes, `rollout_order`, rolling upgrades | Tutorial ch. 6 |
| [disable-and-drain](disable-and-drain.md) | Pause workers, jobs, or single allocations | Tutorial ch. 7 |
| [worker-reboot](worker-reboot.md) | Host OS reboot without losing catalog state | Tutorial ch. 8 |
