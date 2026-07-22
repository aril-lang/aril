package arilrt

import (
	"errors"
	"fmt"
	"testing"
)

// The Stringer renderers (D56): `fmt`/`${}` must show the Aril value, not
// Go's raw `%v` lowering. Each case also runs through fmt.Sprintf("%v", …)
// to prove the method is actually dispatched by fmt (the whole point).

func TestListString(t *testing.T) {
	l := ListOf(1, 2, 3)
	want := "[1, 2, 3]"
	if got := l.String(); got != want {
		t.Fatalf("List.String() = %q, want %q", got, want)
	}
	if got := fmt.Sprintf("%v", l); got != want {
		t.Fatalf("fmt %%v List = %q, want %q", got, want)
	}
	if got := NewList[int]().String(); got != "[]" {
		t.Fatalf("empty List = %q, want []", got)
	}
}

func TestListStringNested(t *testing.T) {
	// A nested composite re-dispatches to its own String() via %v.
	l := ListOf(ListOf(1, 2), ListOf(3))
	want := "[[1, 2], [3]]"
	if got := fmt.Sprintf("%v", l); got != want {
		t.Fatalf("nested List = %q, want %q", got, want)
	}
}

func TestMapString(t *testing.T) {
	m := NewMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	want := "{a: 1, b: 2}" // insertion order
	if got := fmt.Sprintf("%v", m); got != want {
		t.Fatalf("Map = %q, want %q", got, want)
	}
	if got := NewMap[string, int]().String(); got != "{}" {
		t.Fatalf("empty Map = %q, want {}", got)
	}
}

func TestSetString(t *testing.T) {
	s := SetFrom([]int{3})
	if got := fmt.Sprintf("%v", s); got != "{3}" {
		t.Fatalf("Set = %q, want {3}", got)
	}
	s2 := SetFrom([]int{1, 2, 3})
	if got := s2.String(); got != "{1, 2, 3}" {
		t.Fatalf("Set = %q, want {1, 2, 3}", got)
	}
}

func TestStackString(t *testing.T) {
	s := NewStack[int]()
	s.Push(1)
	s.Push(2)
	if got := fmt.Sprintf("%v", s); got != "[1, 2]" {
		t.Fatalf("Stack = %q, want [1, 2]", got)
	}
}

func TestOptionString(t *testing.T) {
	if got := fmt.Sprintf("%v", OptionSome(5)); got != "Some(5)" {
		t.Fatalf("Some = %q, want Some(5)", got)
	}
	if got := fmt.Sprintf("%v", OptionNone[int]()); got != "None" {
		t.Fatalf("None = %q, want None", got)
	}
	// Unquoted string payload (matches Go %v element rendering, D56).
	if got := OptionSome("hi").String(); got != "Some(hi)" {
		t.Fatalf("Some(hi) = %q", got)
	}
}

func TestResultString(t *testing.T) {
	if got := fmt.Sprintf("%v", ResultOk[int, string](5)); got != "Ok(5)" {
		t.Fatalf("Ok = %q, want Ok(5)", got)
	}
	// Result<T, error>: the Err payload's %v is its Error() message.
	r := ResultErr[int, error](errors.New("boom"))
	if got := fmt.Sprintf("%v", r); got != "Err(boom)" {
		t.Fatalf("Err = %q, want Err(boom)", got)
	}
}

func TestStringerNestedInContainer(t *testing.T) {
	// List<Option<int>> — the element Stringer must fire through the outer.
	l := ListOf(OptionSome(1), OptionNone[int]())
	if got := fmt.Sprintf("%v", l); got != "[Some(1), None]" {
		t.Fatalf("List<Option> = %q, want [Some(1), None]", got)
	}
}
