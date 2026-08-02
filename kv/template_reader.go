// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package kv

import (
	"errors"
	"fmt"

	"kive/utils"
)

// TemplateReader resolves KV values for Go text/template helpers with namespace ACL checks.
type TemplateReader struct {
	Store             *Store
	AllowedNamespaces []string
	Job               string
	WorkerIP          string
	ContextLabel      string
}

// Get reads a key from an allowed namespace and panics when missing or disallowed.
func (r *TemplateReader) Get(namespace, key string) string {
	return r.get(namespace, key, false)
}

// GetOptional reads a key from an allowed namespace and returns "" when missing.
func (r *TemplateReader) GetOptional(namespace, key string) string {
	return r.get(namespace, key, true)
}

// GetJob reads kive/job/<job>. One argument uses the rendering job; two arguments use an explicit job name.
func (r *TemplateReader) GetJob(args ...string) string {
	targetJob, key := r.resolveJobArgs(args, "getJob")
	return r.Get(JobCatalogNamespace(targetJob), key)
}

// GetJobOptional is like GetJob but returns "" when the key is missing.
func (r *TemplateReader) GetJobOptional(args ...string) string {
	targetJob, key := r.resolveJobArgs(args, "getJobOptional")
	return r.GetOptional(JobCatalogNamespace(targetJob), key)
}

// GetJobWorker reads kive/job/<render job>/worker/<render worker ip>.
func (r *TemplateReader) GetJobWorker(key string) string {
	return r.Get(JobWorkerNamespace(r.Job, r.WorkerIP), key)
}

// GetJobWorkerOptional is like GetJobWorker but returns "" when the key is missing.
func (r *TemplateReader) GetJobWorkerOptional(key string) string {
	return r.GetOptional(JobWorkerNamespace(r.Job, r.WorkerIP), key)
}

// GetWorker reads kive/worker/<render worker ip>.
func (r *TemplateReader) GetWorker(key string) string {
	return r.Get(WorkerCatalogNamespace(r.WorkerIP), key)
}

// GetWorkerOptional is like GetWorker but returns "" when the key is missing.
func (r *TemplateReader) GetWorkerOptional(key string) string {
	return r.GetOptional(WorkerCatalogNamespace(r.WorkerIP), key)
}

func (r *TemplateReader) resolveJobArgs(args []string, funcName string) (job, key string) {
	switch len(args) {
	case 1:
		return r.Job, args[0]
	case 2:
		return args[0], args[1]
	default:
		panic(fmt.Sprintf("%s: expected 1 or 2 arguments", funcName))
	}
}

func (r *TemplateReader) get(namespace, key string, optional bool) string {
	if len(utils.Difference([]string{namespace}, r.AllowedNamespaces)) > 0 {
		panic(fmt.Sprintf("%s namespace is not available for job %s", namespace, r.contextJob()))
	}
	if IsSecretNamespace(namespace) {
		value, err := r.Store.GetSecret(namespace, key)
		if err != nil {
			if optional && errors.Is(err, ErrNotFound) {
				return ""
			}
			panic(err)
		}
		return value
	}
	value, err := r.Store.Get(namespace, key)
	if err != nil {
		if optional && errors.Is(err, ErrNotFound) {
			return ""
		}
		panic(err)
	}
	return value.Value
}

func (r *TemplateReader) contextJob() string {
	if r.ContextLabel != "" {
		return r.ContextLabel
	}
	return r.Job
}
