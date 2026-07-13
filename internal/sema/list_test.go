package sema

import "testing"

// List<T> — the reference-backed growable sequence (builtins.md §List, D55).
// Mutating methods return unit and mutate in place; pop/get/removeAt return
// Option (None on empty / out-of-range); l[i] indexes to the element type.

func TestListBraceLitEmptyTypesList(t *testing.T) {
	src := `func f() {
  let xs = List<int>{}
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "xs"); got == nil || got.String() != "List<int>" {
		t.Errorf("List<int>{} = %v; want List<int>", got)
	}
}

func TestListBraceLitInitializedTypesList(t *testing.T) {
	src := `func f() {
  let xs = List<int>{1, 2, 3}
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "xs"); got == nil || got.String() != "List<int>" {
		t.Errorf("List<int>{1,2,3} = %v; want List<int>", got)
	}
}

// An initialized-literal element that mismatches the declared T is E0201 —
// unlike Stack (which rejects any entry), List accepts entries but type-checks
// each against the element type.
func TestListBraceLitElementMismatchFiresE0201(t *testing.T) {
	src := `func f() {
  let xs = List<int>{1, "two"}
}
`
	if codes := runCheck(t, src); !contains(codes, "E0201") {
		t.Errorf("expected E0201 (list element type mismatch), got %v", codes)
	}
}

func TestListPopPayloadTypesOptionElem(t *testing.T) {
	src := `func f(l: List<int>) {
  match l.pop() {
    Some(v) => { let a = v },
    None    => {},
  }
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "v"); got == nil || got.String() != "int" {
		t.Errorf("Some(v) payload of l.pop() = %v; want int", got)
	}
}

func TestListGetPayloadTypesOptionElem(t *testing.T) {
	src := `func f(l: List<string>, i: int) {
  match l.get(i) {
    Some(v) => { let a = v },
    None    => {},
  }
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "v"); got == nil || got.String() != "string" {
		t.Errorf("Some(v) payload of l.get(i) = %v; want string", got)
	}
}

// The mutating method set (push/set/insert/removeAt) + len + toSlice typecheck
// cleanly: push/set/insert are unit, len is int, toSlice is []T.
func TestListMutatingMethodsClean(t *testing.T) {
	src := `func f(l: List<int>): int {
  l.push(1)
  l.set(0, 2)
  l.insert(0, 3)
  let _ = l.removeAt(0)
  let s = l.toSlice()
  return l.len() + s.len()
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (push/set/insert:unit, len:int, toSlice:[]int), got %v", codes)
	}
}

// l[i] indexes to the element type (T-Index-List) — the raw read backing At.
func TestListIndexTypesElem(t *testing.T) {
	src := `func f(l: List<int>) {
  let x = l[0]
}
`
	info := checkInfo(t, src)
	if got := defTypeByName(info, "x"); got == nil || got.String() != "int" {
		t.Errorf("l[0] = %v; want int", got)
	}
}

// A List is iterable: `for x in l` binds the element; `for (i, x) in l` binds
// (int, element) — unlike Stack, which is deliberately not iterable.
func TestListIterationBindsElem(t *testing.T) {
	src := `func f(l: List<int>): int {
  var total = 0
  for x in l { total = total + x }
  for (i, x) in l { total = total + i + x }
  return total
}
`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (for x / for (i,x) over List), got %v", codes)
	}
}

// An unknown member on a List is a genuine miss (fully-known method set) → E0214,
// not a raw go/types leak (D38 sound-over-complete; builtinMemberMiss covers List).
func TestListUnknownMemberFiresE0214(t *testing.T) {
	src := `func f(l: List<int>) { let _ = l.frobnicate() }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown List member), got %v", codes)
	}
}

// List is a reserved built-in type name (E0118) — a user class cannot redeclare it.
func TestListNameReservedFiresE0118(t *testing.T) {
	src := `class List<T> { }`
	if codes := runCheck(t, src); !contains(codes, "E0118") {
		t.Errorf("expected E0118 (List is a built-in type), got %v", codes)
	}
}
