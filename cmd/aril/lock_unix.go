//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireBuildLock takes an exclusive advisory lock on dir for the span of
// lowering + go build, so two concurrent `aril build`/`run` in one project
// serialize on gen/ rather than corrupting each other's intermediate Go
// (RFC-0009 §Concurrent builds; Cargo locks target/ for the same reason). The
// lock is blocking — the second build waits for the first. flock is released by
// the kernel when the fd closes or the process dies, so a crashed build leaves
// no stale lock to clear. Returns the release to defer.
func acquireBuildLock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("aril: mkdir out-dir: %w", err)
	}
	// The lock file persists (self-ignored under aril-out/); it is the lock
	// handle, not an artifact — unlinking it would race a waiting builder.
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("aril: open build lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("aril: acquire build lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
