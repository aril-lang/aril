package sema

import "testing"

// String-interpolation holes are ordinary expressions and flow through the
// full checker (resolve + infer): an undefined name or unknown member in a
// hole surfaces as an Aril-coordinate diagnostic (E0103 / E0214), never a
// raw go/types leak (D10); and the interp node itself types as `string` so
// it is usable in a type-inferred value position.

func TestInterpHoleUndefinedNameFiresE0103(t *testing.T) {
	src := `import fmt
func f() { fmt.println("v ${missing}") }`
	if codes := runCheck(t, src); !contains(codes, "E0103") {
		t.Errorf("expected E0103 (unknown name in hole), got %v", codes)
	}
}

func TestInterpHoleUnknownMemberFiresE0214(t *testing.T) {
	src := `import fmt
class Box { let v: int  get(): int { return v } }
func f(b: Box) { fmt.println("v ${b.nope()}") }`
	if codes := runCheck(t, src); !contains(codes, "E0214") {
		t.Errorf("expected E0214 (unknown member in hole), got %v", codes)
	}
}

func TestInterpTypesString(t *testing.T) {
	// A hole-bearing string is `string`, so a value-position interp (here
	// a match arm) unifies and does not fall to Unknown.
	src := `func f(n: int): string {
  return match n { 1 => "one ${n}", _ => "many ${n}" }
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean (interp types string), got %v", codes)
	}
}

func TestInterpCleanHoles(t *testing.T) {
	src := `import fmt
func f(name: string, count: int) {
  fmt.println("hi ${name}, ${count + 1} left")
}`
	if codes := runCheck(t, src); len(codes) != 0 {
		t.Errorf("expected clean, got %v", codes)
	}
}
