package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// projectManifest is the parsed `aril.toml` project file (RFC-0002
// §Manifest). It is the only v0.x configuration surface: project name
// (the cross-package import-path root), the pinned Go toolchain, and an
// optional list of extra Go packages exposed as bare-ident bindings.
//
// The parser below is a deliberately tiny, closed-schema reader — the
// compiler core stays dependency-free (no third-party TOML library), so
// only the exact line shapes RFC-0002 §Manifest documents are accepted.
type projectManifest struct {
	dir           string       // directory containing aril.toml (the resolution root)
	name          string       // [project] name — import-path root prefix
	toolchainGo   string       // [toolchain] go — pinned Go version
	bindingsExtra []string     // [bindings] extra — extra Go import paths
	deps          []dependency // [dependencies.<name>] — external module deps (RFC-0008)
}

// dependency is one [dependencies.<name>] entry (RFC-0008 §The manifest). The
// section name is the dependency's import-path root — the [project] name it
// declares in its own aril.toml — so a consumer writes `import <name>/<pkg>`.
// Resolution/fetch of these deps is later work (this reader only parses +
// validates the schema); the fields carry the whole declared shape.
type dependency struct {
	name    string // import-path root — the [dependencies.<name>] key
	source  string // Git/GitHub fetch location (D5)
	version string // exact pin: a tag or commit (hermetic, D19)
	kind    string // "aril" | "binding" | "go" (default "aril")
	path    string // kind="go": the local binding table, relative to this project
	replace string // optional local filesystem path overriding source (dev/vendor)
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
// `[project]` / `[toolchain]` / `[bindings]` sections, `key = "value"`
// and `key = ["a", "b"]` lines, `#` comments, and blank lines; anything
// else is an error (the schema is closed in v0.x).
func parseProjectManifest(path string) (*projectManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("aril: cannot read %s: %w", path, err)
	}
	m := &projectManifest{dir: filepath.Dir(path)}
	section := ""
	curDep := -1 // index into m.deps while inside a [dependencies.<name>], else -1
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
			// [dependencies.<name>] — a named external-module dependency
			// (RFC-0008). The bare [dependencies] container is rejected: a
			// dependency must carry its import-path-root name.
			if section == "dependencies" || strings.HasPrefix(section, "dependencies.") {
				depName := strings.TrimSpace(strings.TrimPrefix(section, "dependencies"))
				depName = strings.TrimSpace(strings.TrimPrefix(depName, "."))
				if depName == "" {
					return nil, bail(ln, "[dependencies] must be a named sub-section: [dependencies.<name>]")
				}
				for j := range m.deps {
					if m.deps[j].name == depName {
						return nil, bail(ln, fmt.Sprintf("duplicate [dependencies.%s] section", depName))
					}
				}
				m.deps = append(m.deps, dependency{name: depName, kind: "aril"})
				curDep = len(m.deps) - 1
				continue
			}
			switch section {
			case "project", "toolchain", "bindings":
			default:
				return nil, bail(ln, fmt.Sprintf("unknown section %q (expected project / toolchain / bindings / dependencies.<name>)", section))
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, bail(ln, "expected `key = value`")
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if curDep >= 0 {
			// Inside a [dependencies.<name>] section: every value is a scalar
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
				return nil, bail(ln, fmt.Sprintf("unknown key %q in [dependencies.%s] (want source / version / kind / path / replace)", key, d.name))
			}
			continue
		}
		switch [2]string{section, key} {
		case [2]string{"project", "name"}:
			s, err := tomlString(val)
			if err != nil {
				return nil, bail(ln, err.Error())
			}
			m.name = s
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
		default:
			if section == "" {
				return nil, bail(ln, "key outside any [section]")
			}
			return nil, bail(ln, fmt.Sprintf("unknown key %q in [%s]", key, section))
		}
	}
	if m.name == "" {
		return nil, fmt.Errorf("aril: %s: [project] name is required", path)
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
			return nil, fmt.Errorf("aril: %s: [dependencies.%s] collides with the project's own [project] name", path, d.name)
		}
		switch d.kind {
		case "aril", "binding", "go":
		default:
			return nil, fmt.Errorf("aril: %s: [dependencies.%s] unknown kind %q (want aril | binding | go)", path, d.name, d.kind)
		}
		// A `replace`d dependency is resolved locally, so source/version are
		// not required; otherwise both pin the fetch (D5/D19).
		if d.replace == "" {
			if d.source == "" {
				return nil, fmt.Errorf("aril: %s: [dependencies.%s] requires `source` (or a `replace` override)", path, d.name)
			}
			if d.version == "" {
				return nil, fmt.Errorf("aril: %s: [dependencies.%s] requires `version` (an exact tag or commit)", path, d.name)
			}
		}
		if d.kind == "go" && d.path == "" {
			return nil, fmt.Errorf("aril: %s: [dependencies.%s] kind=\"go\" requires `path` (the binding table)", path, d.name)
		}
		if d.kind != "go" && d.path != "" {
			return nil, fmt.Errorf("aril: %s: [dependencies.%s] `path` is only valid for kind=\"go\"", path, d.name)
		}
	}
	return m, nil
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
