package binding

// namespaces.go — the single source for the *importable* builtin-module set.
//
// Two categories of builtin module, both spellable as `import X`:
//   - stdlib: Go-package-backed (fmt, strings, net, …) — they lower through the
//     Go import / rename machinery (codegen.isStdlibNamespaceName reads this).
//   - runtime (arilrt): reflect, big — supplied by the arilrt runtime, inlined
//     as helpers with no user-emitted Go import (reflect.* → arilrt helpers;
//     big.BigInt is a Runtime handle). They are importable but must NOT go
//     through the Go-import machinery.
//
// The *importable* set (what sema seeds + the driver accepts) is the union;
// the Go-machinery set is stdlib alone. Sourcing both here — rather than
// hand-copying a list into sema, codegen, and the driver — stops the three from
// drifting (a hand-copied reflect omission was the bug this consolidated).
// binding-surface.md.
var stdlibNamespaces = []string{
	"errors", "fmt", "os", "strings", "strconv", "bufio", "context",
	"time", "sync", "io", "log", "net", "encoding", "math",
	"unicode", "sort", "json", "slices", "regexp", "http", "url",
	// atomic → Go's sync/atomic (goImportPath); the Go package selector is still
	// `atomic`, so call sites are unaffected. Handle-only namespace (no bound
	// package functions — its whole surface is the atomic.Int64/Uint64/Bool cells).
	"atomic",
}

// runtimeNamespaces are arilrt-backed builtin modules — importable, but not
// Go-package bindings, so excluded from the Go-import/rename machinery.
var runtimeNamespaces = []string{"reflect", "big"}

var stdlibNamespaceSet = toSet(stdlibNamespaces)
var builtinModuleSet = toSet(append(append([]string{}, stdlibNamespaces...), runtimeNamespaces...))

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// IsStdlibNamespace reports whether name is a Go-package-backed stdlib module —
// the Go-import/rename machinery set (excludes the arilrt runtime modules).
func IsStdlibNamespace(name string) bool { return stdlibNamespaceSet[name] }

// IsBuiltinModule reports whether name is an importable builtin module — the
// union of the Go-backed stdlib and the arilrt runtime modules.
func IsBuiltinModule(name string) bool { return builtinModuleSet[name] }

// BuiltinModules returns every importable builtin-module name (stdlib ∪
// runtime); sema seeds its predeclared module scope from these.
func BuiltinModules() []string {
	return append(append([]string{}, stdlibNamespaces...), runtimeNamespaces...)
}
