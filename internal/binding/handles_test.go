package binding

import "testing"

// TestHandleCtorLookup locks the value-handle constructor table: a handle
// constructor resolves to its Go builder + the handle's Aril type spelling, and
// a non-constructor pair misses.
func TestHandleCtorLookup(t *testing.T) {
	b, ok := HandleCtorOf("regexp", "mustCompile")
	if !ok {
		t.Fatal("regexp.mustCompile should be a handle constructor")
	}
	if b.GoName != "MustCompile" || b.Return != "regexp.Regexp" {
		t.Errorf("regexp.mustCompile = %+v; want MustCompile → regexp.Regexp", b)
	}
	if _, ok := HandleCtorOf("regexp", "nope"); ok {
		t.Error("regexp.nope should not be a handle constructor")
	}
}

// TestHandleMethodLookup locks the value-handle method table: a bound method
// resolves to its Go name + return spelling; an unbound method and an unknown
// handle both miss. IsHandleType agrees with the method table.
func TestHandleMethodLookup(t *testing.T) {
	m, ok := HandleMethodOf("regexp.Regexp", "findAll")
	if !ok {
		t.Fatal("regexp.Regexp.findAll should be bound")
	}
	if m.GoName != "FindAllString" || m.Return != "[]string" {
		t.Errorf("findAll = %+v; want FindAllString → []string", m)
	}
	if _, ok := HandleMethodOf("regexp.Regexp", "nope"); ok {
		t.Error("regexp.Regexp.nope should not be bound")
	}
	if _, ok := HandleMethodOf("os.File", "read"); ok {
		t.Error("os.File is not a bound handle type")
	}
	if !IsHandleType("regexp.Regexp") {
		t.Error("regexp.Regexp should be a handle type")
	}
	if IsHandleType("time.Time") {
		t.Error("time.Time is not (yet) a bound handle type")
	}
}

// TestHandleType locks the handle-type lowering: the Go type spelling is
// pointer-correct (regexp.MustCompile returns *regexp.Regexp) and carries the
// Go import package, both of which differ from the Aril spelling.
func TestHandleType(t *testing.T) {
	ht, ok := HandleTypeOf("regexp.Regexp")
	if !ok {
		t.Fatal("regexp.Regexp should be a handle type")
	}
	if ht.GoType != "*regexp.Regexp" {
		t.Errorf("regexp.Regexp GoType = %q; want *regexp.Regexp", ht.GoType)
	}
	if ht.GoPkg != "regexp" {
		t.Errorf("regexp.Regexp GoPkg = %q; want regexp", ht.GoPkg)
	}
	if _, ok := HandleTypeOf("time.Time"); ok {
		t.Error("time.Time is not (yet) a bound handle type")
	}
}
