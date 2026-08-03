// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"sync"

	"kive/data"
)

type allocationState struct {
	liveness  string
	readiness string
	status    string
	detail    string
}

// statusRecorder tracks per-worker health results during one job check.
type statusRecorder struct {
	mu    sync.Mutex
	byIP  map[string]*allocationState
	kinds map[string]struct{}
}

func newStatusRecorder() *statusRecorder {
	return &statusRecorder{
		byIP:  map[string]*allocationState{},
		kinds: map[string]struct{}{},
	}
}

func (r *statusRecorder) noteKind(kind string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.kinds[kind] = struct{}{}
	r.mu.Unlock()
}

func (r *statusRecorder) passWorker(workerIP, kind string) {
	if r == nil || workerIP == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.byIP[workerIP]
	if st == nil {
		st = &allocationState{status: data.HealthStatusHealthy}
		r.byIP[workerIP] = st
	}
	switch kind {
	case "liveness":
		st.liveness = data.HealthKindPass
	case "readiness":
		st.readiness = data.HealthKindPass
	}
	if st.status != data.HealthStatusUnhealthy {
		st.status = data.HealthStatusHealthy
	}
}

func (r *statusRecorder) failWorker(workerIP, kind, detail string) {
	if r == nil || workerIP == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.byIP[workerIP]
	if st == nil {
		st = &allocationState{}
		r.byIP[workerIP] = st
	}
	st.status = data.HealthStatusUnhealthy
	st.detail = detail
	switch kind {
	case "liveness":
		st.liveness = data.HealthKindFail
	case "readiness":
		st.readiness = data.HealthKindFail
	default:
		st.liveness = data.HealthKindFail
	}
}

func (r *statusRecorder) markSkippedWorkers(workerIPs []string) {
	if r == nil {
		return
	}
	for _, ip := range workerIPs {
		r.mu.Lock()
		st := r.byIP[ip]
		if st == nil {
			st = &allocationState{status: data.HealthStatusHealthy}
			r.byIP[ip] = st
		}
		if st.liveness == "" {
			st.liveness = data.HealthKindSkip
		}
		if st.readiness == "" {
			st.readiness = data.HealthKindSkip
		}
		r.mu.Unlock()
	}
}

func (r *statusRecorder) finalizeSuccess(workerIPs []string) {
	if r == nil {
		return
	}
	for _, ip := range workerIPs {
		r.passWorker(ip, "liveness")
		r.passWorker(ip, "readiness")
	}
}

func (r *statusRecorder) workerState(workerIP string) (allocationState, bool) {
	if r == nil {
		return allocationState{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.byIP[workerIP]
	if !ok || st == nil {
		return allocationState{}, false
	}
	return *st, true
}
