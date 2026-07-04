package sema

import (
	"testing"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/binding"
)

// TestHandleCtorReturn locks the value-handle constructor row: regexp.mustCompile
// types as the opaque handle boundary type regexp.Regexp (a *Named), so a `let re
// = regexp.mustCompile(p)` propagates the handle type to method calls on `re`.
func TestHandleCtorReturn(t *testing.T) {
	c := &checker{}
	got := c.stdlibBindingReturn("regexp", "mustCompile")
	n, ok := got.(*Named)
	if !ok || n.N != "regexp.Regexp" {
		t.Fatalf("stdlibBindingReturn(regexp, mustCompile) = %v; want Named regexp.Regexp", got)
	}
}

// TestHandleAnnotationResolvesToNamed locks the C1 fix: a qualified handle
// annotation (`regexp.Regexp` in a param/field/return) resolves to the same
// *Named boundary type a locally-constructed handle has, so codegen's handle
// dispatch fires on either origin (no sema↔codegen split). A non-handle
// qualified name stays Unknown.
func TestHandleAnnotationResolvesToNamed(t *testing.T) {
	c := &checker{}
	nt := &ast.NamedType{QName: []string{"regexp", "Regexp"}}
	got := c.namedTypeToType(nt, map[string]bool{})
	if n, ok := got.(*Named); !ok || n.N != "regexp.Regexp" {
		t.Fatalf("regexp.Regexp annotation = %v; want Named regexp.Regexp", got)
	}
	other := &ast.NamedType{QName: []string{"time", "Time"}}
	if _, ok := c.namedTypeToType(other, map[string]bool{}).(*Unknown); !ok {
		t.Errorf("unbound qualified name should stay Unknown, got %T", c.namedTypeToType(other, map[string]bool{}))
	}
}

// TestHandleMethodSigType locks the value-handle method rows: a bound handle
// method builds a Func with the tabled param + return types, so the call is
// arg-checked and typed rather than Unknown (D37).
func TestHandleMethodSigType(t *testing.T) {
	hm, ok := binding.HandleMethodOf("regexp.Regexp", "matchString")
	if !ok {
		t.Fatal("regexp.Regexp.matchString should be a bound handle method")
	}
	fn := handleMethodSigType(hm)
	if b, ok := fn.Return.(*Builtin); !ok || b.N != "bool" {
		t.Errorf("matchString return = %v; want Builtin bool", fn.Return)
	}
	if len(fn.Params) != 1 {
		t.Fatalf("matchString params = %d; want 1", len(fn.Params))
	}
	if b, ok := fn.Params[0].(*Builtin); !ok || b.N != "string" {
		t.Errorf("matchString param0 = %v; want Builtin string", fn.Params[0])
	}
	find, _ := binding.HandleMethodOf("regexp.Regexp", "findAll")
	if s, ok := handleMethodSigType(find).Return.(*Slice); !ok {
		t.Errorf("findAll return = %v; want *Slice", handleMethodSigType(find).Return)
	} else if b, ok := s.Elem.(*Builtin); !ok || b.N != "string" {
		t.Errorf("findAll elem = %v; want Builtin string", s.Elem)
	}
}

// TestNetHandleMethodSigType locks the net socket method rows: net.Conn.read
// types as a Func returning Result<int, error> (the first handle methods with a
// Result return), and net.listen types as Result<net.Listener, error> — a
// handle Named inside a Result, so `let ln = net.listen(...)` propagates the
// Listener handle to `ln.accept()`.
func TestNetHandleMethodSigType(t *testing.T) {
	read, ok := binding.HandleMethodOf("net.Conn", "read")
	if !ok {
		t.Fatal("net.Conn.read should be a bound handle method")
	}
	r, ok := handleMethodSigType(read).Return.(*Result)
	if !ok {
		t.Fatalf("net.Conn.read return = %v; want *Result", handleMethodSigType(read).Return)
	}
	if b, ok := r.T.(*Builtin); !ok || b.N != "int" {
		t.Errorf("net.Conn.read Result payload = %v; want Builtin int", r.T)
	}
	c := &checker{}
	got := c.stdlibBindingReturn("net", "listen")
	lr, ok := got.(*Result)
	if !ok {
		t.Fatalf("stdlibBindingReturn(net, listen) = %v; want *Result", got)
	}
	if n, ok := lr.T.(*Named); !ok || n.N != "net.Listener" {
		t.Errorf("net.listen Result payload = %v; want Named net.Listener", lr.T)
	}
	// A `net.Conn` param annotation resolves to the same handle Named a
	// dialed/accepted connection carries (no sema↔codegen split).
	nt := &ast.NamedType{QName: []string{"net", "Conn"}}
	if n, ok := c.namedTypeToType(nt, map[string]bool{}).(*Named); !ok || n.N != "net.Conn" {
		t.Errorf("net.Conn annotation = %v; want Named net.Conn", c.namedTypeToType(nt, map[string]bool{}))
	}
}

