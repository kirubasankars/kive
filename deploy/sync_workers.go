// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"kive/bucket"
	"kive/iptables"
	"kive/worker"
	"kive/workspace"
)

func syncWorkerFiles(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string, jobs []string, applyRules bool) error {
	mkdirPaths := []string{bucket.WorkerBucketPath(bucketID)}
	for _, job := range jobs {
		mkdirPaths = append(mkdirPaths, path.Join(bucket.WorkerJobPath(bucketID, job), "certs"))
	}
	mkdirCmd := "mkdir -p " + strings.Join(mkdirPaths, " ")
	if err := runWorkerCommand(
		ctx,
		rt,
		workerIP,
		bucket.CommandContext{
			Job:    strings.Join(jobs, ","),
			Phase:  "rsync",
			Action: "mkdir",
			Cmd:    mkdirCmd,
		},
		[]string{mkdirCmd},
		nil,
	); err != nil {
		return err
	}
	if _, err := writeRsyncFilter(workerIP, jobs, applyRules); err != nil {
		return err
	}
	if err := normalizeStagedCertModes(bucket.GetTempWorkerPath(workerIP), jobs); err != nil {
		return err
	}
	if err := runRsync(ctx, rt, bucketID, workerIP, jobs); err != nil {
		return err
	}
	return runRsyncCerts(ctx, rt, bucketID, workerIP, jobs)
}

func normalizeStagedCertModes(workerDir string, jobs []string) error {
	for _, job := range jobs {
		certsDir := path.Join(workerDir, "jobs", job, "certs")
		info, err := os.Stat(certsDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat staged certs for job %s: %w", job, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("staged certs path for job %s is not a directory", job)
		}

		if err := filepath.WalkDir(certsDir, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.Type().IsRegular() {
				return nil
			}

			var mode os.FileMode
			switch filepath.Ext(filePath) {
			case ".crt":
				mode = 0o644
			case ".key":
				mode = 0o600
			default:
				return nil
			}
			return os.Chmod(filePath, mode)
		}); err != nil {
			return fmt.Errorf("normalize staged cert modes for job %s: %w", job, err)
		}
	}
	return nil
}

func syncWorkers(ctx context.Context, rt *bucket.Runtime, bucketID string, workers []string, jobs []string, applyRules bool) error {
	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}
	limit, err := maxConcurrentSyncs()
	if err != nil {
		return err
	}
	err = runParallelWorkers(ctx, workers, limit, func(ip string) error {
		if err := syncWorkerFiles(ctx, rt, bucketID, ip, jobs, applyRules); err != nil {
			return fmt.Errorf("worker %s: %w", ip, err)
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("rsync failed:\n%s", err.Error())
	}
	return nil
}

func buildRsyncFilterLines(jobs []string, applyRules bool) []string {
	lines := append([]string{}, workspace.JobRuntimeDirRsyncProtectFilterLines()...)
	lines = append(lines, "P jobs/*/certs/\n")
	lines = append(lines, bucket.WorkerBackupsRsyncProtectFilterLines()...)
	// worker.json is uploaded only on the final metadata rsync after a clean run.
	lines = append(lines, "- worker.json\n")
	if applyRules {
		for _, job := range jobs {
			lines = append(lines, fmt.Sprintf("+ jobs/%s/\n", job))
		}
		lines = append(lines, "- jobs/*\n")
	}
	lines = append(lines, "- jobs/**/*.tpl\n")
	return lines
}

func buildMetadataRsyncFilterLines(includeIptables, includeWorkerJSON bool) []string {
	lines := make([]string, 0, 8)
	if includeWorkerJSON {
		lines = append(lines, "+ worker.json\n")
	}
	lines = append(lines,
		"+ jobs.json\n",
		"+ bin/\n",
		"+ bin/**\n",
	)
	if includeIptables {
		lines = append(lines, "+ etc/\n", "+ etc/**\n")
	}
	lines = append(lines, "- *\n")
	return lines
}

func writeRsyncFilter(workerIP string, jobs []string, applyRules bool) (string, error) {
	lines := buildRsyncFilterLines(jobs, applyRules)

	mergePath := path.Join(bucket.TempLocation, "workers", workerIP+".rsync")
	if err := os.MkdirAll(path.Dir(mergePath), 0o755); err != nil {
		return "", bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(mergePath, []byte(strings.Join(lines, "")), 0o644); err != nil {
		return "", err
	}
	return mergePath, nil
}

func writeMetadataRsyncFilter(workerIP string, includeIptables, includeWorkerJSON bool) (string, error) {
	mergePath := path.Join(bucket.TempLocation, "workers", workerIP+".metadata.rsync")
	if err := os.MkdirAll(path.Dir(mergePath), 0o755); err != nil {
		return "", bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(mergePath, []byte(strings.Join(buildMetadataRsyncFilterLines(includeIptables, includeWorkerJSON), "")), 0o644); err != nil {
		return "", err
	}
	return mergePath, nil
}

func syncWorkerMetadata(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string, applyIptables bool) error {
	mkdirCmd := fmt.Sprintf("mkdir -p %s", bucket.WorkerBucketPath(bucketID))
	if err := runWorkerCommand(
		ctx,
		rt,
		workerIP,
		bucket.CommandContext{
			Phase:  "rsync",
			Action: "mkdir",
			Cmd:    mkdirCmd,
		},
		[]string{mkdirCmd},
		nil,
	); err != nil {
		return err
	}
	if err := runRsyncMetadata(ctx, rt, bucketID, workerIP); err != nil {
		return err
	}
	if !applyIptables {
		return nil
	}
	return applyWorkerIptables(ctx, rt, bucketID, workerIP)
}

func applyWorkerIptables(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string) error {
	script := iptables.ApplyScriptPath(bucketID)
	cmd := fmt.Sprintf("%s %s", script, bucketID)
	return runWorkerCommand(
		ctx,
		rt,
		workerIP,
		bucket.CommandContext{
			Phase:  "iptables",
			Action: "apply",
			Cmd:    cmd,
		},
		[]string{cmd},
		nil,
	)
}

func syncWorkersMetadata(ctx context.Context, rt *bucket.Runtime, bucketID string, workers []string, includeWorkerJSON bool) error {
	conf, err := bucket.LoadBucketSettings()
	if err != nil {
		return err
	}
	includeIptables := conf.Iptables

	filterFiles := make([]string, 0, len(workers))
	defer func() {
		for _, file := range filterFiles {
			_ = os.Remove(file)
		}
	}()

	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}

	for _, workerIP := range workers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		mergePath, err := writeMetadataRsyncFilter(workerIP, includeIptables, includeWorkerJSON)
		if err != nil {
			return err
		}
		filterFiles = append(filterFiles, mergePath)
	}

	limit, err := maxConcurrentSyncs()
	if err != nil {
		return err
	}
	err = runParallelWorkers(ctx, workers, limit, func(ip string) error {
		if err := syncWorkerMetadata(ctx, rt, bucketID, ip, includeIptables); err != nil {
			return fmt.Errorf("worker %s: %w", ip, err)
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("metadata rsync failed:\n%s", err.Error())
	}
	return nil
}

func maxConcurrentSyncs() (int, error) {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return 0, err
	}
	if conf.MaxConcurrentSyncs < 1 {
		return bucket.DefaultMaxConcurrentSyncs, nil
	}
	return conf.MaxConcurrentSyncs, nil
}
