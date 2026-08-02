// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"kive/bucket"
	"kive/kv"
)

func serveStoreKeys(gate *txGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleStoreKeyGet(w, r, gate)
		case http.MethodPut:
			handleStoreKeyPut(w, r, gate)
		case http.MethodDelete:
			handleStoreKeyDelete(w, r, gate)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func serveStoreKeysList(gate *txGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleStoreKeysList(w, r, gate)
	}
}

func serveStoreSecret(gate *txGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			handleStoreSecretPut(w, r, gate)
		case http.MethodDelete:
			handleStoreSecretDelete(w, r, gate)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func serveCommandDemands(gate *txGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCommandDemandsGet(w, r, gate)
	}
}

// resolveAllocationFromRequestWithID loads job and worker for the allocation
// named in X-ALLOCATION-ID, serializing Tx access through gate.
func resolveAllocationFromRequestWithID(
	w http.ResponseWriter,
	r *http.Request,
	gate *txGate,
) (jobName, workerIP, allocationID string, body io.ReadCloser, err error) {
	allocationID = r.Header.Get(HeaderAllocationID)
	if allocationID == "" {
		runtimeAPIErrors.missingAllocation.write(w)
		return "", "", "", nil, fmt.Errorf("missing header %s", HeaderAllocationID)
	}

	err = gate.Do(func(tx *sql.Tx) error {
		return tx.QueryRow(
			`SELECT job, worker_ip FROM allocations WHERE alloc_id = ?`,
			allocationID,
		).Scan(&jobName, &workerIP)
	})
	if errors.Is(err, sql.ErrNoRows) {
		runtimeAPIErrors.unknownAllocation.write(w)
		return "", "", "", nil, fmt.Errorf("unknown allocation %s", allocationID)
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return "", "", "", nil, bucket.DatabaseError(err)
	}

	return jobName, workerIP, allocationID, r.Body, nil
}

// withGateTx runs fn while holding the gate lock so multi-statement Tx use
// (e.g. Query + Scan rows) stays single-threaded.
func withGateTx(gate *txGate, fn func(tx *sql.Tx)) {
	if gate == nil {
		fn(nil)
		return
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	fn(gate.tx)
}

func resolveAllocationUnlocked(
	w http.ResponseWriter,
	r *http.Request,
	tx *sql.Tx,
) (jobName, workerIP, allocationID string, body io.ReadCloser, err error) {
	allocationID = r.Header.Get(HeaderAllocationID)
	if allocationID == "" {
		runtimeAPIErrors.missingAllocation.write(w)
		return "", "", "", nil, fmt.Errorf("missing header %s", HeaderAllocationID)
	}

	err = tx.QueryRow(
		`SELECT job, worker_ip FROM allocations WHERE alloc_id = ?`,
		allocationID,
	).Scan(&jobName, &workerIP)
	if errors.Is(err, sql.ErrNoRows) {
		runtimeAPIErrors.unknownAllocation.write(w)
		return "", "", "", nil, fmt.Errorf("unknown allocation %s", allocationID)
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return "", "", "", nil, bucket.DatabaseError(err)
	}

	return jobName, workerIP, allocationID, r.Body, nil
}

func handleStoreKeyGet(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, workerIP, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload storeKeyPayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}

		if apiErr := validateStoreKeyPayload(tx, payload, jobName, workerIP, r.Header.Get(HeaderHookEvent), false); apiErr != nil {
			apiErr.write(w)
			return
		}

		item, err := kv.GetKVStore().Get(payload.Namespace, payload.Key)
		if err != nil {
			log.Printf("runtime api store get miss: namespace=%s key=%s err=%v", payload.Namespace, payload.Key, err)
			runtimeAPIErrors.storeKeyNotFound.write(w)
			return
		}

		value := item.Value
		if kv.IsSecretNamespace(payload.Namespace) {
			plaintext, decryptErr := kv.DecryptStoredValue(item.Value)
			if decryptErr != nil {
				log.Printf("runtime api store get decrypt: %v", decryptErr)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			value = plaintext
		}

		writeJSONResponse(w, http.StatusOK, storeKeyPayload{
			Namespace: payload.Namespace,
			Key:       payload.Key,
			Value:     value,
		})
	})
}

func handleStoreKeyPut(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, workerIP, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload storeKeyPayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}

		if apiErr := validateStoreKeyPayload(tx, payload, jobName, workerIP, r.Header.Get(HeaderHookEvent), true); apiErr != nil {
			apiErr.write(w)
			return
		}

		if kv.IsSecretNamespace(payload.Namespace) {
			runtimeAPIErrors.namespaceDenied.write(w)
			return
		}

		if isHealthProbeEvent(r.Header.Get(HeaderHookEvent)) {
			log.Printf("runtime api: blocked store put during health_check for job %s", jobName)
			runtimeAPIErrors.writeDuringHealth.write(w)
			return
		}

		kv.GetKVStore().Put(payload.Namespace, payload.Key, payload.Value, payload.TTL)
		w.WriteHeader(http.StatusOK)
	})
}

