package sema

import "testing"

// atomic.Pointer<T> — the first-class generic atomic reference cell
// (ATOMICS-BINDING; docs/atomics-lock-free.md §Atomic references). T is a class
// reference and "nil" is None, so load()/swap() yield Option<T>. These lock the
// sema modelling: the type resolves from its qualified-generic spelling, the
// four-method set types correctly, and a miss draws E0214 (D41).

func TestAtomicPointerLoadPayloadTypesElem(t *testing.T) {
	// load() returns Option<Node>, so the Some binding types as the class Node
	// and its field access resolves — without the AtomicPtr model this typed
	// Unknown and the payload was un-inferred.
	src := `class Node { let value: int }
func f(p: atomic.Pointer<Node>) {
  match p.load() {
    Some(n) => { let a = n.value },
    None    => {},
  }
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "a"); got == nil || got.String() != "int" {
		t.Errorf("Some(n).value = %v; want int", got)
	}
}

func TestAtomicPointerMethodSetClean(t *testing.T) {
	// The whole method set types without a diagnostic: store(T)→unit,
	// load()→Option<T>, swap(Option<T>)→Option<T>, compareAndSwap(Option<T>,
	// Option<T>)→bool.
	src := `class Node { let value: int }
func f(p: atomic.Pointer<Node>, n: Node): bool {
  p.store(n)
  let old = p.load()
  let prev = p.swap(old)
  return p.compareAndSwap(old, prev)
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean atomic.Pointer method set, got %v", codes)
	}
}

func TestAtomicPointerConstructEmpty(t *testing.T) {
	// atomic.Pointer<T>{} zero-constructs an empty cell (holds None); its type
	// resolves so a field annotated with it types cleanly.
	src := `class Node { let value: int }
class Cell {
  let slot: atomic.Pointer<Node>
  static new(): Cell {
    return Cell{ slot: atomic.Pointer<Node>{} }
  }
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean atomic.Pointer construction, got %v", codes)
	}
}

func TestAtomicPointerUnknownMethodE0214(t *testing.T) {
	// A method not in the set is a genuine miss (E0214), in .aril coords — not a
	// raw go/types leak (D10/D41).
	src := `class Node { let value: int }
func f(p: atomic.Pointer<Node>) {
  p.frobnicate()
}
`
	if codes := runCheck(t, src); !hasCode(codes, "E0214") {
		t.Errorf("expected E0214 on unknown atomic.Pointer method, got %v", codes)
	}
}

func TestAtomicPointerNonEmptyLiteralE0218(t *testing.T) {
	// The cell is always constructed empty; an entry is a misuse (E0218).
	src := `class Node { let value: int }
func f(n: Node) {
  let p = atomic.Pointer<Node>{ n }
}
`
	if codes := runCheck(t, src); !hasCode(codes, "E0218") {
		t.Errorf("expected E0218 on a non-empty atomic.Pointer literal, got %v", codes)
	}
}
