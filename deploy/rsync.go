// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"kive/bucket"
	"kive/worker"
	"kive/workspace"
)

func jobRsyncExcludeArgs() []string {
	return []string{
		"--exclude=jobs/*/bin",
		"--exclude=jobs/*/certs",
		"--exclude=jobs/*/data",
		"--exclude=jobs/*/logs",
		// Host-local job dirs (_hooks, _prometheus, _docs, …) at any depth.
		"--exclude=_*/",
	}
}

func certsRsyncFilterArgs(jobs []string) []string {
	args := []string{"--include=/jobs/"}
	for _, job := range jobs {
		args = append(args,
			"--include=/jobs/"+job+"/",
			"--include=/jobs/"+job+"/certs/",
			"--include=/jobs/"+job+"/certs/***",
		)
	}
	return append(args, "--exclude=*")
}

func certsRsyncOptions() []string {
	return []string{
		"--timeout=30",
		"--inplace",
		"--whole-file",
		"--checksum",
		"--recursive",
		"--force",
		"--delete-after",
		"--delete",
		"--group",
		"--owner",
		"--perms",
		"--executability",
		"--compress",
		"--verbose",
	}
}

func stagedCertJobs(workerDir string, jobs []string) ([]string, error) {
	staged := make([]string, 0, len(jobs))
	for _, job := range jobs {
		certsDir := path.Join(workerDir, "jobs", job, "certs")
		info, err := os.Stat(certsDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat staged certs for job %s: %w", job, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("staged certs path for job %s is not a directory", job)
		}
		staged = append(staged, job)
	}
	return staged, nil
}

func rsync(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string, jobs []string) error {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}

	user := strings.TrimSpace(conf.SSHUser)
	if user == "" {
		user = "agent"
	}
	keyFilePath, _, err := bucket.SSHKeyHostPath(conf.SSHKeyFile)
	if err != nil {
		return err
	}

	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}

	remoteRS := "rsync"
	if conf.UseSUDO {
		remoteRS = "sudo rsync"
	}

	ruleFilePath, err := filepath.Abs(path.Join(bucket.TempLocation, "workers", fmt.Sprintf("%s.rsync", workerIP)))
	if err != nil {
		return err
	}
	workerDir, err := filepath.Abs(bucket.GetTempWorkerPath(workerIP))
	if err != nil {
		return err
	}

	args := []string{
		"--timeout=30",
		"--inplace",
		"--whole-file",
		"--checksum",
		"--recursive",
		"--force",
		"--delete-after",
		"--delete",
		"--group",
		"--owner",
		"--executability",
		"--compress",
		"--verbose",
	}
	args = append(args, jobRsyncExcludeArgs()...)
	for _, line := range workspace.JobRuntimeDirRsyncProtectFilterLines() {
		args = append(args, "--filter="+strings.TrimSpace(line))
	}
	for _, line := range bucket.WorkerBackupsRsyncProtectFilterLines() {
		args = append(args, "--filter="+strings.TrimSpace(line))
	}
	args = append(args,
		"--rsync-path="+remoteRS,
		"--filter=merge "+ruleFilePath,
		"--rsh="+worker.RSHShell(keyFilePath, workerIP),
		workerDir+string(filepath.Separator),
		fmt.Sprintf("%s@%s:%s", user, workerIP, bucket.WorkerBucketPath(bucketID)),
	)

	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmdCtx := bucket.CommandContext{
		Job:    strings.Join(jobs, ","),
		Phase:  "rsync",
		Action: "rsync",
		Cmd:    bucket.SummarizeExecCmd(cmd),
	}
	if err := rt.RunCommand(workerIP, cmdCtx, cmd); err != nil {
		return fmt.Errorf("rsync failed: worker %s: %w", workerIP, err)
	}
	return nil
}

func rsyncCerts(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string, jobs []string) error {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}

	user := strings.TrimSpace(conf.SSHUser)
	if user == "" {
		user = "agent"
	}
	keyFilePath, _, err := bucket.SSHKeyHostPath(conf.SSHKeyFile)
	if err != nil {
		return err
	}

	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}

	remoteRS := "rsync"
	if conf.UseSUDO {
		remoteRS = "sudo rsync"
	}

	workerDir, err := filepath.Abs(bucket.GetTempWorkerPath(workerIP))
	if err != nil {
		return err
	}
	jobs, err = stagedCertJobs(workerDir, jobs)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	args := certsRsyncOptions()
	args = append(args, certsRsyncFilterArgs(jobs)...)
	args = append(args,
		"--rsync-path="+remoteRS,
		"--rsh="+worker.RSHShell(keyFilePath, workerIP),
		workerDir+string(filepath.Separator),
		fmt.Sprintf("%s@%s:%s", user, workerIP, bucket.WorkerBucketPath(bucketID)),
	)

	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmdCtx := bucket.CommandContext{
		Job:    strings.Join(jobs, ","),
		Phase:  "rsync",
		Action: "certs_rsync",
		Cmd:    bucket.SummarizeExecCmd(cmd),
	}
	if err := rt.RunCommand(workerIP, cmdCtx, cmd); err != nil {
		return fmt.Errorf("certs rsync failed: worker %s: %w", workerIP, err)
	}
	return nil
}

func rsyncWorkerMetadata(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string) error {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}

	user := strings.TrimSpace(conf.SSHUser)
	if user == "" {
		user = "agent"
	}
	keyFilePath, _, err := bucket.SSHKeyHostPath(conf.SSHKeyFile)
	if err != nil {
		return err
	}

	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}

	remoteRS := "rsync"
	if conf.UseSUDO {
		remoteRS = "sudo rsync"
	}

	ruleFilePath, err := filepath.Abs(path.Join(bucket.TempLocation, "workers", fmt.Sprintf("%s.metadata.rsync", workerIP)))
	if err != nil {
		return err
	}
	workerDir, err := filepath.Abs(bucket.GetTempWorkerPath(workerIP))
	if err != nil {
		return err
	}

	args := []string{
		"--timeout=30",
		"--inplace",
		"--whole-file",
		"--checksum",
		"--recursive",
		"--force",
		"--group",
		"--owner",
		"--executability",
		"--compress",
		"--verbose",
		"--rsync-path=" + remoteRS,
		"--filter=merge " + ruleFilePath,
		"--rsh=" + worker.RSHShell(keyFilePath, workerIP),
		workerDir + string(filepath.Separator),
		fmt.Sprintf("%s@%s:%s", user, workerIP, bucket.WorkerBucketPath(bucketID)),
	}

	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmdCtx := bucket.CommandContext{
		Phase:  "rsync",
		Action: "metadata_rsync",
		Cmd:    bucket.SummarizeExecCmd(cmd),
	}
	if err := rt.RunCommand(workerIP, cmdCtx, cmd); err != nil {
		return fmt.Errorf("metadata rsync failed: worker %s: %w", workerIP, err)
	}
	return nil
}
