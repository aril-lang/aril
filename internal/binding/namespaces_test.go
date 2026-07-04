package binding

import "testing"

// The two builtin-module categories: Go-backed stdlib and arilrt runtime
// (reflect, big). The importable set (IsBuiltinModule) is the union; the
// Go-machinery set (IsStdlibNamespace) is stdlib alone. Locking reflect/big
// into runtime guards the drift that made the driver misclassify `import
// reflect` as unknown.
func TestBuiltinModuleCategories(t *testing.T) {
	// Runtime modules: importable, but NOT in the Go-machinery set.
	for _, n := range []string{"reflect", "big"} {
		if !IsBuiltinModule(n) {
			t.Errorf("%s must be an importable builtin module", n)
		}
		if IsStdlibNamespace(n) {
			t.Errorf("%s is arilrt-backed; must not be in the Go-machinery stdlib set", n)
		}
	}
	// Go-backed stdlib: in both.
	for _, n := range []string{"fmt", "strings", "net", "json"} {
		if !IsBuiltinModule(n) || !IsStdlibNamespace(n) {
			t.Errorf("%s must be a Go-backed stdlib namespace (in both sets)", n)
		}
	}
	if IsBuiltinModule("mymodule") {
		t.Error("mymodule must not be a builtin module")
	}
	if len(BuiltinModules()) != len(builtinModuleSet) {
		t.Error("BuiltinModules slice and set disagree")
	}
}
