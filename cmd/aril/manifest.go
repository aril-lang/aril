package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// projectManifest is the parsed `aril.toml` project file (RFC-0008
// §Manifests). A module **self-declares what it *is*** in `[package]` — its
// import-root name, its `kind`, the build-system-format `edition`, and a
// `min-aril` toolchain floor — and a consumer declares only what it *requires*
// in `[dep.<name>]`. `[toolchain] go` (the pinned Go compiler) and `[bindings]
// extra` are retained; `[about]` is a reserved free-form section the reader
// accepts and ignores wholesale (the one deliberate hole in the closed schema).
//
// `[package]` supersedes the RFC-0002 `[project]` name; `[project]` is still
// accepted as a compat alias so pre-revision manifests and the corpus keep
// building (canonical spelling is `[package]`).
//
// The parser below is a deliberately tiny, closed-schema reader — the compiler
// core stays dependency-free (no third-party TOML library, D19), so only the
// exact line shapes `manifest.md` §Schema documents are accepted.
type projectManifest struct {
	dir           string       // directory containing aril.toml (the resolution root)
	name          string       // [package] name — import-path root prefix (was [project] name)
	packageKind   string       // [package] kind — self-declared "aril" | "binding" ("" ⇒ aril)
	edition       string       // [package] edition — project-file/build-system format
	minAril       string       // [package] min-aril — minimum Aril toolchain floor
	binds         string       // [package] binds — kind="binding" only: the bound Go module
	bindsGo       string       // [package] binds-go — kind="binding" only: the bound Go version floor
	toolchainGo   string       // [toolchain] go — pinned Go version
	bindingsExtra []string     // [bindings] extra — extra Go import paths
	deps          []dependency // [dep.<name>] — external module deps (RFC-0008)
	buildOutDir   string       // [build] out-dir — build-artifact directory (RFC-0009)
}

// dependency is one [dep.<name>] entry (RFC-0008 §`[dep.<name>]`). The section
// name is the import-root the consumer uses (defaulting to the dependency's
// self-declared [package] name; a consumer may key it differently to alias).
// `kind` is a *consumer-side* field: for an aril/binding dep it is **omitted**
// (the kind is read from the fetched [package]) and, when present, acts as an
// assert-verify guard; it is **required only for kind="go"** (a raw Go module
// has no aril.toml to self-declare). The fields carry the whole declared shape;
// version *ranges* + MVS resolution are later work in this epoch.
type dependency struct {
	name    string // import-path root — the [dep.<name>] key
	source  string // Git/GitHub fetch location (D5)
	version string // exact pin: a tag or commit (hermetic, D19)
	kind    string // consumer-declared "aril" | "binding" | "go"; "" ⇒ read from the dep's [package]
	path    string // kind="go": the local binding table, relative to this project
	replace string // optional local filesystem path overriding source (dev/vendor)
}

// effectiveDepKind resolves the kind that actually governs a dependency: the
// consumer's declared kind when given, else the dependency's self-declared
// [package] kind, else "aril" (RFC-0008 §`[dep.<name>]` — kind is the package's
// to declare; the consumer restates it only as a guard, or for kind="go").
func effectiveDepKind(consumerKind, packageKind string) string {
	if consumerKind != "" {
		return consumerKind
	}
	if packageKind != "" {
		return packageKind
	}
	return "aril"
}

// depKindGuard reports a mismatch between a consumer's asserted [dep].kind and
// the dependency's self-declared [package].kind — the assert-verify guard
// (RFC-0008: a supply-chain check that you pulled what you expected). It is a
// no-op unless both are given and disagree.
func depKindGuard(name, consumerKind, packageKind string) error {
	if consumerKind != "" && packageKind != "" && consumerKind != packageKind {
		return fmt.Errorf("aril: dependency %q: [dep] declares kind %q but the module's [package] self-declares kind %q (the [dep].kind guard must match)", name, consumerKind, packageKind)
	}
	return nil
}

