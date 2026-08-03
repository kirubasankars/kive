// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/kv"
)

const (
	embeddedKivePyModule = "kive.py"
	embeddedKiveTSModule = "kive.ts"
	embeddedKiveRBModule = "kive.rb"
	embeddedKiveSHModule = "kive.sh"
	certsSubdir           = "certs"
	livenessEvent         = "liveness"
	readinessEvent        = "readiness"
)

func isHealthProbeEvent(event string) bool {
	return event == livenessEvent || event == readinessEvent
}

func prepareWorkerWorkspaces(tx *sql.Tx, jobName string, workerIPs []string, event, hookName string) error {
	store := kv.GetKVStore()
	if store == nil {
		return bucket.UnexpectedError(fmt.Errorf("kv store not initialized"))
	}

	for _, workerIP := range workerIPs {
		var err error
		if isHealthProbeEvent(event) {
			err = prepareHealthWorkerWorkspace(tx, jobName, hookName, workerIP, store)
		} else {
			err = prepareOneWorkerWorkspace(tx, jobName, workerIP, store)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func prepareOneWorkerWorkspace(tx *sql.Tx, jobName, workerIP string, store *kv.Store) error {
	workerDir := bucket.GetTempWorkerPath(workerIP)
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		return err
	}

	jobRoot := path.Join(workerDir, "jobs")
	if err := data.CopyJobFiles(tx, jobName, jobRoot); err != nil {
		return err
	}

	hooksDir := path.Join(jobRoot, jobName, "_hooks")
	return writeCommandSupportFiles(jobName, workerIP, hooksDir, store)
}

func prepareHealthWorkerWorkspace(tx *sql.Tx, jobName, hookName, workerIP string, store *kv.Store) error {
	workerDir := bucket.GetTempWorkerPath(workerIP)
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		return err
	}

	jobRoot := path.Join(workerDir, "jobs")
	if err := data.CopyHookModule(tx, jobName, hookName, jobRoot); err != nil {
		return err
	}
	if err := data.CopyHookLibModules(tx, jobName, jobRoot); err != nil {
		return err
	}

	hooksDir := path.Join(jobRoot, jobName, "_hooks")
	return writeCommandSupportFiles(jobName, workerIP, hooksDir, store)
}

func writeCommandSupportFiles(jobName, workerIP, hooksDir string, store *kv.Store) error {
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(hooksDir, embeddedKivePyModule), KivePy, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(hooksDir, embeddedKiveTSModule), KiveTS, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(hooksDir, embeddedKiveRBModule), KiveRB, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(hooksDir, embeddedKiveSHModule), KiveSH, 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(path.Join(hooksDir, certsSubdir), 0o755); err != nil {
		return err
	}
	return syncWorkerCertificates(jobName, workerIP, hooksDir, store)
}

func syncWorkerCertificates(jobName, workerIP, hooksDir string, store *kv.Store) error {
	workerKVNamespace := fmt.Sprintf("kive/job/%s/worker/%s", jobName, workerIP)

	keys, err := store.GetKeys(workerKVNamespace)
	if err != nil {
		return err
	}

	for _, key := range keys {
		if !strings.HasPrefix(key, certsSubdir+"/") {
			continue
		}

		item, err := store.Get(workerKVNamespace, key)
		if err != nil {
			return err
		}

		destPath := path.Join(hooksDir, key)
		perm := os.FileMode(0o644)
		if strings.HasSuffix(key, ".key") {
			perm = 0o600
		}
		if err := os.WriteFile(destPath, []byte(item.Value), perm); err != nil {
			return err
		}
	}

	caPath := path.Join(hooksDir, certsSubdir, "ca-trust.crt")
	if _, err := os.Stat(caPath); err == nil {
		return nil
	}

	caItem, err := store.Get("kive/worker", "certs/ca-trust.crt")
	if err != nil {
		return err
	}

	if err := os.WriteFile(caPath, []byte(caItem.Value), 0o644); err != nil {
		return err
	}
	_ = os.Remove(path.Join(hooksDir, certsSubdir, "ca.crt"))
	return nil
}
