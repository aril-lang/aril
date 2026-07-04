package sema

import "testing"

// E0217 — an unknown member on a *bound stdlib namespace* (`strings.foo`) is
// reported in Aril coordinates rather than leaking a raw `go build`
// "undefined: pkg.foo" (D10). Gated on the SymBuiltinModule symbol +
// binding.IsMember (sound-over-complete): a bound member — mechanical,
// overlay, handle ctor, or a codegen/sema idiom intercept — stays silent.

func TestUnboundStdlibMemberFiresE0217(t *testing.T) {
	// Value-access form and call form, across namespaces with different
	// binding sources (strings: registry; sync: handle namespace; math: registry).
	cases := []struct{ name, src string }{
		{"strings-call", `import strings
func use(): string { return strings.nonexistentMember("x") }`},
		{"math-value", `import math
func use(): float { return math.notaconstant }`},
		{"sync-call", `import sync
func use() { sync.frobnicate() }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if codes := runCheck(t, c.src); !contains(codes, "E0217") {
				t.Errorf("expected E0217 (unbound stdlib member), got %v", codes)
			}
		})
	}
}

// A bound member must NOT fire E0217 — one witness per binding source, so a
// regression in IsMember's fold is caught atomically (the corpus ratchet is
// the completeness oracle, this is the false-positive guard).
func TestBoundStdlibMemberSilentE0217(t *testing.T) {
	cases := []struct{ name, src string }{
		{"registry-rename", `import strings
func use(): []string { return strings.split("a,b", ",") }`},
		{"overlay-effect", `import fmt
func use() { fmt.println("hi") }`},
		{"idiom-intercept", `import sort
func use(xs: []int): []int { return sort.sortedBy(xs, (x) => x) }`},
		{"reflect-owned", `import reflect
func use(x: int): string { return reflect.typeName(x) }`},
		{"handle-ctor", `import regexp
func use(): bool { let re = regexp.mustCompile("x")  return re.matchString("x") }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if codes := runCheck(t, c.src); contains(codes, "E0217") {
				t.Errorf("E0217 must not fire on a bound member, got %v", codes)
			}
		})
	}
}
