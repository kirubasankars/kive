// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package promconfig

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"

	"kive/bucket"
	"kive/data"
	"kive/kv"
	"kive/utils"
	"kive/workspace"
)

// ScrapeTemplateData is the template context for _prometheus/scrape.yaml.tpl at build time.
// Per-allocation fields such as WorkerIP are not available; use kive:port/* for targets.
type ScrapeTemplateData struct {
	Job            string
	CurrentVersion string
	NewVersion     string
}

// ValidateScrapeFiles ensures a job does not define conflicting scrape config sources.
func ValidateScrapeFiles(jobName string) error {
	plainRel := path.Join(jobName, "_prometheus", ScrapeFileName)
	tplRel := path.Join(jobName, "_prometheus", ScrapeFileTplName)
	plainExists := scrapeFileExists(workspace.JobFilePath(plainRel))
	tplExists := scrapeFileExists(workspace.JobFilePath(tplRel))
	if plainExists && tplExists {
		return fmt.Errorf(
			"%w: job %s has conflicting template sources for %s: %s, %s",
			bucket.ErrInvalidJob, jobName, ScrapeFileName, plainRel, tplRel,
		)
	}
	return nil
}

func scrapeFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadScrapeFileContent loads scrape.yaml or renders scrape.yaml.tpl at build/catalog time.
func ReadScrapeFileContent(tx *sql.Tx, jobName string) ([]byte, error) {
	if err := ValidateScrapeFiles(jobName); err != nil {
		return nil, err
	}

	tplPath := JobScrapeTplPath(jobName)
	if scrapeFileExists(tplPath) {
		tplContent, err := os.ReadFile(tplPath)
		if err != nil {
			return nil, err
		}
		return RenderScrapeTemplate(tx, jobName, tplContent)
	}

	yamlPath := JobScrapePath(jobName)
	content, err := os.ReadFile(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, err
	}
	return content, nil
}

// RenderScrapeTemplate executes scrape.yaml.tpl with job-level template context.
func RenderScrapeTemplate(tx *sql.Tx, jobName string, tplContent []byte) ([]byte, error) {
	allowedNamespaces, err := data.BuildScrapeTemplateNamespaces(tx, jobName)
	if err != nil {
		return nil, err
	}

	version, err := data.GetJobVersion(tx, jobName)
	if err != nil {
		return nil, err
	}
	normalizedVersion := data.NormalizeDeployVersion(version)

	templateData := ScrapeTemplateData{
		Job:            jobName,
		CurrentVersion: normalizedVersion,
		NewVersion:     normalizedVersion,
	}

	funcMap := scrapeTemplateFuncMap(jobName, allowedNamespaces)
	tmpl, err := template.New("scrape").Funcs(funcMap).Parse(string(tplContent))
	if err != nil {
		return nil, fmt.Errorf("%w: job %s %s: %w", bucket.ErrInvalidJob, jobName, ScrapeFileTplName, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, templateData); err != nil {
		return nil, fmt.Errorf("%w: job %s %s: %w", bucket.ErrInvalidJob, jobName, ScrapeFileTplName, err)
	}
	return rendered.Bytes(), nil
}

func scrapeTemplateFuncMap(job string, allowedNamespaces []string) template.FuncMap {
	reader := &kv.TemplateReader{
		Store:             kv.GetKVStore(),
		AllowedNamespaces: allowedNamespaces,
		Job:               job,
		ContextLabel:      job + " scrape template",
	}
	return template.FuncMap{
		"get":            reader.Get,
		"getJob":         reader.GetJob,
		"getJobOptional": reader.GetJobOptional,
		"keys": func(ns string) []string {
			if len(utils.Difference([]string{ns}, allowedNamespaces)) > 0 {
				panic(fmt.Sprintf("%s namespace is not available for job %s scrape template", ns, job))
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
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"join":  strings.Join,
		"add":   func(a, b int) int { return a + b },
		"sub":   func(a, b int) int { return a - b },
		"mul":   func(a, b int) int { return a * b },
		"div":   func(a, b int) int { return a / b },
		"int": func(s any) int {
			switch v := s.(type) {
			case int:
				return v
			case string:
				i, err := strconv.Atoi(v)
				if err != nil {
					panic(err)
				}
				return i
			default:
				panic("expected a string or an int")
			}
		},
	}
}
