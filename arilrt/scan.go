package arilrt

import "fmt"

// scan.go — the stdin scan helpers backing fmt.scan<T> / fmt.scan2 /
// fmt.scan3 (binding-surface.md §fmt). Each wraps one fmt.Scan of N
// pointers into a Result; the multi-value variants use the anonymous
// struct { _0 …; _1 … } tuple shape codegen spells everywhere.

// Scan reads one whitespace-separated value into Result<T, error>.
func Scan[T any]() Result[T, error] {
	var v T
	if _, err := fmt.Scan(&v); err != nil {
		return ResultErr[T, error](err)
	}
	return ResultOk[T, error](v)
}

// Scan2 reads two values into Result<(A, B), error>.
func Scan2[A any, B any]() Result[struct {
	_0 A
	_1 B
}, error] {
	var a A
	var b B
	if _, err := fmt.Scan(&a, &b); err != nil {
		return ResultErr[struct {
			_0 A
			_1 B
		}, error](err)
	}
	return ResultOk[struct {
		_0 A
		_1 B
	}, error](struct {
		_0 A
		_1 B
	}{a, b})
}

// Scan3 reads three values into Result<(A, B, C), error>.
func Scan3[A any, B any, C any]() Result[struct {
	_0 A
	_1 B
	_2 C
}, error] {
	var a A
	var b B
	var c C
	if _, err := fmt.Scan(&a, &b, &c); err != nil {
		return ResultErr[struct {
			_0 A
			_1 B
			_2 C
		}, error](err)
	}
	return ResultOk[struct {
		_0 A
		_1 B
		_2 C
	}, error](struct {
		_0 A
		_1 B
		_2 C
	}{a, b, c})
}
