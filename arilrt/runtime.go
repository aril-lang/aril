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

// MakeSlice backs the predeclared make-slice builtin.
func MakeSlice[T any](n int) []T { return make([]T, n) }
