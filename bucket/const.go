// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"os"
	"path"
)

// EnvBucketRoot is the environment variable that overrides bucket root discovery.
const EnvBucketRoot = "BUCKET_ROOT"

// WorkerInstallRoot is the base directory on workers where bucket trees are deployed.
const WorkerInstallRoot = "/opt/kive"

var (
	Location           = "."
	WorkspaceLocation  = path.Join(Location, "workspace")
	CommandsLocation   = path.Join(WorkspaceLocation, "commands")
	TemplatesLocation  = path.Join(Location, "templates")
	SecretLocation     = path.Join(Location, "secrets")
	TempLocation       = path.Join(Location, "tmp")
	LogLocation        = path.Join(Location, "logs")
	RunLogLocation     = path.Join(LogLocation, "runs")
)

func UpdatePath() {
	WorkspaceLocation = path.Join(Location, "workspace")
	CommandsLocation = path.Join(WorkspaceLocation, "commands")
	TemplatesLocation = path.Join(Location, "templates")
	SecretLocation = path.Join(Location, "secrets")
	TempLocation = path.Join(Location, "tmp")
	LogLocation = path.Join(Location, "logs")
	RunLogLocation = path.Join(LogLocation, "runs")
}

func GetTempWorkerPath(workerIP string) string {
	return path.Join(TempLocation, "workers", workerIP)
}

// PruneTempDir drops the bucket tmp directory once the command that created it
// has cleaned up its own scratch files, so a finished command leaves no empty
// tmp/ behind. Anything still staged there (a concurrent health sample, for
// example) keeps the directory, so this is safe to call unconditionally.
func PruneTempDir() {
	_ = os.Remove(TempLocation)
}

// WorkerBucketPath returns the deployed bucket directory on a worker.
func WorkerBucketPath(bucketID string) string {
	return path.Join(WorkerInstallRoot, bucketID)
}

// WorkerJobPath returns the deployed job directory on a worker.
func WorkerJobPath(bucketID, job string) string {
	return path.Join(WorkerInstallRoot, bucketID, "jobs", job)
}

// WorkerBackupsPath returns the catalog DB backup directory on a worker.
func WorkerBackupsPath(bucketID string) string {
	return path.Join(WorkerBucketPath(bucketID), "backups")
}

// WorkerBackupsRsyncProtectFilterLines returns rsync protect (P) rules so --delete
// does not remove /opt/kive/<bucket_id>/backups/ on workers.
func WorkerBackupsRsyncProtectFilterLines() []string {
	return []string{
		"P backups/\n",
		"P backups/**\n",
	}
}
