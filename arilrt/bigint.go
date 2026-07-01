package arilrt

import "math/big"

// bigint.go — BigInt backs the `big` value-handle binding
// (binding-surface.md §math/big): an arbitrary-precision integer with a
// *functional* surface. Go's math/big API mutates the receiver
// (`z.Add(x, y)` writes z), which the Aril surface hides behind
// value-returning methods — each operation allocates a fresh result and
// never mutates its operands, so a BigInt behaves like an immutable value
// (the zero BigInt{} reads as 0).

// BigInt is an immutable arbitrary-precision integer.
type BigInt struct{ v *big.Int }

// BigFromInt / BigFromInt64 build a BigInt from a machine integer — the
// `big.fromInt` / `big.fromInt64` constructors.
func BigFromInt(n int) BigInt     { return BigInt{big.NewInt(int64(n))} }
func BigFromInt64(n int64) BigInt { return BigInt{big.NewInt(n)} }

// int returns the backing value, treating the zero BigInt{} as 0 so a
// value-typed field or a zero value is usable without an explicit constructor.
func (a BigInt) int() *big.Int {
	if a.v == nil {
		return new(big.Int)
	}
	return a.v
}

// Add / Sub / Mul return a fresh BigInt; the operands are untouched.
func (a BigInt) Add(b BigInt) BigInt { return BigInt{new(big.Int).Add(a.int(), b.int())} }
func (a BigInt) Sub(b BigInt) BigInt { return BigInt{new(big.Int).Sub(a.int(), b.int())} }
func (a BigInt) Mul(b BigInt) BigInt { return BigInt{new(big.Int).Mul(a.int(), b.int())} }

// Div is truncated integer division (Go's Quo — toward zero); a Div by zero
// panics like the underlying math/big operation.
func (a BigInt) Div(b BigInt) BigInt { return BigInt{new(big.Int).Quo(a.int(), b.int())} }

// ToInt64 narrows to int64 (the low 64 bits when the value overflows, per
// math/big's Int64) — used to land a result back in a machine integer.
func (a BigInt) ToInt64() int64 { return a.int().Int64() }

// String renders the decimal representation (`d.string()`).
func (a BigInt) String() string { return a.int().String() }
