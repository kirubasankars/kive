// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const exclusiveLockFile = "kive.lock"

// ExclusiveLock holds a non-blocking exclusive flock on <bucket>/data/kive.lock.
type ExclusiveLock struct {
	f *os.File
}

// Dup returns a close-on-exec duplicate of the lock descriptor. The duplicate
// shares the same open file description (and therefore the same flock) until
// passed through exec.Cmd.ExtraFiles.
func (l *ExclusiveLock) Dup() (*os.File, error) {
	if l == nil || l.f == nil {
		return nil, errors.New("exclusive lock is closed")
	}
	fd, err := unix.FcntlInt(l.f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, UnexpectedError(err)
	}
	return os.NewFile(uintptr(fd), l.f.Name()), nil
}

// TryExclusive acquires an exclusive advisory lock on root's data/kive.lock.
// Returns ErrBucketBusy when another process/run already holds the lock.
func TryExclusive(root string) (*ExclusiveLock, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, UnexpectedError(err)
	}
	dataDir := filepath.Join(abs, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, UnexpectedError(err)
	}
	lockPath := filepath.Join(dataDir, exclusiveLockFile)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, UnexpectedError(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrBucketBusy
		}
		return nil, UnexpectedError(err)
	}
	return &ExclusiveLock{f: f}, nil
}

// Unlock releases the flock and closes the lock file.
func (l *ExclusiveLock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}

// WithExclusive runs fn while holding TryExclusive(root).
func WithExclusive(root string, fn func() error) error {
	lock, err := TryExclusive(root)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()
	return fn()
}
