package arilrt

import (
	"encoding/json"
	"testing"
)

func TestOptionResult(t *testing.T) {
	if got := OptionSome(7); got.Tag != 1 || got.V != 7 {
		t.Fatalf("OptionSome: %+v", got)
	}
	if got := OptionNone[int](); got.Tag != 0 {
		t.Fatalf("OptionNone: %+v", got)
	}
	if got := ResultOk[int, string](3); got.Tag != 0 || got.V != 3 {
		t.Fatalf("ResultOk: %+v", got)
	}
	if got := ResultErr[int, string]("boom"); got.Tag != 1 || got.E != "boom" {
		t.Fatalf("ResultErr: %+v", got)
	}
}

func TestResultOfUnit(t *testing.T) {
	// ResultOf folds (value, nil) → Ok, (zero, err) → Err.
	if got := ResultOf(42, error(nil)); got.Tag != 0 || got.V != 42 {
		t.Fatalf("ResultOf ok: %+v", got)
	}
	if got := ResultOf(0, errString("x")); got.Tag != 1 {
		t.Fatalf("ResultOf err: %+v", got)
	}
	if got := ResultUnit(nil); got.Tag != 0 {
		t.Fatalf("ResultUnit ok: %+v", got)
	}
	if got := ResultUnit(errString("x")); got.Tag != 1 {
		t.Fatalf("ResultUnit err: %+v", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestMapInsertionOrder(t *testing.T) {
	m := NewMap[string, int]()
	m.Set("b", 2)
	m.Set("a", 1)
	m.Set("b", 20) // update must not reorder
	if got := m.Keys(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("Keys order: %v", got)
	}
	if got := m.Get("b"); got.Tag != 1 || got.V != 20 {
		t.Fatalf("Get b: %+v", got)
	}
	if got := m.Get("missing"); got.Tag != 0 {
		t.Fatalf("Get missing should be None: %+v", got)
	}
	m.Delete("b")
	if m.Has("b") || m.Len() != 1 {
		t.Fatalf("after delete: len=%d hasB=%v", m.Len(), m.Has("b"))
	}
}

func TestSetDedupAndOrder(t *testing.T) {
	s := SetFrom([]int{3, 1, 3, 2, 1})
	if got := s.ToSlice(); len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("SetFrom order/dedup: %v", got)
	}
}

func TestStackPopEmpty(t *testing.T) {
	s := NewStack[int]()
	if got := s.Pop(); got.Tag != 1 || got.E.Error() != "empty stack" {
		t.Fatalf("Pop empty should Err: %+v", got)
	}
	s.Push(5)
	if got := s.Peek(); got.Tag != 1 || got.V != 5 {
		t.Fatalf("Peek: %+v", got)
	}
	if got := s.Pop(); got.Tag != 0 || got.V != 5 {
		t.Fatalf("Pop: %+v", got)
	}
}

func TestSorted(t *testing.T) {
	in := []int{3, 1, 2}
	out := Sorted(in, func(a, b int) bool { return a < b })
	if out[0] != 1 || out[1] != 2 || out[2] != 3 {
		t.Fatalf("Sorted: %v", out)
	}
	// must not mutate the input (Aril immutability).
	if in[0] != 3 {
		t.Fatalf("Sorted mutated input: %v", in)
	}
}

func TestTryRecv(t *testing.T) {
	ch := make(chan int, 1)
	if got := TryRecv(ch); got.Tag != 0 {
		t.Fatalf("TryRecv empty should be None: %+v", got)
	}
	ch <- 9
	if got := TryRecv(ch); got.Tag != 1 || got.V != 9 {
		t.Fatalf("TryRecv ready: %+v", got)
	}
}

func TestOptionJSONRoundTrip(t *testing.T) {
	// None ⇄ null, Some(v) ⇄ the bare JSON of v.
	b, _ := json.Marshal(OptionNone[int]())
	if string(b) != "null" {
		t.Fatalf("None marshals to %q", b)
	}
	b, _ = json.Marshal(OptionSome(5))
	if string(b) != "5" {
		t.Fatalf("Some marshals to %q", b)
	}
	var o Option[int]
	if err := json.Unmarshal([]byte("null"), &o); err != nil || o.Tag != 0 {
		t.Fatalf("unmarshal null: %+v err=%v", o, err)
	}
	if err := json.Unmarshal([]byte("8"), &o); err != nil || o.Tag != 1 || o.V != 8 {
		t.Fatalf("unmarshal value: %+v err=%v", o, err)
	}
}

func TestReflectBoxUnbox(t *testing.T) {
	d := Box(123)
	if TypeName(TypeOf(d)) != "int" {
		t.Fatalf("typeName: %q", TypeName(TypeOf(d)))
	}
	// Descriptor identity (CT1): same Go type shares one descriptor.
	if Box(1).Desc != Box(2).Desc {
		t.Fatalf("descriptor identity broken for int")
	}
	if got := Unbox[int](d); got.Tag != 0 || got.V != 123 {
		t.Fatalf("Unbox int: %+v", got)
	}
	if got := Unbox[string](d); got.Tag != 1 {
		t.Fatalf("Unbox wrong type should Err: %+v", got)
	}
	if Show(Box("hi")) != `"hi"` {
		t.Fatalf("Show string: %q", Show(Box("hi")))
	}
	if Show(Box(true)) != "true" {
		t.Fatalf("Show bool: %q", Show(Box(true)))
	}
}