// TestSemaTypeFromSpelling round-trips the binding-registry return spellings
// through the bridge: spelling → sema Type → String() must reproduce the
// spelling, over every construct the stdlib registry uses. This locks the
// bridge that replaced the former hand-built return-type switch.
func TestSemaTypeFromSpelling(t *testing.T) {
	for _, s := range []string{
		"int", "int64", "float64", "string", "bool", "byte",
		"[]string", "[]byte",
		"Result<int, error>", "Result<int64, error>", "Result<float64, error>",
		"Result<[]byte, error>",
		"RecvChan<time.Time>",
	} {
		if got := semaTypeFromSpelling(s).String(); got != s {
			t.Errorf("semaTypeFromSpelling(%q).String() = %q; want round-trip", s, got)
		}
	}
}

// TestBareErrorConstructorReturns locks the idiom rows for the bare-`error`
// constructors (errors.new / fmt.errorf): they return a plain `error` value,
// NOT a Result<unit, error> — they are constructors, not failure-signalling
// effects, so they must not be confused with the registry's bare-error lift.
func TestBareErrorConstructorReturns(t *testing.T) {
	c := &checker{}
	for _, pm := range [][2]string{{"errors", "new"}, {"fmt", "errorf"}} {
		got := c.stdlibBindingReturn(pm[0], pm[1])
		b, ok := got.(*Builtin)
		if !ok || b.N != "error" {
			t.Errorf("stdlibBindingReturn(%s, %s) = %v; want Builtin error", pm[0], pm[1], got)
		}
	}
}

// TestCommaOkOptionReturn locks the comma-ok idiom row: os.lookupEnv types as
// Option<string> (a `(T, bool)` Go referent lifted to Option<T>, not Result).
func TestCommaOkOptionReturn(t *testing.T) {
	c := &checker{}
	got := c.stdlibBindingReturn("os", "lookupEnv")
	o, ok := got.(*Option)
	if !ok {
		t.Fatalf("stdlibBindingReturn(os, lookupEnv) = %v; want *Option", got)
	}
	if b, ok := o.T.(*Builtin); !ok || b.N != "string" {
		t.Errorf("os.lookupEnv Option payload = %v; want Builtin string", o.T)
	}
}

// TestSemaTypeFromSpellingShape checks the structural mapping, not just the
// stringification: a leaf with a dot is a Named (opaque boundary type), a bare
// primitive is a Builtin, and the registry's (T, error) lift is a Result.
func TestSemaTypeFromSpellingShape(t *testing.T) {
	if _, ok := semaTypeFromSpelling("time.Time").(*Named); !ok {
		t.Errorf("time.Time should map to *Named, got %T", semaTypeFromSpelling("time.Time"))
	}
	if _, ok := semaTypeFromSpelling("int").(*Builtin); !ok {
		t.Errorf("int should map to *Builtin, got %T", semaTypeFromSpelling("int"))
	}
	r, ok := semaTypeFromSpelling("Result<int, error>").(*Result)
	if !ok {
		t.Fatalf("Result<int, error> should map to *Result, got %T", semaTypeFromSpelling("Result<int, error>"))
	}
	if b, ok := r.T.(*Builtin); !ok || b.N != "int" {
		t.Errorf("Result payload = %v; want Builtin int", r.T)
	}
	if rc, ok := semaTypeFromSpelling("RecvChan<time.Time>").(*RecvChan); !ok {
		t.Errorf("RecvChan<time.Time> should map to *RecvChan, got %T", semaTypeFromSpelling("RecvChan<time.Time>"))
	} else if n, ok := rc.Elem.(*Named); !ok || n.N != "time.Time" {
		t.Errorf("RecvChan elem = %v; want Named time.Time", rc.Elem)
	}
}
