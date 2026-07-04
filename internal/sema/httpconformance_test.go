package sema

import "testing"

// E0219 — structural conformance of an Aril class against a *bound* Go
// interface (D14; HTTP-SERVER epoch). `class X implements http.Handler` must
// provide `serveHTTP(w: http.ResponseWriter, r: http.Request)`; a missing or
// signature-mismatched method fires E0219 in Aril coordinates, pre-empting a
// raw `go build` "X does not implement http.Handler" leak (D10). This is the
// first structural method-set check in sema (user-interface conformance stays
// nominal-only for now).

// A correctly-conforming handler draws no conformance diagnostic. (It does not
// build end-to-end until codegen lands the server binding — that is the next
// PR — but sema accepts it.)
func TestBoundInterfaceConformanceOK(t *testing.T) {
	src := `import http
class HealthHandler implements http.Handler {
  serveHTTP(w: http.ResponseWriter, r: http.Request) {
    let _ = w.writeString("ok\n")
  }
}
func main() {}`
	if codes := runCheck(t, src); contains(codes, "E0219") {
		t.Errorf("a conforming handler must not fire E0219, got %v", codes)
	}
}

// A class implementing http.Handler but missing serveHTTP entirely.
func TestBoundInterfaceMissingMethodFiresE0219(t *testing.T) {
	src := `import http
class BadHandler implements http.Handler {
  greet(): string { return "hi" }
}
func main() {}`
	if codes := runCheck(t, src); !contains(codes, "E0219") {
		t.Errorf("expected E0219 (missing interface method), got %v", codes)
	}
}

// serveHTTP present but with the wrong arity/signature (missing the request
// parameter) is a conformance failure, not an accidental match.
func TestBoundInterfaceWrongSignatureFiresE0219(t *testing.T) {
	src := `import http
class WrongHandler implements http.Handler {
  serveHTTP(w: http.ResponseWriter) {
    let _ = w.writeString("x")
  }
}
func main() {}`
	if codes := runCheck(t, src); !contains(codes, "E0219") {
		t.Errorf("expected E0219 (signature mismatch), got %v", codes)
	}
}

// A wrong *parameter type* (not just arity) is also caught — the check is
// structural over the full signature, not a name/arity heuristic.
func TestBoundInterfaceWrongParamTypeFiresE0219(t *testing.T) {
	src := `import http
class WrongHandler implements http.Handler {
  serveHTTP(w: http.ResponseWriter, r: string) {
    let _ = w.writeString("x")
  }
}
func main() {}`
	if codes := runCheck(t, src); !contains(codes, "E0219") {
		t.Errorf("expected E0219 (wrong param type), got %v", codes)
	}
}

// A `serveHTTP` present but *static* does not satisfy the (instance) interface
// method — the check filters static methods (classMethod).
func TestBoundInterfaceStaticMethodFiresE0219(t *testing.T) {
	src := `import http
class StaticHandler implements http.Handler {
  static serveHTTP(w: http.ResponseWriter, r: http.Request) {}
}
func main() {}`
	if codes := runCheck(t, src); !contains(codes, "E0219") {
		t.Errorf("expected E0219 (static method does not satisfy instance interface), got %v", codes)
	}
}

// A wrong *return* type (interface requires unit; class returns int) is a
// conformance failure — the check is over the full signature, not just params.
func TestBoundInterfaceWrongReturnFiresE0219(t *testing.T) {
	src := `import http
class WrongHandler implements http.Handler {
  serveHTTP(w: http.ResponseWriter, r: http.Request): int { return 0 }
}
func main() {}`
	if codes := runCheck(t, src); !contains(codes, "E0219") {
		t.Errorf("expected E0219 (wrong return type), got %v", codes)
	}
}

// A class that does not implement the bound interface at all is unaffected —
// the check is scoped to declared `implements` targets (no false positive on
// an ordinary class that happens to lack serveHTTP).
func TestNonImplementingClassNoE0219(t *testing.T) {
	src := `import http
class Plain {
  greet(): string { return "hi" }
}
func main() {}`
	if codes := runCheck(t, src); contains(codes, "E0219") {
		t.Errorf("a class not implementing http.Handler must not fire E0219, got %v", codes)
	}
}
