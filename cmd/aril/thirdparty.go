package main

// thirdparty.go — hermetic third-party dependency plumbing for FFI
// bindings (lang-spec/ffi.md §"Dependency model"). When generated Go
// imports a manifest-listed third-party package, the emitted go.mod gains
// a `require` plus a `replace` to the vendored copy, so the build never
// touches the network.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// buildModuleName is the module path of the temporary build module the
// harness writes main.go into; runtimeImportPath is the import path of
// the arilrt runtime vendored beside it under vendored-mode builds
// (Block R). The package selector is the last element, "arilrt", which
// is the prefix codegen emits (arilrt.Option, …).
const (
	buildModuleName   = "aril-output"
	runtimeImportPath = buildModuleName + "/arilrt"
	// defaultGoVersion is the Go-toolchain floor the emitted go.mod carries when
	// nothing raises it — Aril's minimum supported Go (D36).
	defaultGoVersion = "1.22"
)

// thirdPartyDep is one entry of the binding manifest (std/bindings.json).
type thirdPartyDep struct {
	ImportPath string `json:"importPath"`
	Module     string `json:"module"`
	Version    string `json:"version"`
	Vendor     string `json:"vendor"` // path to the vendored copy, relative to the aril root
}

type manifest struct {
	ThirdParty []thirdPartyDep `json:"thirdParty"`
}

// findArilRoot locates the directory holding std/bindings.json — the
// $ARIL_ROOT override, else the nearest ancestor of the cwd that
// contains it. Returns "" when no manifest is reachable (a stdlib-only
// install): third-party binding is simply unavailable, not an error.
func findArilRoot() string {
	if r := os.Getenv("ARIL_ROOT"); r != "" {
		return r
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "std", "bindings.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// loadManifest reads std/bindings.json under root. A missing manifest
// yields an empty (not failed) manifest — third-party deps are opt-in.
func loadManifest(root string) (manifest, error) {
	var m manifest
	if root == "" {
		return m, nil
	}
	data, err := os.ReadFile(filepath.Join(root, "std", "bindings.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, fmt.Errorf("aril: read binding manifest: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("aril: parse binding manifest: %w", err)
	}
	return m, nil
}

// goImportsPath reports whether the emitted Go imports importPath. It matches
// an import-block line (gofmt indents each with a tab) or a single
// `import "<path>"`, not a bare string literal of the same text in user code —
// a false match would only add a harmless unused `require`, but the precise
// check avoids it.
func goImportsPath(goSrc, importPath string) bool {
	q := `"` + importPath + `"`
	return strings.Contains(goSrc, "\t"+q) || strings.Contains(goSrc, "import "+q)
}

// usedThirdParty returns the manifest deps whose import path the emitted Go
// actually imports (the vendored std/bindings.json path).
func usedThirdParty(goSrc string, m manifest) []thirdPartyDep {
	var used []thirdPartyDep
	for _, d := range m.ThirdParty {
		if goImportsPath(goSrc, d.ImportPath) {
			used = append(used, d)
		}
	}
	return used
}

// usedGoDeps filters the manifest-declared Go-binding deps (kind="go"/"binding",
// RFC-0010) to those the emitted Go actually imports — the fetched-cache peer of
// usedThirdParty for the aril.toml `[dep]` path.
func usedGoDeps(goSrc string, deps []thirdPartyDep) []thirdPartyDep {
	var used []thirdPartyDep
	for _, d := range deps {
		if goImportsPath(goSrc, d.ImportPath) {
			used = append(used, d)
		}
	}
	return used
}

// goModText renders the temp build module's go.mod: the module line + the
// resolved Go-toolchain floor, then a `require` and a hermetic `replace` (to an
// absolute path — a vendored copy or the fetched cache) for each used dep.
func goModText(root string, used []thirdPartyDep, goVersion string) string {
	if goVersion == "" {
		goVersion = defaultGoVersion
	}
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo %s\n", buildModuleName, goVersion)
	for _, d := range used {
		fmt.Fprintf(&b, "\nrequire %s %s\n", d.Module, versionOrZero(d.Version))
		abs := d.Vendor
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, d.Vendor)
		}
		fmt.Fprintf(&b, "replace %s => %s\n", d.Module, abs)
	}
	return b.String()
}

// versionOrZero supplies a placeholder semver for a `replace`-only dep whose
// concrete version is unknown ("") — Go ignores a replaced module's require
// version, but the line must still parse.
func versionOrZero(v string) string {
	if v == "" {
		return "v0.0.0"
	}
	return v
}

// hermeticGoEnv is the environment for aril's `go build`/`go run` invocations,
// pinning GOTOOLCHAIN=local so the emitted go directive (the max-of-floors —
// possibly raised by a dependency's own floor) never silently downloads a newer
// toolchain over the network. An unmet floor fails offline with a clear "go.mod
// requires go >= X" instead — the hermetic/offline contract (RFC-0008), and the
// D36 posture that a supported toolchain is a precondition, not an auto-fetch.
func hermeticGoEnv() []string {
	return append(os.Environ(), "GOTOOLCHAIN=local")
}

// maxGoVersion resolves the Go-toolchain floor for the emitted go.mod as the
// maximum of the default floor, the root's `[toolchain] go`, and each fetched
// Go-binding dep's own `go` directive (RFC-0008 §Compatibility axes — all Aril
// modules lower into one Go module, so the root decides the version as the max
// of all floors). Wiring the deferred RFC0008-REVISION carry-forward.
func maxGoVersion(root *projectManifest, goDeps []thirdPartyDep) string {
	best := defaultGoVersion
	consider := func(v string) {
		if v != "" && goVersionLess(best, v) {
			best = v
		}
	}
	if root != nil {
		consider(root.toolchainGo)
	}
	for _, d := range goDeps {
		consider(readGoDirective(d.Vendor))
	}
	return best
}

// readGoDirective returns the `go X.Y` version declared in the go.mod at moduleDir,
// or "" when absent/unreadable (a dep whose floor cannot be read contributes none).
func readGoDirective(moduleDir string) string {
	data, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "go "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// readGoModuleName returns the `module <path>` declared in the go.mod at
// moduleDir, or "" when absent/unreadable. Used to recover a Go-binding dep's
// module path when a `replace`-based kind="go" dep omits `source`.
func readGoModuleName(moduleDir string) string {
	data, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// goVersionLess reports whether Go version a orders below b, comparing the
// dotted numeric fields (`1.22` < `1.22.1` < `1.23`). A non-numeric field sorts
// as 0, so a malformed version never spuriously wins the max.
func goVersionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = atoiOr0(as[i])
		}
		if i < len(bs) {
			y = atoiOr0(bs[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func atoiOr0(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// thirdPartyGoMod computes the go.mod for a generated program: the vendored
// std/bindings.json deps it uses, plus the manifest-declared Go-binding deps
// (kind="go"/"binding", RFC-0010) resolved through goDeps, under goVersion (the
// resolved Go-toolchain floor).
func thirdPartyGoMod(goSrc string, goDeps []thirdPartyDep, goVersion string) (string, error) {
	root := findArilRoot()
	m, err := loadManifest(root)
	if err != nil {
		return "", err
	}
	used := usedThirdParty(goSrc, m)
	used = append(used, usedGoDeps(goSrc, goDeps)...)
	return goModText(root, used, goVersion), nil
}
