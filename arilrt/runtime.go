package arilrt

// runtime.go — Option / Result and their boundary-lift helpers.
//
// Struct shapes are the contract (lang-spec/lowering-go.md §Container
// types — runtime representation): Option[T] is {Tag, V}; Result[T, E]
// is {Tag, V, E}. Codegen references these by their exported names,
// prefixed with the package selector in vendored mode (arilrt.Option)
// and bare in single-file inline mode.

// Option is the predeclared optional sum: Tag 1 = Some(V), Tag 0 = None.
type Option[T any] struct {
	Tag uint8
	V   T
}

// OptionSome builds Some(value).
func OptionSome[T any](value T) Option[T] { return Option[T]{Tag: 1, V: value} }

// OptionNone builds None.
func OptionNone[T any]() Option[T] { return Option[T]{Tag: 0} }

// IsSome reports whether the option holds a value (builtins.md §Option methods).
func (o Option[T]) IsSome() bool { return o.Tag == 1 }

// IsNone reports whether the option is empty (builtins.md §Option methods).
func (o Option[T]) IsNone() bool { return o.Tag == 0 }

// UnwrapOr returns the held value, or fallback when None (builtins.md §Option methods).
func (o Option[T]) UnwrapOr(fallback T) T {
	if o.Tag == 1 {
		return o.V
	}
	return fallback
}

// Result is the predeclared fallible sum: Tag 0 = Ok(V), Tag 1 = Err(E).
type Result[T any, E any] struct {
	Tag uint8
	V   T
	E   E
}

// ResultOk builds Ok(value).
func ResultOk[T any, E any](value T) Result[T, E] { return Result[T, E]{Tag: 0, V: value} }

// ResultErr builds Err(err).
func ResultErr[T any, E any](err E) Result[T, E] { return Result[T, E]{Tag: 1, E: err} }

// IsOk reports whether the result is Ok (builtins.md §Result methods).
func (r Result[T, E]) IsOk() bool { return r.Tag == 0 }

// IsErr reports whether the result is Err (builtins.md §Result methods).
func (r Result[T, E]) IsErr() bool { return r.Tag == 1 }

// UnwrapOr returns the Ok value, or fallback when Err (builtins.md §Result methods).
func (r Result[T, E]) UnwrapOr(fallback T) T {
	if r.Tag == 0 {
		return r.V
	}
	return fallback
}

// MapErr transforms the Err payload E→E2 (leaving an Ok untouched), so the
// value keeps flowing as a Result. It is the error-conversion combinator that
// lets `try` cross an error-type boundary (builtins.md §Result methods). A free
// function, not a method: a Go method cannot introduce the fresh E2 type
// parameter, though the Aril surface spells it `r.mapErr(f)`.
func MapErr[T any, E any, E2 any](r Result[T, E], f func(E) E2) Result[T, E2] {
	if r.Tag == 1 {
		return Result[T, E2]{Tag: 1, E: f(r.E)}
	}
	return Result[T, E2]{Tag: 0, V: r.V}
}

// ResultOf folds a Go (T, error) pair into Result<T, error>: a non-nil
// error becomes Err, a successful value becomes Ok. Backs the
// (T, error) → Result stdlib bindings (strconv.atoi, os.readFile, …).
func ResultOf[T any](v T, err error) Result[T, error] {
	if err != nil {
		return ResultErr[T, error](err)
	}
	return ResultOk[T, error](v)
}

// ResultUnit folds a bare Go error into Result<unit, error> for extern
// referents that return only an error (os.Chdir, os.WriteFile, …). unit
// lowers to the zero-byte struct{}.
func ResultUnit(err error) Result[struct{}, error] {
	if err != nil {
		return ResultErr[struct{}, error](err)
	}
	return ResultOk[struct{}, error](struct{}{})
}

// OptionOf folds a Go comma-ok (T, ok) pair into Option<T>: ok=true
// becomes Some(v), ok=false becomes None. The Option mirror of ResultOf;
// backs the (T, bool) → Option stdlib bindings (os.lookupEnv, …).
func OptionOf[T any](v T, ok bool) Option[T] {
	if ok {
		return OptionSome[T](v)
	}
	return OptionNone[T]()
}

// SlicesReverse backs slices.reverse(xs): a NEW slice with the elements in
// reverse order (Aril preserves the input's immutability; Go's slices.Reverse
// mutates in place). binding-surface.md §slices.
func SlicesReverse[T any](xs []T) []T {
	out := make([]T, len(xs))
	for i, v := range xs {
		out[len(xs)-1-i] = v
	}
	return out
}

// MakeSlice backs the predeclared make-slice builtin.
func MakeSlice[T any](n int) []T { return make([]T, n) }
