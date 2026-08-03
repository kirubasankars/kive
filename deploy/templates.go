// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"

	"kive/bucket"
	"kive/data"
	"kive/kv"
	"kive/promconfig"
	"kive/utils"
)

func transpile(tx *sql.Tx, job, workerIP string) error {
	workerDir := bucket.GetTempWorkerPath(workerIP)
	jobDir := path.Join(workerDir, "jobs", job)

	var jobTemplates []string
	err := fs.WalkDir(os.DirFS(jobDir), ".", func(relPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".tpl") {
			jobTemplates = append(jobTemplates, relPath)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(jobTemplates) == 0 {
		return nil
	}

	allowedNamespaces, err := data.AllowedKVNamespacesWithUpstream(tx, job, workerIP)
	if err != nil {
		return err
	}
	funcMap := templateFuncMap(tx, job, workerIP, allowedNamespaces)

	workerData, err := getWorkerData(tx, workerIP)
	if err != nil {
		return err
	}

	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return err
	}

	versions, err := allocationVersionsForWorker(tx, job, workerIP)
	if err != nil {
		return err
	}

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	templateData := AllocationData{
		AllocationID:        allocID,
		Job:                 job,
		CurrentVersion:      versions.AppliedVersion,
		NewVersion:          versions.Version,
		CurrentMajorVersion: versionMajor(versions.AppliedVersion),
		NewMajorVersion:     versionMajor(versions.Version),
		WorkerData:          workerData,
		BucketPath:          bucket.WorkerBucketPath(bucketID),
		JobPath:             bucket.WorkerJobPath(bucketID, job),
	}

	for _, jobTemplate := range jobTemplates {
		if err := renderTemplate(jobDir, jobTemplate, funcMap, templateData); err != nil {
			return err
		}
	}
	return nil
}

func templateFuncMap(tx *sql.Tx, job, workerIP string, allowedNamespaces []string) template.FuncMap {
	reader := &kv.TemplateReader{
		Store:             kv.GetKVStore(),
		AllowedNamespaces: allowedNamespaces,
		Job:               job,
		WorkerIP:          workerIP,
	}
	return template.FuncMap{
		"get":           reader.Get,
		"getOptional":   reader.GetOptional,
		"getJob":        reader.GetJob,
		"getJobOptional": reader.GetJobOptional,
		"getJobWorker":  reader.GetJobWorker,
		"getJobWorkerOptional": reader.GetJobWorkerOptional,
		"getWorker":     reader.GetWorker,
		"getWorkerOptional": reader.GetWorkerOptional,
		"keys": func(ns string) []string {
			if len(utils.Difference([]string{ns}, allowedNamespaces)) > 0 {
				panic(fmt.Sprintf("%s namespace is not available for job %s", ns, job))
			}
			value, err := reader.Store.GetKeys(ns)
			if err != nil {
				panic(err)
			}
			return value
		},
		"getSecret": func(key string) string {
			ns := kv.SecretJobNamespace(job)
			value, err := reader.Store.GetSecret(ns, key)
			if err != nil {
				panic(err)
			}
			return value
		},
		"split": strings.Split,
		"trim":  strings.TrimSpace,
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"join":  strings.Join,
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"div": func(a, b int) int { return a / b },
		"min": func(a, b int) int {
			if a < b {
				return a
			}
			return b
		},
		"max": func(a, b int) int {
			if a > b {
				return a
			}
			return b
		},
		"int": templateInt,
		"scrapeConfigs": func() string {
			if len(utils.Difference([]string{promconfig.KVNamespace}, allowedNamespaces)) > 0 {
				panic(fmt.Sprintf("%s namespace is not available for job %s", promconfig.KVNamespace, job))
			}
			yamlFragment, err := promconfig.RenderScrapeConfigsYAML(tx, nil)
			if err != nil {
				panic(err)
			}
			return yamlFragment
		},
		"ruleFiles": func() string {
			yamlFragment, err := promconfig.RenderRuleFilesYAML(tx)
			if err != nil {
				panic(err)
			}
			return yamlFragment
		},
	}
}

func templateInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(math.Round(n))
	case float32:
		return int(math.Round(float64(n)))
	case string:
		s := strings.TrimSpace(n)
		if i, err := strconv.Atoi(s); err == nil {
			return i
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			panic(err)
		}
		return int(math.Round(f))
	default:
		panic("int: expected string, integer, or float")
	}
}

func renderTemplate(jobDir, jobTemplate string, funcMap template.FuncMap, data AllocationData) error {
	templateContent, err := os.ReadFile(path.Join(jobDir, jobTemplate))
	if err != nil {
		return err
	}

	tmpl, err := template.New("template").Funcs(funcMap).Parse(string(templateContent))
	if err != nil {
		return fmt.Errorf("parse template %s: %w", jobTemplate, err)
	}

	outPath := strings.TrimSuffix(jobTemplate, path.Ext(jobTemplate))
	file, err := os.Create(path.Join(jobDir, outPath))
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("execute template %s: %w", jobTemplate, err)
	}
	return file.Close()
}
