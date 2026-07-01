package main

import (
	"fmt"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
)

// toolchain.go — external-dependency preflight. `aril build`/`run`/`repl`
// compile the generated Go, so they need a Go toolchain of a supported version;
// `aril emit` only writes the generated source and needs nothing. The generated
// program links only the Go stdlib + the bundled arilrt runtime (itself
// stdlib-only), so the Go toolchain is aril's sole external compile-stage
// dependency — this file checks it up front and reports it in `aril version`.

// minGo is the minimum Go toolchain aril's generated code compiles against. The
// emitted Go uses the builtin `min`/`max` and the `slices` package (Go 1.21) and
// is built under go.mod's `go 1.22`; bindgen introspects via `go/importer`
// source mode, which needs a matching GOROOT source tree.
const (
	minGoMajor = 1
	minGoMinor = 22
)

// parseGoVersion extracts the normalized version ("go1.22") and its major/minor
// from `go version` output ("go version go1.22.3 linux/amd64"). ok=false when no
// `goN.M` token is present. Pure (no exec) so it is unit-testable.
func parseGoVersion(raw string) (norm string, major, minor int, ok bool) {
	for _, f := range strings.Fields(raw) {
		v, cut := strings.CutPrefix(f, "go")
		if !cut || v == "" || v[0] < '0' || v[0] > '9' {
			continue
		}
		parts := strings.SplitN(v, ".", 3)
		if len(parts) < 2 {
			continue
		}
		// The minor may carry a devel suffix ("go1.24-abcdef" → "24-abcdef");
		// take the leading digit run of each component.
		maj, ok1 := leadingInt(parts[0])
		min, ok2 := leadingInt(parts[1])
		if !ok1 || !ok2 {
			continue
		}
		return fmt.Sprintf("go%d.%d", maj, min), maj, min, true
	}
	return "", 0, 0, false
}

// leadingInt returns the integer formed by the leading digit run of s.
func leadingInt(s string) (int, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, _ := strconv.Atoi(s[:i])
	return n, true
}

// supportedGo reports whether (major, minor) meets the minimum aril needs.
func supportedGo(major, minor int) bool {
	return major > minGoMajor || (major == minGoMajor && minor >= minGoMinor)
}

// goToolchainVersion runs `go version` and returns its normalized version +
// parsed major/minor. An error means the `go` binary is absent from PATH or its
// output is unparseable.
func goToolchainVersion() (norm string, major, minor int, err error) {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "", 0, 0, err
	}
	norm, major, minor, ok := parseGoVersion(string(out))
	if !ok {
		return "", 0, 0, fmt.Errorf("could not parse `go version` output %q", strings.TrimSpace(string(out)))
	}
	return norm, major, minor, nil
}

// requireGoToolchain is the preflight for the compile-stage commands
// (build / run / repl): it fails fast with an actionable message when the Go
// toolchain is missing or older than the supported minimum. `aril emit` skips
// it (it writes generated Go without compiling).
func requireGoToolchain() error {
	norm, major, minor, err := goToolchainVersion()
	if err != nil {
		return fmt.Errorf("Go toolchain not found on PATH — `aril build`/`run` compile the generated Go, so Go %d.%d+ is required (install from https://go.dev/dl/). `aril emit` writes the Go source without a toolchain.\n  (%v)", minGoMajor, minGoMinor, err)
	}
	if !supportedGo(major, minor) {
		return fmt.Errorf("Go %s is too old — aril needs Go %d.%d+ (the generated code uses builtin min/max and the slices package). Update from https://go.dev/dl/.", norm, minGoMajor, minGoMinor)
	}
	return nil
}

// versionString is the reported aril version: the curated semver base plus the
// build's git stamp (short revision, commit date, and a `dirty` flag for
// uncommitted changes), read from the VCS build info Go embeds into `go build`
// binaries. The stamp advances every commit, so it distinguishes builds across
// PRs even between manual base bumps; it is omitted when no VCS info is present
// (e.g. a build from a tarball).
func versionString() string {
	v := version
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	var rev, when string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return v
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	stamp := rev
	if when != "" {
		stamp += ", " + when
	}
	if dirty {
		stamp += ", dirty"
	}
	return v + " (" + stamp + ")"
}

// printVersion prints aril's version plus the versions of the tools it depends
// on. The Go toolchain is the only external compile-stage dependency (the
// generated code links stdlib + the bundled arilrt runtime only).
func printVersion() {
	fmt.Printf("aril %s\n", versionString())
	norm, major, minor, err := goToolchainVersion()
	switch {
	case err != nil:
		fmt.Printf("  go:      not found on PATH (needed for build/run/repl; `aril emit` works without it)\n")
	case !supportedGo(major, minor):
		fmt.Printf("  go:      %s — TOO OLD, need go%d.%d+\n", norm, minGoMajor, minGoMinor)
	default:
		fmt.Printf("  go:      %s (min go%d.%d)\n", norm, minGoMajor, minGoMinor)
	}
	fmt.Printf("  runtime: arilrt (bundled with the compiler)\n")
}
