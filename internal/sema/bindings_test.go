package sema

import "testing"

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
