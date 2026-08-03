// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"context"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/worker"
	"kive/workspace"
)

// ExecuteTrust pins SSH host keys for target workers using ssh-keyscan.
func ExecuteTrust(opts Options, force bool) error {
	return ExecuteTrustContext(context.Background(), opts, force)
}

// ExecuteTrustContext pins host keys with cancellation.
func ExecuteTrustContext(ctx context.Context, opts Options, force bool) error {
	rt, err := bucket.SetupRuntime("", bucket.NewRunContext("worker", 0))
	if err != nil {
		return err
	}
	return runWorkerSubcommand(rt, "trust", func() error {
		if opts.Concurrency < 1 {
			return errConcurrencyTooLow()
		}
		if opts.WorkerCSV != "" && opts.LabelCSV != "" {
			return errWorkersAndLabelsTogether()
		}

		workers, err := workspace.ReadWorkersFile()
		if err != nil {
			return err
		}

		targetWorkers, err := selectTargetWorkers(workers, opts.WorkerCSV, opts.LabelCSV)
		if err != nil {
			return err
		}
		if len(targetWorkers) == 0 {
			return errNoTargetWorkers()
		}

		targetHosts := make([]string, 0, len(targetWorkers))
		for _, w := range targetWorkers {
			targetHosts = append(targetHosts, strings.TrimSpace(w.Host))
		}

		failures, err := worker.TrustHostsLoggedFailuresContext(
			ctx, rt, targetHosts, force, opts.Concurrency,
		)
		if err != nil {
			return err
		}

		if len(failures) == 0 || opts.IgnoreFailure {
			for _, host := range targetHosts {
				if _, failed := failures[host]; failed {
					continue
				}
				fmt.Printf("trusted %s\n", host)
				fps, err := worker.HostKeyFingerprintsContext(ctx, host)
				if err != nil {
					return err
				}
				for _, fp := range fps {
					fmt.Printf("  %s\n", fp)
				}
			}
		}

		if len(failures) == 0 {
			return nil
		}
		if opts.IgnoreFailure {
			printWorkerFailures("trust", failures)
			return probeFailuresError("trust", failures)
		}
		lines := make([]string, 0, len(failures))
		for _, failErr := range failures {
			lines = append(lines, failErr.Error())
		}
		return fmt.Errorf("%w:\n%s", bucket.ErrWorkerPrerequisites, strings.Join(lines, "\n"))
	})
}
