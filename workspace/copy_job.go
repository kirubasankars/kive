// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"kive/bucket"
)

// CopyJobFromTemplate copies templates/<templateName>/ to workspace/jobs/<destName>/.
func CopyJobFromTemplate(templateName, destName string) error {
	if err := ValidateJobName(templateName); err != nil {
		return err
	}
	if err := ValidateJobName(destName); err != nil {
		return err
	}

	srcDir := path.Join(bucket.TemplatesLocation, templateName)
	destDir := path.Join(bucket.WorkspaceLocation, "jobs", destName)

	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("%w: job template %q not found", bucket.ErrInvalidJob, templateName)
	} else if err != nil {
		return bucket.UnexpectedError(err)
	}

	if !templateHasJobConf(srcDir) {
		return fmt.Errorf(
			"%w: job template %q has no job.conf or job.conf.bt",
			bucket.ErrInvalidJob, templateName,
		)
	}

	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("%w: job directory already exists: %s", bucket.ErrInvalidJob, destName)
	} else if !os.IsNotExist(err) {
		return bucket.UnexpectedError(err)
	}

	return copyJobTree(srcDir, destDir)
}

// RemoveJobDir deletes workspace/jobs/<name>/. Missing directories are a no-op.
func RemoveJobDir(name string) error {
	if err := ValidateJobName(name); err != nil {
		return err
	}
	destDir := path.Join(bucket.WorkspaceLocation, "jobs", name)
	if err := os.RemoveAll(destDir); err != nil {
		return bucket.UnexpectedError(err)
	}
	return nil
}

func templateHasJobConf(dir string) bool {
	for _, name := range []string{JobConfName, JobConfBTName} {
		if _, err := os.Stat(path.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func copyJobTree(srcDir, destDir string) error {
	return filepath.WalkDir(srcDir, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(srcDir, absPath)
		if err != nil {
			return bucket.UnexpectedError(err)
		}
		if rel == "." {
			return nil
		}

		if ShouldSkipJobWalk(rel, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		destPath := path.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return bucket.UnexpectedError(err)
		}
		return copyFile(absPath, destPath, info.Mode())
	})
}

func copyFile(srcPath, destPath string, mode fs.FileMode) error {
	if err := os.MkdirAll(path.Dir(destPath), 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	defer func() {
		_ = src.Close()
	}()

	dest, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	defer func() {
		_ = dest.Close()
	}()

	if _, err := io.Copy(dest, src); err != nil {
		return bucket.UnexpectedError(err)
	}
	return nil
}
