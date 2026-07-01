package binding

// handles.go — the value-handle binding surface: Go stdlib *struct handles*
// (regexp.Regexp, big.BigInt, bufio.Scanner, …) surfaced through the
// builtin-module namespace, together with their method sets. Unlike the
// mechanical package-function registry (registry_gen.go, D33 — derived from
// go/types over a curated Manifest), a handle carries a value type with a
// method set, and the highest-value cases (big's functional wrapper over Go's
// pointer-mutation methods) are irreducibly non-mechanical. So this is a
// hand-curated idiom table rather than a derived one — but, like the registry,
// it is the *single* source both sema (return typing) and codegen (Go-name
// lowering) read, so the two can never drift (the D33 discipline).
//
// A handle type is spelled `pkg.Type` (e.g. "regexp.Regexp"); that spelling is
// the Aril boundary type sema carries (semaTypeFromSpelling → Named) and the Go
// type codegen emits. Constructors are package-qualified functions returning a
// handle; methods dispatch on the handle's Aril type at the call site.

// HandleBinding is one bound member — a constructor or a method: its Go
// spelling, the Aril type spellings of its parameters, and the Aril return-type
// spelling. sema parses Params/Return via semaTypeFromSpelling; codegen emits
// GoName verbatim.
type HandleBinding struct {
	GoName string
	Params []string
	Return string
}

// handleCtors maps a package-qualified constructor `(pkg, arilName)` to the Go
// function that builds the handle and the handle's Aril type spelling.
var handleCtors = map[[2]string]HandleBinding{
	{"regexp", "mustCompile"}: {GoName: "MustCompile", Params: []string{"string"}, Return: "regexp.Regexp"},
}

// handleMethods maps a handle type spelling (`pkg.Type`) to its bound method
// set (Aril method name → binding).
var handleMethods = map[string]map[string]HandleBinding{
	"regexp.Regexp": {
		"matchString": {GoName: "MatchString", Params: []string{"string"}, Return: "bool"},
		"findAll":     {GoName: "FindAllString", Params: []string{"string", "int"}, Return: "[]string"},
	},
}

// HandleCtorOf returns the binding for a handle constructor `pkg.arilName`, or
// ok=false when the pair is not a handle constructor.
func HandleCtorOf(pkg, arilName string) (HandleBinding, bool) {
	b, ok := handleCtors[[2]string{pkg, arilName}]
	return b, ok
}

// HandleMethodOf returns the binding for method `arilName` on the handle type
// spelled `handle` (`pkg.Type`), or ok=false when it is not a bound method.
func HandleMethodOf(handle, arilName string) (HandleBinding, bool) {
	m, ok := handleMethods[handle]
	if !ok {
		return HandleBinding{}, false
	}
	b, ok := m[arilName]
	return b, ok
}

// IsHandleType reports whether `spelled` names a bound stdlib handle type.
func IsHandleType(spelled string) bool {
	_, ok := handleMethods[spelled]
	return ok
}
