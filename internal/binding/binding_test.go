package binding

import "testing"

// TestLookupKinds spot-checks the derived registry's two lowering shapes and
// the return spellings, reading the committed registry (no Go source tree
// needed — that is the point of committing it).
func TestLookupKinds(t *testing.T) {
	// A value/effect rename: strconv.itoa → Itoa, returns a bare string.
	if f, ok := Lookup("strconv", "itoa"); !ok || f.GoName != "Itoa" || f.Kind != Rename || f.Return != "string" {
		t.Errorf("strconv.itoa = %+v, %v; want {Itoa, Rename, string}", f, ok)
	}
	// A (T, error) result-wrap: strconv.atoi → Atoi, Result<int, error>.
	if f, ok := Lookup("strconv", "atoi"); !ok || f.GoName != "Atoi" || f.Kind != ResultWrap || f.Return != "Result<int, error>" {
		t.Errorf("strconv.atoi = %+v, %v; want {Atoi, ResultWrap, Result<int, error>}", f, ok)
	}
	// The #43 fact, now derived: a directional channel return.
	if f, ok := Lookup("time", "after"); !ok || f.GoName != "After" || f.Kind != Rename || f.Return != "RecvChan<time.Time>" {
		t.Errorf("time.after = %+v, %v; want {After, Rename, RecvChan<time.Time>}", f, ok)
	}
}

// TestNewMechanicalRows locks the STDLIB-COVERAGE additions, including the
// bare-`error` vs `(T, error)` distinction the codegen lift depends on:
// os.writeFile returns a bare error → Result<unit, error> (lowered via
// ResultUnit), not a `(T, error)` pair (ResultOf). A regression here would
// re-introduce the `not enough arguments in call to ResultOf` miscompile.
func TestNewMechanicalRows(t *testing.T) {
	cases := []struct {
		pkg, name, goName, ret string
		kind                   Kind
	}{
		{"errors", "is", "Is", "bool", Rename},
		{"math", "max", "Max", "float64", Rename},
		{"math", "min", "Min", "float64", Rename},
		{"strconv", "parseBool", "ParseBool", "Result<bool, error>", ResultWrap},
		{"strconv", "formatFloat", "FormatFloat", "string", Rename},
		// Bare-error effect: lifted to Result<unit, error>, NOT Result<T, error>.
		{"os", "writeFile", "WriteFile", "Result<unit, error>", ResultWrap},
	}
	for _, c := range cases {
		f, ok := Lookup(c.pkg, c.name)
		if !ok || f.GoName != c.goName || f.Kind != c.kind || f.Return != c.ret {
			t.Errorf("%s.%s = %+v, %v; want {%s, kind=%d, %s}", c.pkg, c.name, f, ok, c.goName, c.kind, c.ret)
		}
	}
}

// TestLookupHelpers checks the kind-filtered accessors the consumers use.
func TestLookupHelpers(t *testing.T) {
	if g, ok := RenameOf("strings", "split"); !ok || g != "Split" {
		t.Errorf("RenameOf(strings.split) = %q, %v; want Split", g, ok)
	}
	// A result-wrap binding is not a Rename.
	if _, ok := RenameOf("strconv", "atoi"); ok {
		t.Error("RenameOf(strconv.atoi) should be false — it is a ResultWrap")
	}
	if g, ok := ResultWrapOf("os", "readFile"); !ok || g != "ReadFile" {
		t.Errorf("ResultWrapOf(os.readFile) = %q, %v; want ReadFile", g, ok)
	}
	// A rename binding is not a ResultWrap.
	if _, ok := ResultWrapOf("strings", "split"); ok {
		t.Error("ResultWrapOf(strings.split) should be false — it is a Rename")
	}
	if r, ok := ReturnSpelling("os", "args"); !ok || r != "[]string" {
		t.Errorf("ReturnSpelling(os.args) = %q, %v; want []string", r, ok)
	}
	// An effect binding (os.exit) has an empty return spelling.
	if r, ok := ReturnSpelling("os", "exit"); !ok || r != "" {
		t.Errorf("ReturnSpelling(os.exit) = %q, %v; want empty", r, ok)
	}
	// An unregistered pair.
	if _, ok := Lookup("strings", "nope"); ok {
		t.Error("Lookup(strings.nope) should be false")
	}
}
