// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

type runtimeAPIContext struct {
	gate       *txGate
	semaphores *semaphoreCoordinator
	token      string
}

func newRuntimeAPIMux(apiCtx *runtimeAPIContext) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(RouteStoreKeys, serveStoreKeys(apiCtx.gate))
	mux.HandleFunc(RouteStoreKeysList, serveStoreKeysList(apiCtx.gate))
	mux.HandleFunc(RouteStoreSecret, serveStoreSecret(apiCtx.gate))
	mux.HandleFunc(RouteDemands, serveCommandDemands(apiCtx.gate))
	mux.HandleFunc(RouteSemaphoreAcquire, serveSemaphoreAcquire(apiCtx))
	mux.HandleFunc(RouteSemaphoreRelease, serveSemaphoreRelease(apiCtx))
	mux.HandleFunc(RouteSemaphoreStatus, serveSemaphoreStatus(apiCtx))
	mux.HandleFunc(RouteHTTP, serveHTTPProxy(apiCtx))
	return mux
}

func serveSemaphoreAcquire(apiCtx *runtimeAPIContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		jobName, _, allocationID, body, err := resolveAllocationFromRequestWithID(w, r, apiCtx.gate)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload semaphoreAcquirePayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}
		if payload.Name == "" {
			runtimeAPIErrors.missingKeyFields.write(w)
			return
		}

		timeout := normalizeAcquireTimeout(payload.TimeoutSeconds)
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Now().Add(timeout + 5*time.Second))

		scopeKey := semaphoreScopeKey(jobName, r.Header.Get(HeaderHookEvent), payload.Name)
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := apiCtx.semaphores.acquire(ctx, scopeKey, allocationID, payload.Capacity); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				log.Printf("runtime api semaphore acquire timeout: job=%s name=%s alloc=%s", jobName, payload.Name, allocationID)
				runtimeAPIErrors.semaphoreTimeout.write(w)
				return
			}
			log.Printf("runtime api semaphore acquire: %v", err)
			runtimeAPIErrors.semaphoreConflict.write(w)
			return
		}

		status, _ := apiCtx.semaphores.status(scopeKey)
		writeJSONResponse(w, http.StatusOK, semaphoreAcquireResponse{
			Name:         payload.Name,
			AllocationID: allocationID,
			Capacity:     status.Capacity,
			Acquired:     true,
		})
	}
}

func serveSemaphoreRelease(apiCtx *runtimeAPIContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		jobName, _, allocationID, body, err := resolveAllocationFromRequestWithID(w, r, apiCtx.gate)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		var payload semaphoreReleasePayload
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			writeJSONDecodeError(w, err)
			return
		}
		if payload.Name == "" {
			runtimeAPIErrors.missingKeyFields.write(w)
			return
		}

		scopeKey := semaphoreScopeKey(jobName, r.Header.Get(HeaderHookEvent), payload.Name)
		if err := apiCtx.semaphores.release(scopeKey, allocationID); err != nil {
			log.Printf("runtime api semaphore release: %v", err)
			runtimeAPIErrors.semaphoreConflict.write(w)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func serveSemaphoreStatus(apiCtx *runtimeAPIContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		jobName, _, _, _, err := resolveAllocationFromRequestWithID(w, r, apiCtx.gate)
		if err != nil {
			return
		}

		semaphoreName := r.URL.Query().Get("name")
		if semaphoreName == "" {
			runtimeAPIErrors.missingKeyFields.write(w)
			return
		}

		scopeKey := semaphoreScopeKey(jobName, r.Header.Get(HeaderHookEvent), semaphoreName)
		status, found := apiCtx.semaphores.status(scopeKey)
		status.Name = semaphoreName
		if !found {
			status.Capacity = defaultSemaphoreCapacity
			status.Available = defaultSemaphoreCapacity
		}

		writeJSONResponse(w, http.StatusOK, status)
	}
}
