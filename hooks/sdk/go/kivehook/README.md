<!--
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
-->

# kivehook

Go client SDK for kive hooks compiled as standalone executables. It mirrors
`kive.py` / `kive.ts` / `kive.rb` / `kive.sh`: allocation environment
accessors plus a client for the hook runtime HTTP API (KV store, secrets,
demands, semaphores).

It has **zero third-party dependencies** (Go standard library only), so you
can consume it either way:

## Option 1: copy the file

Copy `kivehook.go` directly into your hook's own package. This is the
simplest option and matches how the scripted SDKs (`kive.py`, etc.) work —
no module coordination required.

## Option 2: import as a module

If you're building the hook from within a checkout of this repo, add a
`replace` directive pointing at this directory:

```go
// go.mod
module myhook

go 1.23.1

require kivehook v0.0.0
replace kivehook => ../../../path/to/kive/hooks/sdk/go/kivehook
```

```go
// main.go
package main

import (
	"fmt"
	"os"

	"kivehook"
)

func main() {
	client := kivehook.NewClient()
	if err := client.PutJobVariable("last_deploy_by", kivehook.AllocationID()); err != nil {
		fmt.Fprintln(os.Stderr, "kivehook: put failed:", err)
		os.Exit(1)
	}
}
```

## Building a hook

Compile to an **extensionless** binary named `hook_<name>` (matching the
hook's manifest name) and place it under `workspace/jobs/<job>/_hooks/`:

```bash
cd workspace/jobs/api/_hooks
GOOS=<host-os> GOARCH=<host-arch> go build -o hook_smoke_test ./cmd/hook_smoke_test
```

Hooks run on the kive CLI host (not on workers), so `GOOS`/`GOARCH` must
match the machine running `kive build` / `kive deploy` / `kive hooks`, not
the target workers. `kive build` validates that exactly one of
`hook_<name>.py`, `.ts`, `.js`, `.rb`, `.sh`, or an extensionless executable
exists — it does not invoke `go build` for you.

## API surface

- Allocation context: `AllocationID`, `AllocationIP`, `AllocationIndex`,
  `IsAllocationDisabled`, `IsOneShot`, `HookEvent`, `HookName`, `JobName`.
- `Client` (via `NewClient()`): `GetStoreValue`, `PutJobVariable`,
  `PutJobSecret`, `DeleteJobVariable`, `DeleteJobSecret`, `ListJobKeys`,
  `PutRolloutOrder`, `GetRolloutOrder`, `ListHookDemands`,
  `AcquireSemaphore`, `ReleaseSemaphore`, `SemaphoreStatus`.

See [`public-docs/reference/cli.md`](../../../../public-docs/reference/cli.md)
for hook command flags.
