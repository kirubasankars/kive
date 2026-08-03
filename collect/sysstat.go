// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"context"
	"strings"

	"kive/bucket"
	"kive/worker"
)

// Line-buffer remote stdout (SSH pipes are fully buffered without a TTY) so each
// section appears in Activity/CLI as it finishes instead of after the whole snapshot.
const sysstatScript = `set -eu
if command -v stdbuf >/dev/null 2>&1; then
  exec > >(stdbuf -oL cat) 2> >(stdbuf -oL cat >&2)
elif command -v awk >/dev/null 2>&1; then
  exec > >(awk '{ print; fflush() }') 2> >(awk '{ print; fflush() }' >&2)
fi

has_sar=0
has_mpstat=0
has_iostat=0
has_pidstat=0
command -v sar >/dev/null 2>&1 && has_sar=1
command -v mpstat >/dev/null 2>&1 && has_mpstat=1
command -v iostat >/dev/null 2>&1 && has_iostat=1
command -v pidstat >/dev/null 2>&1 && has_pidstat=1

if [ "$has_sar" -eq 0 ] && [ "$has_mpstat" -eq 0 ] && [ "$has_iostat" -eq 0 ] && [ "$has_pidstat" -eq 0 ]; then
  echo "STATUS=missing"
  exit 0
fi

if [ "$has_sar" -eq 1 ]; then
  echo "# cpu"
  sar -q 1 1 | grep -v "Average:" || true
fi

if [ "$has_sar" -eq 1 ]; then
  echo "# memory"
  sar -r 1 1 | grep -v "Average:" || true
fi

if [ "$has_sar" -eq 1 ]; then
  echo "# swap"
  sar -W 1 1 | grep -v "Average:" || true
fi

if [ "$has_mpstat" -eq 1 ]; then
  echo "# per-cpu"
  mpstat -P ALL 1 1 | grep -v "Average:" || true
fi

if [ "$has_iostat" -eq 1 ]; then
  echo "# disk"
  iostat -xz 1 2 | awk '/^Device:/ {count++} count==2 {print}' || true
fi

if [ "$has_sar" -eq 1 ]; then
  echo "# network"
  sar -n DEV 1 1 | grep -v "Average:" | grep -E "IFACE|eth|en|lo" || true
fi

if [ "$has_sar" -eq 1 ]; then
  echo "# tcp"
  sar -n TCP,ETCP 1 1 | grep -v "Average:" || true
fi

if [ "$has_pidstat" -eq 1 ]; then
  echo "# processes"
  pidstat 1 1 | grep -v "Average:" || true
fi
`

// ExecuteSysstat probes workers and streams the sysstat snapshot as remote lines arrive.
func ExecuteSysstat(opts Options) error {
	return ExecuteSysstatContext(context.Background(), opts)
}

// ExecuteSysstatContext probes sysstat with cancellation.
func ExecuteSysstatContext(ctx context.Context, opts Options) error {
	rt, err := bucket.SetupRuntime("", bucket.NewRunContext("worker", 0))
	if err != nil {
		return err
	}
	return runWorkerSubcommand(rt, "sysstat", func() error {
		return executeCollect(ctx, rt, opts, "sysstat", probeSysstat(ctx, rt), nil, nil, nil)
	})
}

func probeSysstat(ctx context.Context, rt *bucket.Runtime) func(string) (struct{}, error) {
	return func(workerIP string) (struct{}, error) {
		err := worker.RunRemoteScript(ctx, rt, workerIP, bucket.CommandContext{
			Phase:  "worker",
			Action: "sysstat",
			Quiet:  false, // stream section output live to CLI / Activity
		}, strings.NewReader(sysstatScript), false)
		return struct{}{}, err
	}
}
