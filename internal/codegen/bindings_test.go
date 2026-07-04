package codegen

import (
	"testing"

	"github.com/aril-lang/aril/internal/binding"
)

// TestStdlibRenameOf locks the rename registry's gating: a stdlib
// namespace + known method renames to the Go identifier; a non-stdlib
// receiver or an unknown method does not. It also guards the boundary
// with the conversion bindings — `strings.fromBytes` must NOT be a
// rename (it lowers to a Go conversion, handled separately), or the
// import-tracking and call lowering would diverge.
func TestStdlibRenameOf(t *testing.T) {
	cases := []struct {
		recv, name string
		want       string
		ok         bool
	}{
		{"strings", "split", "Split", true},
		{"strconv", "itoa", "Itoa", true},
		{"os", "args", "Args", true},
		{"fmt", "println", "Println", true},
		{"time", "after", "After", true},
		{"time", "sleep", "Sleep", true},
		// Not renames: result-wrapping bindings go through emitCall.
		{"strconv", "atoi", "", false},
		{"os", "readFile", "", false},
		// Not a rename: conversion binding, lowered to `string(b)`.
		{"strings", "fromBytes", "", false},
		// Not a stdlib namespace.
		{"myObj", "split", "", false},
		// Unknown method on a real namespace.
		{"strings", "noSuchMethod", "", false},
	}
	for _, c := range cases {
		got, ok := stdlibRenameOf(c.recv, c.name)
		if got != c.want || ok != c.ok {
			t.Errorf("stdlibRenameOf(%q, %q) = (%q, %v); want (%q, %v)",
				c.recv, c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestStdlibResultWrapOf locks the (T, error) → Result registry.
func TestStdlibResultWrapOf(t *testing.T) {
	if got, ok := stdlibResultWrapOf("strconv", "atoi"); !ok || got != "Atoi" {
		t.Errorf(`stdlibResultWrapOf("strconv","atoi") = (%q,%v); want ("Atoi",true)`, got, ok)
	}
	if got, ok := stdlibResultWrapOf("os", "readFile"); !ok || got != "ReadFile" {
		t.Errorf(`stdlibResultWrapOf("os","readFile") = (%q,%v); want ("ReadFile",true)`, got, ok)
	}
	if got, ok := stdlibResultWrapOf("strconv", "parseFloat"); !ok || got != "ParseFloat" {
		t.Errorf(`stdlibResultWrapOf("strconv","parseFloat") = (%q,%v); want ("ParseFloat",true)`, got, ok)
	}
	// A rename binding is not a result-wrap binding.
	if _, ok := stdlibResultWrapOf("strconv", "itoa"); ok {
		t.Errorf(`stdlibResultWrapOf("strconv","itoa") should be false`)
	}
}

// TestStdlibConversionExclusivity locks the three-way exclusivity the
// import-tracking depends on: `strings.fromBytes` is a *conversion*
// (→ `string(b)`, no import) and must be neither a rename nor a
// result-wrap. A drift here would mis-track the `strings` import.
func TestStdlibConversionExclusivity(t *testing.T) {
	if got, ok := stdlibConversionOf("strings", "fromBytes"); !ok || got != "string" {
		t.Errorf(`stdlibConversionOf("strings","fromBytes") = (%q,%v); want ("string",true)`, got, ok)
	}
	if _, ok := stdlibRenameOf("strings", "fromBytes"); ok {
		t.Errorf("fromBytes must not be a rename binding")
	}
	if _, ok := stdlibResultWrapOf("strings", "fromBytes"); ok {
		t.Errorf("fromBytes must not be a result-wrap binding")
	}
	// isConversionBinding must agree with the table (single source).
	if !isConversionBinding("strings", "fromBytes") {
		t.Errorf("isConversionBinding disagrees with stdlibConversion table")
	}
	if isConversionBinding("strings", "split") {
		t.Errorf("isConversionBinding(split) should be false")
	}
}

// TestTimeDurationUnitNotRename locks the codegen-side exclusivity: the
// Duration constructors (now binding.DurationUnitOf — they lower to
// `time.Duration(n) * time.<Unit>`) must NOT also resolve as a rename, or the
// call lowering would diverge. The unit mapping itself is tested in
// internal/binding (TestMembershipAccessors).
func TestTimeDurationUnitNotRename(t *testing.T) {
	if got, ok := binding.DurationUnitOf("milliseconds"); !ok || got != "Millisecond" {
		t.Errorf(`binding.DurationUnitOf("milliseconds") = (%q,%v); want ("Millisecond",true)`, got, ok)
	}
	// The Duration constructors must not also be in the rename table.
	if _, ok := stdlibRenameOf("time", "milliseconds"); ok {
		t.Errorf(`time.milliseconds must not be a rename binding`)
	}
}

// TestStdlibNamespaceMachinerySet locks codegen's binding-machinery namespace
// predicate to binding's Go-backed stdlib set (the importable union minus the
// arilrt runtime modules), so a drift back to a hand-copied list is caught.
func TestStdlibNamespaceMachinerySet(t *testing.T) {
	for _, n := range binding.BuiltinModules() {
		want := binding.IsStdlibNamespace(n)
		if got := isStdlibNamespaceName(n); got != want {
			t.Errorf("isStdlibNamespaceName(%q) = %v; want %v", n, got, want)
		}
	}
	// The arilrt runtime modules are importable but not Go-machinery.
	for _, n := range []string{"reflect", "big"} {
		if isStdlibNamespaceName(n) {
			t.Errorf("isStdlibNamespaceName(%q) should be false (arilrt runtime module)", n)
		}
	}
}
