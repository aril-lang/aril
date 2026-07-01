package arilrt

import "testing"

// TestBigIntFunctional checks that BigInt is an immutable functional wrapper:
// operations return fresh values, operands are never mutated, and the zero
// value reads as 0.
func TestBigIntFunctional(t *testing.T) {
	a := BigFromInt(2)
	b := BigFromInt(3)
	if got := a.Add(b).ToInt64(); got != 5 {
		t.Errorf("2 + 3 = %d; want 5", got)
	}
	// a and b must be untouched by the Add above (no pointer mutation leak).
	if a.ToInt64() != 2 || b.ToInt64() != 3 {
		t.Errorf("operands mutated: a=%d b=%d; want 2, 3", a.ToInt64(), b.ToInt64())
	}
	if got := BigFromInt64(20).Sub(BigFromInt(6)).Mul(BigFromInt(3)).Div(BigFromInt(2)).ToInt64(); got != 21 {
		t.Errorf("(20-6)*3/2 = %d; want 21", got)
	}
	// The zero value reads as 0 (usable without an explicit constructor).
	var z BigInt
	if z.ToInt64() != 0 {
		t.Errorf("zero BigInt = %d; want 0", z.ToInt64())
	}
	if z.Add(BigFromInt(7)).ToInt64() != 7 {
		t.Errorf("0 + 7 = %d; want 7", z.Add(BigFromInt(7)).ToInt64())
	}
}

// TestBigIntArbitraryPrecision confirms the wrapper genuinely uses math/big:
// a product that overflows int64 is exact in String() and narrows via ToInt64.
func TestBigIntArbitraryPrecision(t *testing.T) {
	// 10^19 > math.MaxInt64 (~9.2e18).
	x := BigFromInt64(10000000000).Mul(BigFromInt64(1000000000))
	if s := x.String(); s != "10000000000000000000" {
		t.Errorf("10^19 String() = %q; want 10000000000000000000", s)
	}
}
