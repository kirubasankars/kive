// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package durablefs provides crash-durable file and directory installation.
package durablefs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Hook is used by tests to fail a specific durability boundary.
type Hook func(operation, path string) error

// CommitError reports a failure after the new path became visible.
type CommitError struct {
	Operation string
	Path      string
	Installed bool
	Err       error
}

func (e *CommitError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Operation, e.Path, e.Err)
}

func (e *CommitError) Unwrap() error {
	return e.Err
}

func callHook(hook Hook, operation, path string) error {
	if hook == nil {
		return nil
	}
	if err := hook(operation, path); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

// Replace atomically and durably replaces path with data.
func Replace(path string, data []byte, mode fs.FileMode) error {
	return ReplaceWithHook(path, data, mode, nil)
}

// ReplaceWithHook is Replace with deterministic failure injection.
func ReplaceWithHook(path string, data []byte, mode fs.FileMode, hook Hook) error {
	dir := filepath.Dir(path)
	if err := callHook(hook, "create-temp", path); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	tempPath := temp.Name()
	installed := false
	defer func() {
		if !installed {
			_ = temp.Close()
			_ = os.Remove(tempPath)
		}
	}()

	if err := callHook(hook, "chmod", path); err != nil {
		return err
	}
	if err := temp.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("chmod staged file: %w", err)
	}
	if err := callHook(hook, "write", path); err != nil {
		return err
	}
	if err := writeAll(temp, data); err != nil {
		return fmt.Errorf("write staged file: %w", err)
	}
	if err := callHook(hook, "sync-file", path); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync staged file: %w", err)
	}
	if err := callHook(hook, "close-file", path); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close staged file: %w", err)
	}
	if err := callHook(hook, "rename", path); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install staged file: %w", err)
	}
	installed = true
	if err := syncDirWithHook(dir, hook, "open-parent", "sync-parent", "close-parent"); err != nil {
		return &CommitError{Operation: "sync parent", Path: dir, Installed: true, Err: err}
	}
	return nil
}

// WriteNew creates, syncs, and closes a new file without replacing an existing path.
func WriteNew(path string, data []byte, mode fs.FileMode, hook Hook) error {
	return CopyNew(path, bytes.NewReader(data), mode, hook)
}

// CopyNew creates, copies, syncs, and closes a new file.
func CopyNew(path string, src io.Reader, mode fs.FileMode, hook Hook) error {
	if err := callHook(hook, "create-file", path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := callHook(hook, "chmod-file", path); err != nil {
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := callHook(hook, "write-file", path); err != nil {
		return err
	}
	if _, err := io.Copy(file, src); err != nil {
		return err
	}
	if err := callHook(hook, "sync-new-file", path); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := callHook(hook, "close-new-file", path); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	keep = true
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// SyncDir flushes directory entries.
func SyncDir(dir string) error {
	return syncDirWithHook(dir, nil, "", "", "")
}

func syncDirWithHook(dir string, hook Hook, openOp, syncOp, closeOp string) error {
	if openOp != "" {
		if err := callHook(hook, openOp, dir); err != nil {
			return err
		}
	}
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	if syncOp != "" {
		if err := callHook(hook, syncOp, dir); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if closeOp != "" {
		if err := callHook(hook, closeOp, dir); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

// SyncTree syncs regular files and directories bottom-up.
func SyncTree(root string, hook Hook) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink in staged tree: %s", path)
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular file in staged tree: %s", path)
		}
		if err := callHook(hook, "sync-tree-file", path); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
	if err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(os.PathSeparator)) > strings.Count(dirs[j], string(os.PathSeparator))
	})
	for _, dir := range dirs {
		if err := callHook(hook, "sync-tree-dir", dir); err != nil {
			return err
		}
		if err := SyncDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// CommitDir installs a complete sibling directory without replacing dest.
func CommitDir(stage, dest string, hook Hook) error {
	if filepath.Dir(stage) != filepath.Dir(dest) {
		return fmt.Errorf("stage and destination must share a parent")
	}
	if err := callHook(hook, "rename-tree", dest); err != nil {
		return err
	}
	if err := renameNoReplace(stage, dest); err != nil {
		return err
	}
	if err := callHook(hook, "after-rename-tree", dest); err != nil {
		return &CommitError{Operation: "after tree rename", Path: dest, Installed: true, Err: err}
	}
	parent := filepath.Dir(dest)
	if err := syncDirWithHook(
		parent,
		hook,
		"open-tree-parent",
		"sync-tree-parent",
		"close-tree-parent",
	); err != nil {
		return &CommitError{Operation: "sync tree parent", Path: parent, Installed: true, Err: err}
	}
	return nil
}

// RemoveTree removes a pre-commit stage and durably records its removal.
func RemoveTree(stage string, hook Hook) error {
	if err := callHook(hook, "remove-stage", stage); err != nil {
		return err
	}
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	parent := filepath.Dir(stage)
	return syncDirWithHook(
		parent,
		hook,
		"open-cleanup-parent",
		"sync-cleanup-parent",
		"close-cleanup-parent",
	)
}

// Installed reports whether err occurred after the new path became visible.
func Installed(err error) bool {
	var commitErr *CommitError
	return errors.As(err, &commitErr) && commitErr.Installed
}
