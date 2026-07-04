package sema

import "testing"

// E0218 — a bound stdlib handle cannot be brace-built freely: a constructable
// one takes no fields, an obtain-only one is never a `{}` literal. Diagnosed in
// Aril coordinates rather than leaking a codegen bail string (D10).

func TestConstructableHandleWithFieldsFiresE0218(t *testing.T) {
	src := `import sync
func use() { let mu = sync.Mutex{x: 1}  mu.lock() }`
	if codes := runCheck(t, src); !contains(codes, "E0218") {
		t.Errorf("expected E0218 (handle takes no fields), got %v", codes)
	}
}

func TestObtainOnlyHandleBraceLitFiresE0218(t *testing.T) {
	src := `import regexp
func use() { let r = regexp.Regexp{} }`
	if codes := runCheck(t, src); !contains(codes, "E0218") {
		t.Errorf("expected E0218 (obtain-only handle), got %v", codes)
	}
}

// A constructable handle built empty (`sync.Mutex{}`) must NOT fire E0218 — it
// is the valid zero-construction form.
func TestConstructableHandleEmptySilentE0218(t *testing.T) {
	src := `import sync
func use() { let mu = sync.Mutex{}  mu.lock() }`
	if codes := runCheck(t, src); contains(codes, "E0218") {
		t.Errorf("E0218 must not fire on the valid `sync.Mutex{}`, got %v", codes)
	}
}
