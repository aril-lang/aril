package main

import _ "embed"

// Compiler-bundled standard-library modules, written in Aril and embedded into
// the compiler. A `import std/<name>` resolves not to a filesystem directory
// (like a user package) but to the embedded source here: the module's decls are
// merged into the build under a stable virtual path (so `//line` blame and the
// emitted-Go hash stay deterministic across machines), and the import is
// stripped from the Go import block like a user-package import (RFC-0002).
//
// v1 ships one module — `std/pred`, the contract predicate vocabulary
// (RFC-0006). The mechanism is manifest-independent: a lone `.aril` file with
// no `aril.toml` can still `import std/pred`.

//go:embed std/pred.aril
var stdPredSource string

// stdModule is one bundled module: the stable virtual file path used as its
// `//line` label and read key, and its embedded Aril source.
type stdModule struct {
	label  string
	source string
}

// stdModules maps an import path to its bundled module.
var stdModules = map[string]stdModule{
	"std/pred": {label: "std/pred.aril", source: stdPredSource},
}

// isStdModule reports whether an import path names a bundled std module.
func isStdModule(path string) bool {
	_, ok := stdModules[path]
	return ok
}

// stdModuleSourceByLabel returns the embedded source for a module's virtual
// file path (the label assigned in stdModules), or ("", false). Used by the
// compile read loop to satisfy a virtual path from the embed instead of disk.
func stdModuleSourceByLabel(label string) (string, bool) {
	for _, m := range stdModules {
		if m.label == label {
			return m.source, true
		}
	}
	return "", false
}

// gatherStdModules scans real source files for `import std/<name>` and returns
// the bundled modules' virtual file paths (to add to the build) plus the
// import paths to strip from the Go import block. Deduplicated; the modules
// themselves import nothing, so no transitive resolution is needed. fileImports
// reads from disk, so it is only called on the real (non-virtual) files.
func gatherStdModules(files []string) (virtualFiles []string, stripPaths map[string]bool) {
	stripPaths = map[string]bool{}
	seen := map[string]bool{}
	for _, f := range files {
		imps, err := fileImports(f)
		if err != nil {
			continue // defer to the authoritative parse in compilePackage
		}
		for _, p := range imps {
			if mod, ok := stdModules[p]; ok && !seen[p] {
				seen[p] = true
				virtualFiles = append(virtualFiles, mod.label)
				stripPaths[p] = true
			}
		}
	}
	return virtualFiles, stripPaths
}
