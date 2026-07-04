package binding

import "testing"

// TestMembershipAccessors locks the consolidated overlay accessors moved here
// from internal/codegen (renameOverlay / conversion / commaOk / DurationUnit),
// so sema and codegen read one source.
func TestMembershipAccessors(t *testing.T) {
	if g, ok := RenameOverlayOf("fmt", "println"); !ok || g != "Println" {
		t.Errorf(`RenameOverlayOf(fmt.println) = (%q,%v); want ("Println",true)`, g, ok)
	}
	if g, ok := RenameOverlayOf("slices", "contains"); !ok || g != "Contains" {
		t.Errorf(`RenameOverlayOf(slices.contains) = (%q,%v); want ("Contains",true)`, g, ok)
	}
	if _, ok := RenameOverlayOf("fmt", "nope"); ok {
		t.Error("RenameOverlayOf(fmt.nope) should be false")
	}
	if g, ok := ConversionOf("strings", "fromBytes"); !ok || g != "string" {
		t.Errorf(`ConversionOf(strings.fromBytes) = (%q,%v); want ("string",true)`, g, ok)
	}
	if g, ok := CommaOkOf("os", "lookupEnv"); !ok || g != "LookupEnv" {
		t.Errorf(`CommaOkOf(os.lookupEnv) = (%q,%v); want ("LookupEnv",true)`, g, ok)
	}
	if g, ok := DurationUnitOf("seconds"); !ok || g != "Second" {
		t.Errorf(`DurationUnitOf(seconds) = (%q,%v); want ("Second",true)`, g, ok)
	}
	if _, ok := DurationUnitOf("after"); ok {
		t.Error("DurationUnitOf(after) should be false — it is a rename")
	}
}

// TestIsMember checks the folded membership predicate across every source, and
// (critically for sema's sound-over-complete diagnostic) that a genuinely
// unbound member on a real namespace is NOT a member.
func TestIsMember(t *testing.T) {
	members := [][2]string{
		{"strings", "split"},      // registry Rename
		{"strconv", "atoi"},       // registry ResultWrap
		{"fmt", "println"},        // rename overlay
		{"slices", "max"},         // rename overlay
		{"strings", "fromBytes"},  // conversion
		{"os", "lookupEnv"},       // comma-ok
		{"time", "seconds"},       // duration unit
		{"regexp", "mustCompile"}, // handle ctor
		{"big", "fromInt"},        // handle ctor (runtime-backed)
		{"json", "parse"},         // idiom (codegen/sema generic)
		{"errors", "as"},          // idiom (generic binding)
		{"fmt", "scan2"},          // idiom (stdin intercept)
		{"sort", "sortedBy"},      // idiom (runtime helper)
		{"slices", "reverse"},     // idiom (runtime helper)
		{"reflect", "typeName"},   // idiom (codegen-owned reflect)
	}
	for _, m := range members {
		if !IsMember(m[0], m[1]) {
			t.Errorf("IsMember(%q, %q) = false; want true", m[0], m[1])
		}
	}
	nonMembers := [][2]string{
		{"strings", "nonexistentMember"},
		{"sync", "frobnicate"},
		{"reflect", "methods"}, // recognized-but-unimplemented → not yet a member
		{"time", "after2"},
	}
	for _, m := range nonMembers {
		if IsMember(m[0], m[1]) {
			t.Errorf("IsMember(%q, %q) = true; want false", m[0], m[1])
		}
	}
}
