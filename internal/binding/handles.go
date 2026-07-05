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
	// Constructable: built in place via `pkg.Type{}`, vs obtain-only handles
	// reached through a ctor (lowering-go §Brace literals).
	Constructable bool
}

// handleTypes registers every bound handle type. IsHandleType / HandleTypeOf
// read it; a type must be here for an annotation `pkg.Type` to resolve (sema)
// and to lower to the right Go type (codegen).
var handleTypes = map[string]HandleType{
	"regexp.Regexp": {GoType: "*regexp.Regexp", GoPkg: "regexp"},
	"big.BigInt":    {GoType: "BigInt", Runtime: true},
	"bufio.Scanner": {GoType: "*bufio.Scanner", GoPkg: "bufio"},
	// time.Duration is a Go scalar (`type Duration int64`), not a struct handle:
	// `string` is a real method (→ String()), but `add`/`mul` lower to Go
	// operators (codegen intercept), since Duration has no Add/Mul method.
	"time.Duration": {GoType: "time.Duration", GoPkg: "time"},
	// The net socket layer (NETWORKING epoch). Conn/Listener/Addr are Go
	// *interfaces*, so GoType is the bare interface spelling (no pointer, unlike
	// *regexp.Regexp). net.Conn *is* io.Reader+io.Writer+io.Closer, so it flows
	// into io.readAll without extra wiring. Their read/write/accept/close methods
	// return Result (see handleMethods below) — the handle-method emit path wraps
	// the (T,error) / bare-error Go returns via ResultOf/ResultUnit.
	"net.Conn":     {GoType: "net.Conn", GoPkg: "net"},
	"net.Listener": {GoType: "net.Listener", GoPkg: "net"},
	"net.Addr":     {GoType: "net.Addr", GoPkg: "net"},
	// sync primitives — Constructable value structs (binding-surface §sync).
	"sync.Mutex":     {GoType: "sync.Mutex", GoPkg: "sync", Constructable: true},
	"sync.WaitGroup": {GoType: "sync.WaitGroup", GoPkg: "sync", Constructable: true},
	// context.Context — a Go interface (bare spelling, like net.Conn), obtain-only.
	"context.Context": {GoType: "context.Context", GoPkg: "context"},
	// net/http server-path handles (HTTP-SERVER epoch). ResponseWriter is a Go
	// interface (bare spelling, like net.Conn); Request is a pointer handle
	// (*http.Request). Both obtain-only — a handler receives them, never
	// constructs them. GoPkg is the *Aril import name* `http` (the key codegen
	// marks used against the `import http` statement), not the import path
	// `net/http`; the two differ only for a multi-segment package, and the Go
	// selector in GoType (`http.`) is already correct.
	"http.ResponseWriter": {GoType: "http.ResponseWriter", GoPkg: "http"},
	"http.Request":        {GoType: "*http.Request", GoPkg: "http"},
	// net/http client-path handles (HTTP-CLIENT epoch). Response is a pointer
	// handle (*http.Response) that exposes *fields* (statusCode/status/header/
	// body) read through the handle-field table (handleFields) — the first handle
	// with a field axis, not just methods. Header is Go's `http.Header` (a map
	// type with value methods), method-only. Both obtain-only (a Response comes
	// from http.get/do; a Header from a Response/Request field).
	"http.Response": {GoType: "*http.Response", GoPkg: "http"},
	"http.Header":   {GoType: "http.Header", GoPkg: "http"},
	// net/url (HTTP-CLIENT epoch). url.URL is a pointer handle (*url.URL) with a
	// field axis (scheme/host/path) + a string() method; obtained from url.parse
	// or an http.Request's url field. GoPkg is the Aril import name `url` (→ net/url
	// via goImportPath), like http.
	"url.URL": {GoType: "*url.URL", GoPkg: "url"},
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
	"time.Duration": {
		// add/mul lower to Go operators (codegen durationOpMethod intercept);
		// GoName is unused for them. string is a genuine Duration method.
		"add":    {GoName: "", Params: []string{"time.Duration"}, Return: "time.Duration"},
		"mul":    {GoName: "", Params: []string{"int"}, Return: "time.Duration"},
		"string": {GoName: "String", Params: nil, Return: "string"},
	},
	// net socket layer. Result-returning methods (read/write/accept/close) are
	// wrapped by the handle-method emit path (ResultOf for (T,error), ResultUnit
	// for bare error) — the first handle methods to carry a Result return.
	"net.Conn": {
		"read":  {GoName: "Read", Params: []string{"[]byte"}, Return: "Result<int, error>"},
		"write": {GoName: "Write", Params: []string{"[]byte"}, Return: "Result<int, error>"},
		"close": {GoName: "Close", Params: nil, Return: "Result<unit, error>"},
	},
	"net.Listener": {
		"accept": {GoName: "Accept", Params: nil, Return: "Result<net.Conn, error>"},
		"close":  {GoName: "Close", Params: nil, Return: "Result<unit, error>"},
		"addr":   {GoName: "Addr", Params: nil, Return: "net.Addr"},
	},
	"net.Addr": {
		"string": {GoName: "String", Params: nil, Return: "string"},
	},
	// sync method sets — lock/unlock/wait/done return `unit` (binding-surface §sync).
	"sync.Mutex": {
		"lock":    {GoName: "Lock", Params: nil, Return: "unit"},
		"unlock":  {GoName: "Unlock", Params: nil, Return: "unit"},
		"tryLock": {GoName: "TryLock", Params: nil, Return: "bool"},
	},
	"sync.WaitGroup": {
		"add":  {GoName: "Add", Params: []string{"int"}, Return: "unit"},
		"done": {GoName: "Done", Params: nil, Return: "unit"},
		"wait": {GoName: "Wait", Params: nil, Return: "unit"},
	},
	"context.Context": {
		// done() → Go's <-chan struct{}; err/deadline/value deferred (binding-surface §context).
		"done": {GoName: "Done", Params: nil, Return: "RecvChan<unit>"},
	},
	// net/http ResponseWriter method set (binding-surface §net/http). `write` and
	// `writeHeader` are the real Go methods; `writeString` is an Aril convenience
	// (ResponseWriter is an io.Writer) with no matching Go method — it lowers via
	// a codegen intercept to `ResultOf(w.Write([]byte(s)))`, so its GoName is
	// unused (empty, like time.Duration's operator methods). Result-returning
	// methods are wrapped via ResultOf like the net handle methods.
	"http.ResponseWriter": {
		"write":       {GoName: "Write", Params: []string{"[]byte"}, Return: "Result<int, error>"},
		"writeString": {GoName: "", Params: []string{"string"}, Return: "Result<int, error>"},
		"writeHeader": {GoName: "WriteHeader", Params: []string{"int"}, Return: "unit"},
	},
	// http.Request is opaque in v1 (a handler may ignore it); its field/method
	// surface (method/url/header/body) is a carry-forward. Registered as a handle
	// type above so `r: http.Request` annotations resolve; no methods bound yet.
	// net/http Header method set (binding-surface §net/http). Go's http.Header is
	// a map type with value methods; Aril `delete` maps to Go's `Del` (not Delete).
	"http.Header": {
		"get":    {GoName: "Get", Params: []string{"string"}, Return: "string"},
		"values": {GoName: "Values", Params: []string{"string"}, Return: "[]string"},
		"set":    {GoName: "Set", Params: []string{"string", "string"}, Return: "unit"},
		"add":    {GoName: "Add", Params: []string{"string", "string"}, Return: "unit"},
		"delete": {GoName: "Del", Params: []string{"string"}, Return: "unit"},
	},
	// net/url URL method set (binding-surface §net/url). string() reassembles the
	// URL (Go's (*url.URL).String); the components are read as fields (handleFields).
	"url.URL": {
		"string": {GoName: "String", Params: nil, Return: "string"},
	},
}

