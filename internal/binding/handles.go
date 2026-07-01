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

// HandleType describes how a handle's Aril boundary type (spelled `pkg.Type`)
// lowers. Two flavours:
//   - an *external* Go package handle (regexp.Regexp): GoType is the Go type
//     spelling (a pointer handle `*regexp.Regexp`), GoPkg its import path.
//   - a *runtime-backed* handle (big.BigInt): the type is an arilrt wrapper, so
//     Runtime is set and GoType is the bare runtime type name (`BigInt`) that
//     codegen qualifies via the vendored/inline package selector (`rt`); it
//     needs no external import (GoPkg ""). The `big` namespace maps to the
//     runtime like `reflect` does, not to a Go package of the same name.
//
// The Go type may differ from the Aril spelling in pointer-ness and package, so
// it is modelled explicitly rather than derived from the Aril name.
type HandleType struct {
	GoType  string
	GoPkg   string
	Runtime bool
}

// handleTypes registers every bound handle type. IsHandleType / HandleTypeOf
// read it; a type must be here for an annotation `pkg.Type` to resolve (sema)
// and to lower to the right Go type (codegen).
var handleTypes = map[string]HandleType{
	"regexp.Regexp": {GoType: "*regexp.Regexp", GoPkg: "regexp"},
	"big.BigInt":    {GoType: "BigInt", Runtime: true},
	"bufio.Scanner": {GoType: "*bufio.Scanner", GoPkg: "bufio"},
}

// HandleTypeOf returns the lowering of the handle type spelled `spelled`
// (`pkg.Type`), or ok=false when it is not a bound handle type.
func HandleTypeOf(spelled string) (HandleType, bool) {
	ht, ok := handleTypes[spelled]
	return ht, ok
}

// handleCtors maps a package-qualified constructor `(pkg, arilName)` to the Go
// function that builds the handle and the handle's Aril type spelling.
var handleCtors = map[[2]string]HandleBinding{
	{"regexp", "mustCompile"}: {GoName: "MustCompile", Params: []string{"string"}, Return: "regexp.Regexp"},
	// big constructors lower to arilrt runtime helpers (BigFromInt / BigFromInt64),
	// not a `big.*` package call — the handle is runtime-backed.
	{"big", "fromInt"}:   {GoName: "BigFromInt", Params: []string{"int"}, Return: "big.BigInt"},
	{"big", "fromInt64"}: {GoName: "BigFromInt64", Params: []string{"int64"}, Return: "big.BigInt"},
	// bufio.newScanner(r) wraps an io.Reader (os.stdin is *os.File, which Go
	// accepts as io.Reader); handle-ctor args aren't type-verified in v1, so the
	// Unknown-typed reader draws no diagnostic (the Params spelling is documentary).
	{"bufio", "newScanner"}: {GoName: "NewScanner", Params: []string{"io.Reader"}, Return: "bufio.Scanner"},
}

// handleMethods maps a handle type spelling (`pkg.Type`) to its bound method
// set (Aril method name → binding).
var handleMethods = map[string]map[string]HandleBinding{
	"regexp.Regexp": {
		"matchString": {GoName: "MatchString", Params: []string{"string"}, Return: "bool"},
		"findAll":     {GoName: "FindAllString", Params: []string{"string", "int"}, Return: "[]string"},
	},
	"big.BigInt": {
		"add":     {GoName: "Add", Params: []string{"big.BigInt"}, Return: "big.BigInt"},
		"sub":     {GoName: "Sub", Params: []string{"big.BigInt"}, Return: "big.BigInt"},
		"mul":     {GoName: "Mul", Params: []string{"big.BigInt"}, Return: "big.BigInt"},
		"div":     {GoName: "Div", Params: []string{"big.BigInt"}, Return: "big.BigInt"},
		"toInt64": {GoName: "ToInt64", Params: nil, Return: "int64"},
	},
	"bufio.Scanner": {
		"scan": {GoName: "Scan", Params: nil, Return: "bool"},
		"text": {GoName: "Text", Params: nil, Return: "string"},
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
	_, ok := handleTypes[spelled]
	return ok
}
