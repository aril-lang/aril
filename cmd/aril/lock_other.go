//go:build !unix

package main

// acquireBuildLock is a no-op where flock is unavailable: concurrent builds in
// one project are unguarded there (RFC-0009 §Concurrent builds). The primary
// motivating case — a parallel corpus run — hands each invocation a distinct
// out-dir anyway, so the lock only matters for a user running two builds of one
// project at once, on a unix host.
func acquireBuildLock(dir string) (func(), error) {
	return func() {}, nil
}
