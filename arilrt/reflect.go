package arilrt

import (
	"reflect"
	"strconv"
)

// reflect.go — Aril's reflection layer (lang-spec/builtins.md §reflect,
// D18): the Dynamic box, type descriptors, the descriptor registry, and
// the reflect.* surface (box / unbox / typeOf / typeName / kind / fields
// / fieldValue / show). Per-class descriptors and field accessors are
// emitted into the user program and registered here at init time, so the
// registry and the FieldAccessors map are exported for cross-package
// population in vendored mode.

// Dynamic is the boxed-value wrapper handed to reflect.* functions.
type Dynamic struct {
	Payload any
	Desc    *TypeDescriptor
}

// TypeDescriptor carries a type's Aril-side metadata. FieldList is
// exported so the user program's init block can populate class fields.
type TypeDescriptor struct {
	Name      string
	Kind      Kind
	FieldList []FieldInfo
}

// FieldInfo describes one class field.
type FieldInfo struct {
	Name string
	Desc *TypeDescriptor
}

// Kind tags the broad category of a type.
type Kind struct {
	Tag uint8
}

var (
	KindPrimitive = Kind{Tag: 0}
	KindClass     = Kind{Tag: 1}
	KindSum       = Kind{Tag: 2}
	KindSlice     = Kind{Tag: 3}
	KindFunction  = Kind{Tag: 4}
	KindUnit      = Kind{Tag: 5}
)

// DescRegistry maps a Go-runtime type-name key to its descriptor. The
// user program's init block registers per-class descriptors here.
var DescRegistry = map[string]*TypeDescriptor{}

// Primitive descriptors — registered eagerly so reflect.box on any
// primitive value finds a descriptor. byte aliases uint8 and rune
// aliases int32 at the Go-runtime level, so those collapse to one
// descriptor each.
var (
	DescInt     = &TypeDescriptor{Name: "int", Kind: KindPrimitive}
	DescInt64   = &TypeDescriptor{Name: "int64", Kind: KindPrimitive}
	DescInt32   = &TypeDescriptor{Name: "int32", Kind: KindPrimitive}
	DescString  = &TypeDescriptor{Name: "string", Kind: KindPrimitive}
	DescBool    = &TypeDescriptor{Name: "bool", Kind: KindPrimitive}
	DescFloat64 = &TypeDescriptor{Name: "float64", Kind: KindPrimitive}
	DescByte    = &TypeDescriptor{Name: "byte", Kind: KindPrimitive}
)

func init() {
	DescRegistry["int"] = DescInt
	DescRegistry["int64"] = DescInt64
	DescRegistry["int32"] = DescInt32
	DescRegistry["string"] = DescString
	DescRegistry["bool"] = DescBool
	DescRegistry["float64"] = DescFloat64
	DescRegistry["uint8"] = DescByte
}

// Box wraps a statically-typed value as Dynamic.
func Box[T any](v T) Dynamic {
	return Dynamic{Payload: v, Desc: DescForKey(reflect.TypeOf(v).String())}
}

// DescForKey looks up (and caches) a descriptor for the Go-runtime
// type-name key, preserving descriptor identity (CT1): two Box calls on
// the same Go-runtime type share a Desc pointer.
func DescForKey(key string) *TypeDescriptor {
	if d, ok := DescRegistry[key]; ok {
		return d
	}
	d := &TypeDescriptor{Name: key, Kind: KindPrimitive}
	DescRegistry[key] = d
	return d
}

func TypeOf(d Dynamic) *TypeDescriptor     { return d.Desc }
func TypeName(t *TypeDescriptor) string    { return t.Name }
func KindOf(t *TypeDescriptor) Kind        { return t.Kind }
func Fields(t *TypeDescriptor) []FieldInfo { return t.FieldList }

// Unbox recovers a T from a Dynamic, or Err when the payload is not T.
func Unbox[T any](d Dynamic) Result[T, error] {
	v, ok := d.Payload.(T)
	if !ok {
		var zero T
		return Result[T, error]{Tag: 1, E: unboxError(d.Desc.Name), V: zero}
	}
	return Result[T, error]{Tag: 0, V: v}
}

type unboxErr struct{ typeName string }

func (e unboxErr) Error() string {
	return "reflect.unbox: payload is not the requested type (have " + e.typeName + ")"
}

func unboxError(typeName string) error { return unboxErr{typeName: typeName} }

// FieldAccessor reads a named field off a class value, boxed.
type FieldAccessor = func(v any, name string) (Dynamic, bool)

// FieldAccessors holds one accessor per non-generic class in the
// program, registered by the user program's init block.
var FieldAccessors = map[string]FieldAccessor{}

// FieldValue reads field `name` off a class-kinded Dynamic.
func FieldValue(d Dynamic, name string) Result[Dynamic, error] {
	if d.Desc == nil {
		var zero Dynamic
		return Result[Dynamic, error]{Tag: 1, E: fieldErr("Dynamic has no descriptor"), V: zero}
	}
	fn, ok := FieldAccessors[d.Desc.Name]
	if !ok {
		var zero Dynamic
		return Result[Dynamic, error]{Tag: 1, E: fieldErr("type " + d.Desc.Name + " has no field accessor"), V: zero}
	}
	v, ok := fn(d.Payload, name)
	if !ok {
		var zero Dynamic
		return Result[Dynamic, error]{Tag: 1, E: fieldErr("type " + d.Desc.Name + " has no field " + name), V: zero}
	}
	return Result[Dynamic, error]{Tag: 0, V: v}
}

type fieldErrT struct{ msg string }

func (e fieldErrT) Error() string { return e.msg }

func fieldErr(msg string) error { return fieldErrT{msg: msg} }

// BoxAny boxes a value whose type is known only at runtime (used by
// per-class field accessors). Routes through DescForKey so descriptor
// identity follows CT1.
func BoxAny(v any) Dynamic {
	if v == nil {
		return Dynamic{Payload: nil, Desc: DescForKey("<nil>")}
	}
	return Dynamic{Payload: v, Desc: DescForKey(reflect.TypeOf(v).String())}
}

// Show renders a Dynamic value as a human-readable string — the runtime
// building block for the REPL auto-printer and `:inspect` (RFC-0003).
// Panic-free per D18 CT2: class graphs with cycles render the back-edge
// as "<cycle …>" instead of blowing the stack.
func Show(d Dynamic) string {
	return showWalk(d, map[any]bool{})
}

func showWalk(d Dynamic, seen map[any]bool) string {
	if d.Desc == nil {
		return "<nil>"
	}
	switch d.Desc.Kind {
	case KindClass:
		// Classes are pointer-to-struct on the Go side; use the payload
		// as a cycle key. Non-class kinds skip the check because their
		// payloads aren't reliably comparable.
		if d.Payload != nil {
			if seen[d.Payload] {
				return "<cycle " + d.Desc.Name + ">"
			}
			seen[d.Payload] = true
		}
		out := d.Desc.Name + "{"
		for i, fi := range d.Desc.FieldList {
			if i > 0 {
				out += ", "
			}
			fv := FieldValue(d, fi.Name)
			if fv.Tag != 0 {
				out += fi.Name + ": <unreadable>"
				continue
			}
			out += fi.Name + ": " + showWalk(fv.V, seen)
		}
		return out + "}"
	case KindPrimitive:
		return showPrimitive(d)
	default:
		return "<" + d.Desc.Name + ">"
	}
}

func showPrimitive(d Dynamic) string {
	switch v := d.Payload.(type) {
	case nil:
		return "<nil>"
	case string:
		return strconv.Quote(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return "<" + d.Desc.Name + ">"
	}
}