// findProjectManifest walks up from startDir to the filesystem root
// looking for a `aril.toml`. It returns (nil, nil) when none is found —
// a single-package, stdlib-only program needs no manifest (RFC-0002
// §Resolution: step 1 is skipped without one).
func findProjectManifest(startDir string) (*projectManifest, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("aril: %w", err)
	}
	for {
		path := filepath.Join(dir, "aril.toml")
		if _, err := os.Stat(path); err == nil {
			return parseProjectManifest(path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

// parseProjectManifest reads the closed-schema `aril.toml`. It accepts
// `[package]` (or its `[project]` alias) / `[toolchain]` / `[bindings]` /
// `[build]` sections plus the repeatable `[dep.<name>]`, `key = "value"` and
// `key = ["a", "b"]` lines, `#` comments, and blank lines; anything else is an
// error (the schema is closed in v0.x). `[about]` is reserved and free-form —
// its whole body is accepted and ignored (RFC-0008 §`[about]`).
func parseProjectManifest(path string) (*projectManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("aril: cannot read %s: %w", path, err)
	}
	m := &projectManifest{dir: filepath.Dir(path)}
	section := ""
	curDep := -1     // index into m.deps while inside a [dep.<name>], else -1
	inAbout := false // inside the reserved free-form [about] section (body ignored)
	bail := func(line int, msg string) error {
		return fmt.Errorf("aril: %s:%d: %s", path, line, msg)
	}
	for i, raw := range strings.Split(string(data), "\n") {
		ln := i + 1
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, bail(ln, "malformed section header")
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			curDep = -1
			inAbout = false
			// [dep.<name>] — a named external-module dependency
			// (RFC-0008). The bare [dep] container is rejected: a
			// dependency must carry its import-path-root name.
			if section == "dep" || strings.HasPrefix(section, "dep.") {
				depName := strings.TrimSpace(strings.TrimPrefix(section, "dep"))
				depName = strings.TrimSpace(strings.TrimPrefix(depName, "."))
				if depName == "" {
					return nil, bail(ln, "[dep] must be a named sub-section: [dep.<name>]")
				}
				for j := range m.deps {
					if m.deps[j].name == depName {
						return nil, bail(ln, fmt.Sprintf("duplicate [dep.%s] section", depName))
					}
				}
				m.deps = append(m.deps, dependency{name: depName})
				curDep = len(m.deps) - 1
				continue
			}
			// [project] is the pre-revision spelling of [package]; canonicalise
			// so all downstream key dispatch keys on "package" (compat alias).
			if section == "project" {
				section = "package"
			}
			switch section {
			case "package", "toolchain", "bindings", "build":
			case "about":
				inAbout = true // reserved + free-form: ignore the whole body
			default:
				return nil, bail(ln, fmt.Sprintf("unknown section %q (expected package / toolchain / bindings / build / about / dep.<name>)", section))
			}
			continue
		}
		// [about] is free-form: its body lines are accepted and ignored
		// wholesale (a following `[table]` header ends the block, above).
		if inAbout {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, bail(ln, "expected `key = value`")
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if curDep >= 0 {
			// Inside a [dep.<name>] section: every value is a scalar
			// string. Unknown keys are rejected (the schema is closed).
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			d := &m.deps[curDep]
			switch key {
			case "source":
				d.source = s
			case "version":
				d.version = s
			case "kind":
				d.kind = s
			case "path":
				d.path = s
			case "replace":
				d.replace = s
			default:
				return nil, bail(ln, fmt.Sprintf("unknown key %q in [dep.%s] (want source / version / kind / path / replace)", key, d.name))
			}
			continue
		}
		switch [2]string{section, key} {
		case [2]string{"package", "name"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.name = s
		case [2]string{"package", "kind"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.packageKind = s
		case [2]string{"package", "edition"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.edition = s
		case [2]string{"package", "min-aril"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.minAril = s
		case [2]string{"package", "binds"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.binds = s
		case [2]string{"package", "binds-go"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.bindsGo = s
		case [2]string{"toolchain", "go"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.toolchainGo = s
		case [2]string{"bindings", "extra"}:
			items, err := tomlStringArray(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.bindingsExtra = items
		case [2]string{"build", "out-dir"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.buildOutDir = s
		default:
			if section == "" {
				return nil, bail(ln, "key outside any [section]")
			}
			return nil, bail(ln, fmt.Sprintf("unknown key %q in [%s]", key, section))
		}
	}
	if m.name == "" {
		return nil, fmt.Errorf("aril: %s: [package] name is required", path)
	}
	// A module self-declares only kind="aril" or "binding": "go" is a raw Go
	// module with no aril.toml to self-declare in (RFC-0008 §The three kinds),
	// so it can appear only as a consumer's [dep].kind, never here.
	switch m.packageKind {
	case "", "aril", "binding":
	case "go":
		return nil, fmt.Errorf("aril: %s: [package] kind = \"go\" is not self-declarable (a raw Go module has no aril.toml); kind=\"go\" is a consumer's [dep] choice", path)
	default:
		return nil, fmt.Errorf("aril: %s: [package] unknown kind %q (want aril | binding)", path, m.packageKind)
	}
	// A binding package must self-declare the Go module it wraps and its version
	// (RFC-0010 — the require+replace target); `binds`/`binds-go` are meaningless
	// on any other kind.
	if m.packageKind == "binding" {
		if m.binds == "" || m.bindsGo == "" {
			return nil, fmt.Errorf("aril: %s: [package] kind = \"binding\" requires both `binds` (the bound Go module) and `binds-go` (its version)", path)
		}
	} else if m.binds != "" || m.bindsGo != "" {
		return nil, fmt.Errorf("aril: %s: [package] `binds`/`binds-go` are valid only for kind = \"binding\"", path)
	}
	// Compatibility axes (RFC-0008 §Compatibility axes) — enforced on every
	// manifest read (root + each dependency), so a floor a *dependency* declares
	// binds the whole build.
	if err := validateCompat(m, path); err != nil {
		return nil, err
	}
	// Two extra bindings sharing a last segment collide on their local
	// import name (RFC-0002 §Manifest).
	seen := map[string]bool{}
	for _, p := range m.bindingsExtra {
		last := lastSegment(p)
		if seen[last] {
			return nil, fmt.Errorf("aril: %s: [bindings] extra has two entries with last segment %q", path, last)
		}
		seen[last] = true
	}
	// Per-dependency schema validation (RFC-0008). Head-segment collisions
	// across the resolution surface (dep root vs builtin module vs [bindings]
	// extra) are a resolver concern, not a manifest-parse one — deferred.
	for i := range m.deps {
		d := &m.deps[i]
		if d.name == m.name {
			return nil, fmt.Errorf("aril: %s: [dep.%s] collides with the project's own [package] name", path, d.name)
		}
		// The consumer's kind is optional: "" means "read the kind from the
		// dependency's own [package]" (RFC-0008). A present value is a guard,
		// or the required self-declaration for kind="go".
		switch d.kind {
		case "", "aril", "binding", "go":
		default:
			return nil, fmt.Errorf("aril: %s: [dep.%s] unknown kind %q (want aril | binding | go, or omit to read it from the dependency)", path, d.name, d.kind)
		}
		// A `replace`d dependency is resolved locally, so source/version are
		// not required; otherwise both pin the fetch (D5/D19).
		if d.replace == "" {
			if d.source == "" {
				return nil, fmt.Errorf("aril: %s: [dep.%s] requires `source` (or a `replace` override)", path, d.name)
			}
			if d.version == "" {
				return nil, fmt.Errorf("aril: %s: [dep.%s] requires `version` (a constraint like `^1.3`, an exact `vX.Y.Z` tag, or a commit)", path, d.name)
			}
			// The version is a constraint (RFC-0008 §Version constraints):
			// caret/tilde/wildcard/compound/exact ranges, an exact tag, or a
			// commit SHA. A bare `1.2.3` gets the targeted `v`-prefix
			// diagnostic here.
			if _, err := parseConstraint(d.version); err != nil {
				return nil, fmt.Errorf("aril: %s: [dep.%s] version: %v", path, d.name, err)
			}
		}
		if d.kind == "go" && d.path == "" {
			return nil, fmt.Errorf("aril: %s: [dep.%s] kind=\"go\" requires `path` (the binding table)", path, d.name)
		}
		if d.kind != "go" && d.path != "" {
			return nil, fmt.Errorf("aril: %s: [dep.%s] `path` is only valid for kind=\"go\"", path, d.name)
		}
	}
	return m, nil
}

// supportedEdition is the one project-file/build-system format v0.x defines
// (RFC-0008 §Compatibility axes — the field reserves the mechanism for later
// editions). An empty edition means "the default".
const supportedEdition = "2026"

// validateCompat enforces the two library-side compatibility axes a manifest
// carries (RFC-0008 §Compatibility axes): the build-system-format `edition`
// (an unsupported value is rejected) and the `min-aril` toolchain floor (a
// module needing a newer Aril than the running one is a hard error — the
// Go/Cargo lineage, not npm's soft warn). Both are build-time checks, never
// resolution inputs.
func validateCompat(m *projectManifest, path string) error {
	if m.edition != "" && m.edition != supportedEdition {
		return fmt.Errorf("aril: %s: [package] edition %q is not supported by this toolchain (this version understands edition %q)", path, m.edition, supportedEdition)
	}
	if m.minAril != "" {
		floor, err := parseSemverCore(strings.TrimPrefix(m.minAril, "v"))
		if err != nil {
			return fmt.Errorf("aril: %s: [package] min-aril %q is not a valid version", path, m.minAril)
		}
		if floor.compare(runningToolchainVersion()) > 0 {
			return fmt.Errorf("aril: %s: [package] requires Aril >= %s but this toolchain is %s; upgrade the Aril toolchain", path, m.minAril, runningToolchainVersion())
		}
	}
	return nil
}

// runningToolchainVersion is the semver core of the compiler's `version` const
// (the `-dev`/`+meta` build-stamp suffix stripped) — the floor `min-aril` is
// compared against.
func runningToolchainVersion() semver {
	base := version
	if i := strings.IndexAny(base, "-+"); i >= 0 {
		base = base[:i]
	}
	v, err := parseSemverCore(base)
	if err != nil {
		return semver{}
	}
	return v
}

// stripComment removes a trailing `#` comment. v0.x has no `#` inside a
// manifest string value, so a bare split is sufficient for the closed
// schema.
func stripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

// tomlString decodes a double-quoted scalar string value.
func tomlString(v string) (string, error) {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return "", fmt.Errorf("expected a double-quoted string, got %q", v)
	}
	return v[1 : len(v)-1], nil
}

// tomlStringArray decodes a single-line `["a", "b"]` array of strings.
func tomlStringArray(v string) ([]string, error) {
	if len(v) < 2 || v[0] != '[' || v[len(v)-1] != ']' {
		return nil, fmt.Errorf("expected an array `[...]`, got %q", v)
	}
	body := strings.TrimSpace(v[1 : len(v)-1])
	if body == "" {
		return nil, nil
	}
	var out []string
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		s, err := tomlString(part)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// lastSegment returns the final `/`-separated segment of an import path
// — the local identifier a package binds to (RFC-0002 §Cross-package
// imports): `myproj/svc/store` → `store`, `fmt` → `fmt`.
func lastSegment(importPath string) string {
	if i := strings.LastIndexByte(importPath, '/'); i >= 0 {
		return importPath[i+1:]
	}
	return importPath
}
