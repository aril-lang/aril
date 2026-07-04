package binding

import (
	"strings"
	"testing"
)

// http.Handler is a bound interface with the serveHTTP method mapping to Go's
// ServeHTTP.
func TestBoundInterfaceHTTPHandler(t *testing.T) {
	if !IsBoundInterface("http.Handler") {
		t.Fatal("http.Handler should be a bound interface")
	}
	methods, ok := BoundInterfaceOf("http.Handler")
	if !ok || len(methods) != 1 {
		t.Fatalf("http.Handler should have one method, got %v", methods)
	}
	if goName, ok := BoundInterfaceMethodGoName("http.Handler", "serveHTTP"); !ok || goName != "ServeHTTP" {
		t.Errorf("serveHTTP should map to Go ServeHTTP, got %q ok=%v", goName, ok)
	}
	if _, ok := BoundInterfaceMethodGoName("http.Handler", "nope"); ok {
		t.Error("unknown method should not resolve a Go name")
	}
}

func TestUnknownIsNotBoundInterface(t *testing.T) {
	if IsBoundInterface("http.NotAThing") {
		t.Error("http.NotAThing is not a bound interface")
	}
	if IsBoundInterface("regexp.Regexp") {
		t.Error("a handle type is not a bound interface")
	}
}

// Lockstep guard: every qualified (`pkg.Type`) parameter/return spelling a bound
// interface method mentions must itself be a registered handle type — otherwise
// sema would type the conformance signature against an Unknown boundary and the
// structural check (equal) would silently pass a mismatch. Catches drift when a
// bound interface gains a method referencing a not-yet-registered handle.
func TestBoundInterfaceParamsAreRegisteredHandles(t *testing.T) {
	for iface, methods := range boundInterfaces {
		for name, sig := range methods {
			for _, spelled := range append(append([]string{}, sig.Params...), sig.Return) {
				spelled = strings.TrimPrefix(spelled, "[]")
				// Only qualified names (`pkg.Type`) must be handle types; bare
				// primitives (int/string/unit) and generic wrappers (Result<…>)
				// are typed directly by sema.
				if !strings.Contains(spelled, ".") || strings.Contains(spelled, "<") {
					continue
				}
				if !IsHandleType(spelled) && !IsBoundInterface(spelled) {
					t.Errorf("%s.%s references %q, which is not a registered handle type or bound interface",
						iface, name, spelled)
				}
			}
		}
	}
}