// handleFields maps a handle type spelling (`pkg.Type`) to its bound *field* set
// (Aril field name → binding: GoName is the exported Go struct field, Return the
// Aril type spelling of the field). This is the field axis of the handle table —
// the mirror of handleMethods for a handle that exposes struct fields, not just
// methods (http.Response is the first). `Params` is unused for a field. Read by
// sema (field-access typing) and codegen (Go field-name lowering), so the two
// can never drift (the D33/D37 single-source discipline).
var handleFields = map[string]map[string]HandleBinding{
	// net/http Response fields (binding-surface §net/http). Go's *http.Response
	// exposes StatusCode/Status/Header/Body; Body is an io.ReadCloser the caller
	// drains via io.readAll.
	"http.Response": {
		"statusCode": {GoName: "StatusCode", Return: "int"},
		"status":     {GoName: "Status", Return: "string"},
		"header":     {GoName: "Header", Return: "http.Header"},
		"body":       {GoName: "Body", Return: "io.ReadCloser"},
	},
	// net/url URL fields (binding-surface §net/url) — all capitalize identically
	// to their Go struct field (scheme→Scheme, …).
	"url.URL": {
		"scheme":   {GoName: "Scheme", Return: "string"},
		"host":     {GoName: "Host", Return: "string"},
		"path":     {GoName: "Path", Return: "string"},
		"rawQuery": {GoName: "RawQuery", Return: "string"},
		"fragment": {GoName: "Fragment", Return: "string"},
	},
	// http.Request's url field (binding-surface §net/http). `url` → Go `URL` is the
	// first *divergent-name* handle field: exportFieldName(url) would be `Url`, but
	// Go's field is `URL` — so this exercises the handleFieldGoName table lookup
	// distinctly from the generic export path (the field yields a url.URL handle).
	"http.Request": {
		"method": {GoName: "Method", Return: "string"},
		"url":    {GoName: "URL", Return: "url.URL"},
	},
}

// HandleFieldOf returns the binding for field `arilName` on the handle type
// spelled `handle` (`pkg.Type`), or ok=false when it is not a bound field.
func HandleFieldOf(handle, arilName string) (HandleBinding, bool) {
	m, ok := handleFields[handle]
	if !ok {
		return HandleBinding{}, false
	}
	b, ok := m[arilName]
	return b, ok
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

// IsConstructableHandle reports whether `spelled` is built in place with a
// zero-value `pkg.Type{}` literal (lowering-go §Brace literals).
func IsConstructableHandle(spelled string) bool {
	ht, ok := handleTypes[spelled]
	return ok && ht.Constructable
}
