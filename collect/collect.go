// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package collect probes workers over SSH for kive worker subcommands.
package collect

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"kive/bucket"
	"kive/prereq"
	"kive/utils"
	"kive/worker"
	"kive/workspace"
)

// Options configures collect subcommands.
type Options struct {
	WorkerCSV       string
	LabelCSV        string
	Concurrency     int
	IgnoreFailure   bool
	GenerateWorkers bool // facts only: print updated workers.conf to stdout
}

func executeCollect[T any](
	ctx context.Context,
	rt *bucket.Runtime,
	opts Options,
	probeName string,
	probe func(string) (T, error),
	printResult func(string, T),
	printResults func([]workspace.WorkerRecord, []workspace.WorkerRecord, map[string]T) error,
	applyResults func(map[string]T) error,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
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

	if len(workers) == 0 && opts.WorkerCSV == "" && opts.LabelCSV == "" {
		return errNoWorkersConfigured()
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

	if err := prereq.CheckLocalRunCommand(); err != nil {
		return err
	}

	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}

	if opts.IgnoreFailure {
		// Match run_command: skip batch trust/prereq so reachable workers still
		// probe when some selected hosts are untrusted or offline.
		if err := worker.ValidateSSHConfig(); err != nil {
			return fmt.Errorf("%w: %s", bucket.ErrWorkerPrerequisites, err.Error())
		}
		if err := worker.EnsureSSHStateDir(); err != nil {
			return err
		}
		probe = wrapProbeWithPrereq(ctx, rt, conf.UseSUDO, probe)
	} else if rt != nil {
		if err := worker.CheckRunCommandPrerequisitesWithRuntime(ctx, rt, targetHosts, conf.UseSUDO); err != nil {
			return err
		}
	} else if err := worker.CheckRunCommandPrerequisitesContext(ctx, targetHosts, conf.UseSUDO); err != nil {
		return err
	}

	results, failures, err := probeWorkers(ctx, targetHosts, opts.Concurrency, probe, printResult)
	if err != nil {
		return err
	}

	return reportCollectResults(probeName, opts.IgnoreFailure, workers, targetWorkers, results, failures, printResults, applyResults)
}

func wrapProbeWithPrereq[T any](ctx context.Context, rt *bucket.Runtime, useSudo bool, probe func(string) (T, error)) func(string) (T, error) {
	return func(workerIP string) (T, error) {
		var err error
		if rt != nil {
			err = worker.CheckWorkerRunCommandPrerequisitesWithRuntime(ctx, rt, workerIP, useSudo)
		} else {
			err = worker.CheckWorkerRunCommandPrerequisitesContext(ctx, workerIP, useSudo)
		}
		if err != nil {
			var zero T
			return zero, err
		}
		return probe(workerIP)
	}
}

func reportCollectResults[T any](
	probeName string,
	ignoreFailure bool,
	allWorkers []workspace.WorkerRecord,
	targetWorkers []workspace.WorkerRecord,
	results map[string]T,
	failures map[string]error,
	printResults func([]workspace.WorkerRecord, []workspace.WorkerRecord, map[string]T) error,
	applyResults func(map[string]T) error,
) error {
	if len(failures) == 0 {
		if applyResults != nil {
			if err := applyResults(results); err != nil {
				return err
			}
		}
		if printResults != nil {
			if err := printResults(allWorkers, targetWorkers, results); err != nil {
				return err
			}
		}
		return nil
	}
	if ignoreFailure {
		if applyResults != nil && len(results) > 0 {
			if err := applyResults(results); err != nil {
				return err
			}
		}
		if printResults != nil && len(results) > 0 {
			if err := printResults(allWorkers, targetWorkers, results); err != nil {
				return err
			}
		}
		printWorkerFailures(probeName, failures)
		return probeFailuresError(probeName, failures)
	}
	return probeFailuresError(probeName, failures)
}

func selectTargetWorkers(workers []workspace.WorkerRecord, workerCSV, labelCSV string) ([]workspace.WorkerRecord, error) {
	workerFilter := parseCSVList(workerCSV)
	labelFilter := parseCSVList(labelCSV)

	switch {
	case len(workerFilter) > 0:
		known := make(map[string]workspace.WorkerRecord, len(workers))
		for _, w := range workers {
			host := strings.TrimSpace(w.Host)
			if host != "" {
				known[host] = w
			}
		}
		unknown := make([]string, 0)
		selected := make([]workspace.WorkerRecord, 0, len(workerFilter))
		for _, host := range utils.Unique(workerFilter) {
			w, ok := known[host]
			if !ok {
				unknown = append(unknown, host)
				continue
			}
			selected = append(selected, w)
		}
		if len(unknown) > 0 {
			return nil, errUnknownWorkers(unknown)
		}
		return selected, nil

	case len(labelFilter) > 0:
		return workspace.FilterWorkersByLabels(workers, labelFilter), nil

	default:
		return workers, nil
	}
}

func parseCSVList(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func probeWorkers[T any](
	ctx context.Context,
	hosts []string,
	concurrency int,
	probe func(string) (T, error),
	onSuccess func(string, T),
) (map[string]T, map[string]error, error) {
	hosts = utils.Unique(hosts)
	if len(hosts) == 0 {
		return nil, nil, errNoTargetWorkers()
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures = make(map[string]error, len(hosts))
		results  = make(map[string]T, len(hosts))
		sem      = make(chan struct{}, concurrency)
	)

	for _, host := range hosts {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return nil, nil, err
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil, nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-sem
			wg.Wait()
			return nil, nil, err
		}
		wg.Add(1)
		go func(workerIP string) {
			defer wg.Done()
			defer func() { <-sem }()

			value, err := probe(workerIP)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures[workerIP] = err
				return
			}
			results[workerIP] = value
			if onSuccess != nil {
				onSuccess(workerIP, value)
			}
		}(host)
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return results, failures, nil
}
