package arilrt

import "errors"

// errors.go — the errors.as<T> binding (binding-surface.md §errors).

// ErrorsAs lifts Go's errors.As pointer-out protocol into Option<T>: Some(t)
// when a matching error is found in err's chain, None otherwise. Backs
// errors.as<T> in vendored mode; the inline mirror is writePredeclaredErrorsAs.
func ErrorsAs[T any](err error) Option[T] {
	var t T
	if errors.As(err, &t) {
		return OptionSome[T](t)
	}
	return OptionNone[T]()
}
