// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"kive/bucket"
	"kive/workspace"
)

//go:embed Makefile
var makefile []byte

//go:embed Makefile.compose
var composeMakefile []byte

//go:embed docker-compose.yml.tpl
var composeTemplate []byte

//go:embed .dockerignore.compose
var composeDockerignore []byte

//go:embed Makefile.systemd
var systemdMakefile []byte

type jobCreateOptions struct {
	jobTemplate string
	selectors   string
	runtime     string
}

type jobScaffoldFile struct {
	name    string
	content []byte
	mode    os.FileMode
}

var jobCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates job",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()
		jobTemplate, _ := flags.GetString("job-template")
		selectorComma, _ := flags.GetString("selectors")
		runtime, _ := flags.GetString("runtime")
		if err := runJobCreate(args[0], jobCreateOptions{
			jobTemplate: jobTemplate,
			selectors:   selectorComma,
			runtime:     runtime,
		}); err != nil {
			log.Fatal(err)
		}
	},
}

func runJobCreate(job string, opts jobCreateOptions) error {
	if err := workspace.ValidateJobName(job); err != nil {
		return err
	}

	if opts.runtime != "" && opts.runtime != "compose" && opts.runtime != "systemd" {
		return fmt.Errorf("%w: unsupported runtime %q (expected compose or systemd)", bucket.ErrInvalidJob, opts.runtime)
	}
	if opts.jobTemplate != "" {
		if opts.selectors != "" {
			return fmt.Errorf("%w: --selectors cannot be used with --job-template", bucket.ErrInvalidJob)
		}
		if opts.runtime != "" {
			return fmt.Errorf("%w: --runtime cannot be used with --job-template", bucket.ErrInvalidJob)
		}
		return workspace.CopyJobFromTemplate(opts.jobTemplate, job)
	}

	jobDir := path.Join(bucket.WorkspaceLocation, "jobs", job)
	if _, err := os.Stat(jobDir); err == nil {
		return fmt.Errorf("job directory already exists: %s", job)
	} else if !os.IsNotExist(err) {
		return err
	}

	var selectors []string
	if len(opts.selectors) > 0 {
		for _, s := range strings.Split(opts.selectors, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				selectors = append(selectors, s)
			}
		}
	}
	manifestContent := []byte(workspace.EmitJobConf(workspace.Manifest{
		Version:   "1.0",
		Selectors: selectors,
	}))

	files := []jobScaffoldFile{
		{name: workspace.JobConfName, content: manifestContent, mode: 0o644},
		{name: "Makefile", content: makefile, mode: 0o644},
	}
	switch opts.runtime {
	case "compose":
		files[1].content = composeMakefile
		files = append(files,
			jobScaffoldFile{name: "docker-compose.yml.tpl", content: composeTemplate, mode: 0o644},
			jobScaffoldFile{name: ".dockerignore", content: composeDockerignore, mode: 0o644},
		)
	case "systemd":
		files[1].content = []byte(strings.ReplaceAll(string(systemdMakefile), "{{JOB_NAME}}", job))
	}

	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return err
	}
	for _, file := range files {
		if err := os.WriteFile(path.Join(jobDir, file.name), file.content, file.mode); err != nil {
			return errors.Join(err, os.RemoveAll(jobDir))
		}
	}
	return nil
}

func init() {
	jobCmd.AddCommand(jobCreateCmd)
	jobCreateCmd.Flags().String("job-template", "", "copy job from templates/<name>/")
	jobCreateCmd.Flags().String("runtime", "", "scaffold runtime (compose or systemd)")
	jobCreateCmd.Flags().StringP("selectors", "s", "", "comma seperated selectors")
	// TODO: other manifest input for ease access.
}
