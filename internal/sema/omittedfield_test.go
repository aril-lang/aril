package sema

import "testing"

// A record/class literal may omit fields with a safe zero value — scalars,
// Option, reference-containers, records built from those — but omitting a
// bare user `class` field (a nil pointer with no empty constructor) is the
// unsound partial build E0220. See lowering-go.md §Container defaulting.
func TestOmittedFieldPolicy(t *testing.T) {
	// Omitting a scalar / container / Option field is fine (partial construction).
	safe := `import fmt
type Bag = { items: List<int>, tag: Option<int>, n: int }
func main() {
  let b = Bag{ n: 5 }
  fmt.println(b.n)
}
`
	if codes := runCheck(t, safe); hasCode(codes, "E0220") {
		t.Errorf("omitting scalar/container/Option fields is safe, got %v", codes)
	}

	// Omitting a bare user `class` field → E0220 (nil landmine).
	bare := `import fmt
class Box { var v: int }
type Rec = { b: Box, n: int }
func main() {
  let r = Rec{ n: 5 }
  fmt.println(r.n)
}
`
	if codes := runCheck(t, bare); !hasCode(codes, "E0220") {
		t.Errorf("want E0220 for an omitted bare class field, got %v", codes)
	}

	// Providing the class field silences it.
	provided := `import fmt
class Box { var v: int }
type Rec = { b: Box, n: int }
func main() {
  let r = Rec{ b: Box{ v: 1 }, n: 5 }
  fmt.println(r.n)
}
`
	if codes := runCheck(t, provided); hasCode(codes, "E0220") {
		t.Errorf("a provided class field is fine, got %v", codes)
	}

	// Wrapping it in Option<Box> makes it safely defaultable (→ None).
	opt := `import fmt
class Box { var v: int }
type Rec = { b: Option<Box>, n: int }
func main() {
  let r = Rec{ n: 5 }
  fmt.println(r.n)
}
`
	if codes := runCheck(t, opt); hasCode(codes, "E0220") {
		t.Errorf("Option<Box> zero-defaults to None, got %v", codes)
	}

	// An empty class literal `Box{}` omitting a class field is also caught.
	empty := `import fmt
class Inner { var v: int }
class Outer { var inner: Inner }
func main() {
  let o = Outer{}
  fmt.println("made")
}
`
	if codes := runCheck(t, empty); !hasCode(codes, "E0220") {
		t.Errorf("want E0220 for empty literal omitting a class field, got %v", codes)
	}
}