func handleStoreKeyDelete(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, workerIP, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload storeKeyPayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}

		if payload.Namespace == "" || payload.Key == "" {
			runtimeAPIErrors.missingKeyFields.write(w)
			return
		}

		if kv.IsSecretNamespace(payload.Namespace) {
			runtimeAPIErrors.namespaceDenied.write(w)
			return
		}

		expectedNamespace := fmt.Sprintf("vars/job/%s", jobName)
		if payload.Namespace != expectedNamespace {
			runtimeAPIErrors.namespaceDenied.write(w)
			return
		}

		if apiErr := validateReadNamespace(tx, payload.Namespace, jobName, workerIP); apiErr != nil {
			apiErr.write(w)
			return
		}

		if blockedWriteDuringHealthCheck(w, r, jobName) {
			return
		}

		if err := kv.GetKVStore().Delete(payload.Namespace, payload.Key); err != nil {
			if errors.Is(err, kv.ErrNotFound) {
				runtimeAPIErrors.storeKeyNotFound.write(w)
				return
			}
			log.Printf("runtime api store delete: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func handleStoreKeysList(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, workerIP, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload storeKeyPayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}

		namespaces := jobLevelKVNamespaces(jobName)
		if payload.Namespace != "" {
			if !isJobLevelListNamespace(payload.Namespace, jobName) {
				runtimeAPIErrors.namespaceDenied.write(w)
				return
			}
			if apiErr := validateReadNamespace(tx, payload.Namespace, jobName, workerIP); apiErr != nil {
				apiErr.write(w)
				return
			}
			namespaces = []string{payload.Namespace}
		}

		store := kv.GetKVStore()
		result := make(map[string][]string, len(namespaces))
		for _, namespace := range namespaces {
			keys, err := store.GetKeys(namespace)
			if err != nil {
				log.Printf("runtime api store list keys: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			result[namespace] = keys
		}

		writeJSONResponse(w, http.StatusOK, listKeysResponse{Namespaces: result})
	})
}

func handleStoreSecretPut(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, workerIP, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload storeKeyPayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}

		if apiErr := validateStoreSecretPayload(payload, jobName, workerIP); apiErr != nil {
			apiErr.write(w)
			return
		}

		if isHealthProbeEvent(r.Header.Get(HeaderHookEvent)) {
			log.Printf("runtime api: blocked secret put during health_check for job %s", jobName)
			runtimeAPIErrors.writeDuringHealth.write(w)
			return
		}

		if err := kv.GetKVStore().PutSecret(payload.Namespace, payload.Key, payload.Value, payload.TTL); err != nil {
			log.Printf("runtime api store secret put: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func handleStoreSecretDelete(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, workerIP, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload storeKeyPayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}

		if apiErr := validateStoreSecretKeyPayload(payload, jobName, workerIP); apiErr != nil {
			apiErr.write(w)
			return
		}

		if blockedWriteDuringHealthCheck(w, r, jobName) {
			return
		}

		if err := kv.GetKVStore().Delete(payload.Namespace, payload.Key); err != nil {
			if errors.Is(err, kv.ErrNotFound) {
				runtimeAPIErrors.storeKeyNotFound.write(w)
				return
			}
			log.Printf("runtime api store secret delete: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func blockedWriteDuringHealthCheck(w http.ResponseWriter, r *http.Request, jobName string) bool {
	if !isHealthProbeEvent(r.Header.Get(HeaderHookEvent)) {
		return false
	}
	log.Printf("runtime api: blocked kv write during health_check for job %s", jobName)
	runtimeAPIErrors.writeDuringHealth.write(w)
	return true
}

func jobLevelKVNamespaces(jobName string) []string {
	return []string{
		fmt.Sprintf("vars/job/%s", jobName),
		kv.SecretJobNamespace(jobName),
	}
}

func handleCommandDemandsGet(w http.ResponseWriter, r *http.Request, gate *txGate) {
	withGateTx(gate, func(tx *sql.Tx) {
		jobName, _, _, body, err := resolveAllocationUnlocked(w, r, tx)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		hookName := r.Header.Get(HeaderHookName)
		rows, err := tx.Query(`
		SELECT job AS requester_job,
		       name AS requester_hook,
		       demand_config
		FROM hooks
		WHERE demand_job = ? AND demand_hook = ?`,
			jobName, hookName,
		)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer func() {
			_ = rows.Close()
		}()

		demands := make([]hookDemandPayload, 0)
		for rows.Next() {
			var entry hookDemandPayload
			var configJSON string
			if err := rows.Scan(&entry.Job, &entry.Hook, &configJSON); err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			if err := json.Unmarshal([]byte(configJSON), &entry.DemandConfig); err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			demands = append(demands, entry)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		writeJSONResponse(w, http.StatusOK, demands)
	})
}
