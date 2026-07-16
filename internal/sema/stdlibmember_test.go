package sema

import (
	"strings"
	"testing"
)

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

// The AUDIT-2 F-sort cluster: an unbound `sort.*` member (a Go `sort.Slice`
// idiom leaking) fires E0217 with a *tailored* hint naming `sort.sorted`, so
// the diagnostic teaches the trap (D38/D41 tailored-hint precedent). The real
// bindings `sort.sorted`/`sort.sortedBy` stay silent (covered above too).
func TestSortMemberMissCarriesSortedHint(t *testing.T) {
	// The forms AUDIT-2 models reached for (sort.Slice/slice/sort/by).
	cases := []string{
		`import sort
func use(xs: []int) { sort.Slice(xs, (i, j) => xs[i] > xs[j]) }`,
		`import sort
func use(xs: []int) { sort.slice(xs, (i, j) => xs[i] > xs[j]) }`,
		`import sort
func use(xs: []int) { sort.sort(xs) }`,
		`import sort
func use(xs: []int) { sort.by(xs, (x) => x) }`,
	}
	const want = "`sort.sorted(xs, less)`"
	for _, src := range cases {
		t.Run("", func(t *testing.T) {
			msgs := runCheckMsgs(t, src)
			found := false
			for _, m := range msgs {
				if strings.Contains(m, want) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected E0217 message to carry the sort.sorted hint %q, got %v", want, msgs)
			}
		})
	}
}
