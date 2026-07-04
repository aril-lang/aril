package binding

// interfaces.go — bound Go *interfaces* an Aril class can `implements` (D14
// conformance realized against a *bound* interface). This is the inverse of the
// handle table: a handle is a Go type Aril *calls into* (regexp.Regexp,
// net.Conn); a bound interface is a Go interface an Aril class is *implemented
// against* (`class X implements http.Handler`). The table is the single source
// for two consumers:
//   - sema reads the method set to structurally verify the class provides every
//     interface method with a matching signature (the first structural
//     conformance check — E0219), turning a Go-side "does not implement" build
//     failure into an Aril-coordinate diagnostic (D10);
//   - codegen reads the Aril→Go method-name correspondence (serveHTTP →
//     ServeHTTP) so the emitted class satisfies Go's interface.
//
// A bound interface is spelled `pkg.Type` (e.g. "http.Handler"), the same
// boundary spelling sema carries as a Named and codegen emits as the Go type.
// The method binding reuses HandleBinding (GoName / Params / Return spellings).

// boundInterfaces registers every bound Go interface, mapping its Aril spelling
// to its method set (Aril method name → binding). Params/Return are Aril type
// spellings (parsed via semaTypeFromSpelling); GoName is the exported Go method.
var boundInterfaces = map[string]map[string]HandleBinding{
	// net/http Handler — the one interface healthcheck_server implements.
	// Go: ServeHTTP(w http.ResponseWriter, r *http.Request). Return unit.
	"http.Handler": {
		"serveHTTP": {GoName: "ServeHTTP", Params: []string{"http.ResponseWriter", "http.Request"}, Return: "unit"},
	},
}

// BoundInterfaceOf returns the method set of the bound interface spelled
// `spelled` (`pkg.Type`), or ok=false when it is not a bound interface.
func BoundInterfaceOf(spelled string) (map[string]HandleBinding, bool) {
	m, ok := boundInterfaces[spelled]
	return m, ok
}

// IsBoundInterface reports whether `spelled` names a bound Go interface an Aril
// class can `implements`.
func IsBoundInterface(spelled string) bool {
	_, ok := boundInterfaces[spelled]
	return ok
}

// BoundInterfaceMethodGoName returns the exported Go method name for Aril method
// `arilName` on the bound interface `spelled` (codegen's Aril→Go boundary), or
// ok=false when either is unbound.
func BoundInterfaceMethodGoName(spelled, arilName string) (string, bool) {
	m, ok := boundInterfaces[spelled]
	if !ok {
		return "", false
	}
	b, ok := m[arilName]
	if !ok {
		return "", false
	}
	return b.GoName, true
}
