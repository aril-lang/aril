package main

// get.go — `aril get`, the dependency fetch step (RFC-0008 §Fetch & the cache).
// It reads the project's [dependencies], fetches each pinned source@version via
// git into the hermetic module cache, resolves the transitive closure, and
// writes aril.lock. It is the ONLY network step: `aril build`/`run` resolve
// offline against the populated cache (the resolver already reads cacheModuleDir).

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func cmdGet(args []string) int {
	fs := flag.NewFlagSet("aril get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: aril get") }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aril:", err)
		return 1
	}
	m, err := findProjectManifest(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if m == nil {
		fmt.Fprintln(os.Stderr, "aril get: no aril.toml found (a project manifest declares [dependencies])")
		return 1
	}
	entries, err := fetchAll(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeLock(m.dir, entries); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("aril: %d dependency module(s) resolved; wrote %s\n", len(entries), lockFileName)
	return 0
}

// fetchAll resolves the transitive dependency closure, fetching each non-replace
// module into the cache, and returns the lock entries. A `replace`d dependency
// is local (not fetched). Only kind="aril" modules are fetched end-to-end today;
// binding/go deps (a Go require) are later work. Two pins of one module to
// different versions are a conflict (E0122) — v0.x is exact-pin, no MVS.
func fetchAll(root *projectManifest) ([]lockEntry, error) {
	locked := map[string]lockEntry{}
	pinned := map[string]string{} // dep name → "source@version"
	var walk func(m *projectManifest) error
	walk = func(m *projectManifest) error {
		for i := range m.deps {
			d := &m.deps[i]
			if d.replace != "" || d.kind != "aril" {
				continue
			}
			key := d.source + "@" + d.version
			if prev, ok := pinned[d.name]; ok {
				if prev != key {
					return fmt.Errorf("aril: error[E0122]: dependency %q is pinned to two versions in the dependency graph: %s and %s", d.name, prev, key)
				}
				continue
			}
			pinned[d.name] = key

			dest := cacheModuleDir(d.source, d.version)
			resolved, err := ensureFetched(d.source, d.version, dest)
			if err != nil {
				return err
			}
			hash, err := hashTree(dest)
			if err != nil {
				return err
			}
			locked[d.name] = lockEntry{name: d.name, source: d.source, version: d.version, resolved: resolved, hash: hash}

			// Recurse into the fetched module's own dependencies.
			sub, err := manifestAt(dest)
			if err != nil {
				return err
			}
			if sub != nil {
				if err := walk(sub); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	out := make([]lockEntry, 0, len(locked))
	for _, e := range locked {
		out = append(out, e)
	}
	return out, nil
}

// ensureFetched makes sure source@version is present in the cache at dest,
// returning the exact commit the version resolved to. A cache entry is immutable
// once written, so an existing one is reused (its commit re-derived is not
// re-run — the version is the pin). Returns the commit for a fresh fetch, or ""
// when reusing a cache entry (the lock keeps the original pin either way).
func ensureFetched(source, version, dest string) (string, error) {
	if fi, err := os.Stat(filepath.Join(dest, "aril.toml")); err == nil && fi.Mode().IsRegular() {
		return "", nil // already fetched (immutable cache entry)
	}
	return fetchModule(source, version, dest)
}

// fetchModule git-clones source, checks out version, strips the .git metadata
// (the cache holds source only, so the content hash is stable), and moves the
// tree into dest. Returns the resolved commit.
func fetchModule(source, version, dest string) (string, error) {
	tmp := dest + ".fetching"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("aril get: %w", err)
	}
	if err := runGit("", "clone", "--quiet", gitURL(source), tmp); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("aril get: fetching %q: git clone failed: %v", source, err)
	}
	if err := runGit(tmp, "checkout", "--quiet", version); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("aril get: fetching %q@%q: git checkout failed (is %q a valid tag/commit?): %v", source, version, version, err)
	}
	commit, err := gitOutput(tmp, "rev-parse", "HEAD")
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("aril get: resolving %q@%q: %v", source, version, err)
	}
	os.RemoveAll(filepath.Join(tmp, ".git"))
	os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("aril get: caching %q: %w", source, err)
	}
	return strings.TrimSpace(commit), nil
}

// gitURL maps a dependency source to a git-clonable URL. A source with an
// explicit scheme (`https://`, `file://`, `ssh://`) or a local filesystem path
// is used verbatim; a bare host/path (`github.com/x/y`) gets `https://` (D5:
// GitHub-hosted by default).
func gitURL(source string) string {
	if strings.Contains(source, "://") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") {
		return source
	}
	return "https://" + source
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	return string(out), err
}

// hashTree computes a deterministic sha256 over a module tree: every file's
// slash-normalised relative path, length, and content, in sorted path order.
func hashTree(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		content, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(content))
		h.Write(content)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
