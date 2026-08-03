// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"encoding/json"
	"os"
	"path"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
	"kive/iptables"
)

func getWorkerData(tx *sql.Tx, workerIP string) (WorkerData, error) {
	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return WorkerData{}, err
	}

	workerID, err := data.GetWorkerID(tx, workerIP)
	if err != nil {
		return WorkerData{}, err
	}

	labels, err := data.GetWorkerLabels(tx, workerID)
	if err != nil {
		return WorkerData{}, err
	}

	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return WorkerData{}, err
	}

	return WorkerData{
		BucketID:   bucketID,
		WorkerID:   workerID,
		WorkerIP:   workerIP,
		Labels:     labels,
		Generation: generation,
	}, nil
}

func prepareWorkersFiles(tx *sql.Tx, workers []string) error {
	for _, workerIP := range workers {
		if err := prepareOneWorkerFiles(tx, workerIP); err != nil {
			return err
		}
	}
	return nil
}

func prepareOneWorkerFiles(tx *sql.Tx, workerIP string) error {
	workerDirPath := bucket.GetTempWorkerPath(workerIP)
	if err := os.MkdirAll(workerDirPath, 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}

	deployableWorker, err := getWorkerData(tx, workerIP)
	if err != nil {
		return err
	}

	workerData, err := json.MarshalIndent(deployableWorker, "", "   ")
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(path.Join(workerDirPath, "worker.json"), workerData, 0o644); err != nil {
		return bucket.UnexpectedError(err)
	}

	workerJobs, err := buildWorkerJobsList(tx, workerIP)
	if err != nil {
		return err
	}

	workerJobsData, err := json.MarshalIndent(workerJobs, "", "   ")
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(path.Join(workerDirPath, "jobs.json"), workerJobsData, 0o644); err != nil {
		return bucket.UnexpectedError(err)
	}

	if err := os.MkdirAll(path.Join(workerDirPath, "bin"), 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(path.Join(workerDirPath, "bin", "runner.py"), runnerPy, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(workerDirPath, "bin", "worker.py"), workerPy, 0o644); err != nil {
		return err
	}

	conf, err := bucket.LoadBucketSettings()
	if err != nil {
		return err
	}
	if conf.Iptables {
		if err := os.WriteFile(path.Join(workerDirPath, "bin", "apply_iptables"), applyIptablesSh, 0o755); err != nil {
			return bucket.UnexpectedError(err)
		}
		if err := iptables.StageWorkerRules(tx, deployableWorker.BucketID, workerIP); err != nil {
			return err
		}
	}

	return os.MkdirAll(path.Join(workerDirPath, "jobs"), 0o755)
}

func prepareJobsFiles(tx *sql.Tx, jobs []string) error {
	for _, job := range jobs {
		workers, err := data.GetNonRemovedAllocations(tx, job)
		if err != nil {
			return err
		}
		for _, workerIP := range workers {
			if err := prepareJobOnWorker(tx, job, workerIP); err != nil {
				return err
			}
		}
	}
	return nil
}

func prepareJobOnWorker(tx *sql.Tx, job, workerIP string) error {
	workerDirPath := bucket.GetTempWorkerPath(workerIP)

	if err := data.CopyJobFiles(tx, job, path.Join(workerDirPath, "jobs")); err != nil {
		return err
	}

	hooksDir := path.Join(workerDirPath, "jobs", job, "_hooks")
	if _, err := os.Stat(hooksDir); err == nil {
		if err := os.WriteFile(path.Join(hooksDir, "kive.py"), hooks.KivePy, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(path.Join(hooksDir, "kive.ts"), hooks.KiveTS, 0o644); err != nil {
			return err
		}
	}

	// Stage certs before template render so a template failure does not leave
	// workers without TLS material if a later path still syncs this tree.
	if err := updateCerts(tx, job, workerIP); err != nil {
		return err
	}
	if err := transpile(tx, job, workerIP); err != nil {
		return err
	}
	hasPrometheusConfig, err := data.JobHasPrometheusServerConfig(tx, job)
	if err != nil {
		return err
	}
	if hasPrometheusConfig {
		prometheusJobDir := path.Join(workerDirPath, "jobs", job)
		if err := assemblePrometheusAlertRules(tx, job, prometheusJobDir, workerIP); err != nil {
			return err
		}
	}
	return nil
}
