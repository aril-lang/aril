package codegen

import (
	"strconv"

	"github.com/aril-lang/aril/internal/ast"
)

// writePredeclaredReflect emits Aril's reflection layer (lang-spec/
// builtins.md §reflect, D18): the Dynamic wrapper, TypeDescriptor, Kind,
// the descriptor registry, and the reflect.* surface. The fixed part is
// the authoritative arilrt/reflect.go — emitted inline only in
// single-file mode; in vendored mode it comes from the imported arilrt
// package. The per-program part (per-class descriptors + accessors + the
// init registration) is always emitted into main, qualifying the
// arilrt-provided types/vars via rt() under vendored mode.
func (g *gen) writePredeclaredReflect() {
	if !g.usesReflect {
		return
	}
	if !g.vendored() {
		g.writeReflectPreludeFixed()
	}
	// Per-user-type descriptors collected during emit, with init
	// block registering them into the runtime map.
	if len(g.descriptors) == 0 {
		return
	}
	td := g.rt("TypeDescriptor")
	dyn := g.rt("Dynamic")
	for _, d := range g.descriptors {
		g.b.WriteString("var arilDesc_")
		g.b.WriteString(d.arilName)
		g.b.WriteString(" = &" + td + "{Name: ")
		g.b.WriteString(strconv.Quote(d.arilName))
		g.b.WriteString(", Kind: ")
		g.b.WriteString(g.rt(d.kind))
		g.b.WriteString("}\n")
	}
	// Per-class field accessor functions — emitted after the
	// classes themselves are emitted into the package, so the
	// accessor body can refer to the class struct's field by
	// its exported Go-side name (exportFieldName); the `case`
	// label stays the Aril name for runtime lookup. The accessor takes any so
	// the dispatcher in FieldValue can call it through the
	// map without per-class typed indirection.
	for _, d := range g.descriptors {
		if d.kind != "KindClass" {
			continue
		}
		g.b.WriteString("func arilFieldOf_")
		g.b.WriteString(d.arilName)
		g.b.WriteString("(v any, name string) (" + dyn + ", bool) {\n")
		g.b.WriteString("\tc, _ := v.(*")
		g.b.WriteString(d.arilName)
		g.b.WriteString(")\n")
		g.b.WriteString("\tif c == nil {\n\t\treturn " + dyn + "{}, false\n\t}\n")
		g.b.WriteString("\tswitch name {\n")
		for _, fi := range d.fields {
			g.b.WriteString("\tcase ")
			g.b.WriteString(strconv.Quote(fi.arilName))
			g.b.WriteString(":\n")
			if fi.descRef != "" {
				g.b.WriteString("\t\treturn " + dyn + "{Payload: c.")
				g.b.WriteString(exportFieldName(fi.arilName))
				g.b.WriteString(", Desc: ")
				g.b.WriteString(fi.descRef)
				g.b.WriteString("}, true\n")
			} else {
				// Unknown-static-type — fall back to BoxAny so the
				// descriptor is at least synthesised at runtime.
				g.b.WriteString("\t\treturn " + g.rt("BoxAny") + "(c.")
				g.b.WriteString(exportFieldName(fi.arilName))
				g.b.WriteString("), true\n")
			}
		}
		g.b.WriteString("\t}\n\treturn " + dyn + "{}, false\n}\n")
	}
	g.b.WriteString("func init() {\n")
	reg := g.rt("DescRegistry")
	for _, d := range g.descriptors {
		g.b.WriteString("\t" + reg + "[")
		g.b.WriteString(strconv.Quote(d.goType))
		g.b.WriteString("] = arilDesc_")
		g.b.WriteString(d.arilName)
		g.b.WriteString("\n")
	}
	// Populate field metadata + accessor registry for each
	// class descriptor. Fields list is emitted as a literal
	// inside init() so it can reference the per-field type
	// descriptors that were declared above.
	fi := g.rt("FieldInfo")
	acc := g.rt("FieldAccessors")
	for _, d := range g.descriptors {
		if d.kind != "KindClass" {
			continue
		}
		if len(d.fields) > 0 {
			g.b.WriteString("\tarilDesc_")
			g.b.WriteString(d.arilName)
			g.b.WriteString(".FieldList = []" + fi + "{\n")
			for _, fd := range d.fields {
				g.b.WriteString("\t\t{Name: ")
				g.b.WriteString(strconv.Quote(fd.arilName))
				g.b.WriteString(", Desc: ")
				if fd.descRef != "" {
					g.b.WriteString(fd.descRef)
				} else {
					g.b.WriteString("nil")
				}
				g.b.WriteString("},\n")
			}
			g.b.WriteString("\t}\n")
		}
		g.b.WriteString("\t" + acc + "[")
		g.b.WriteString(strconv.Quote(d.arilName))
		g.b.WriteString("] = arilFieldOf_")
		g.b.WriteString(d.arilName)
		g.b.WriteString("\n")
	}
	g.b.WriteString("}\n")
}

// writeReflectPreludeFixed emits the fixed reflection runtime inline —
// byte-for-byte the body of arilrt/reflect.go (exported names), used in
// single-file mode. Vendored mode imports the same definitions from
// arilrt instead.
func (g *gen) writeReflectPreludeFixed() {
	g.b.WriteString(`type Dynamic struct {
	Payload any
	Desc    *TypeDescriptor
}

type TypeDescriptor struct {
	Name      string
	Kind      Kind
	FieldList []FieldInfo
}

type FieldInfo struct {
	Name string
	Desc *TypeDescriptor
}

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

var DescRegistry = map[string]*TypeDescriptor{}

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

func Box[T any](v T) Dynamic {
	return Dynamic{Payload: v, Desc: DescForKey(reflect.TypeOf(v).String())}
}

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

type FieldAccessor = func(v any, name string) (Dynamic, bool)

var FieldAccessors = map[string]FieldAccessor{}

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

func BoxAny(v any) Dynamic {
	if v == nil {
		return Dynamic{Payload: nil, Desc: DescForKey("<nil>")}
	}
	return Dynamic{Payload: v, Desc: DescForKey(reflect.TypeOf(v).String())}
}

func Show(d Dynamic) string {
	return showWalk(d, map[any]bool{})
}

func showWalk(d Dynamic, seen map[any]bool) string {
	if d.Desc == nil {
		return "<nil>"
	}
	switch d.Desc.Kind {
	case KindClass:
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
`)
}

// descRefForType resolves a Aril TypeExpr to the Go-side reference of its
// type descriptor — a primitive descriptor from arilrt (rt-qualified) or
// a per-class descriptor var emitted into main (arilDesc_<class>).
// Returns "" when the type has no emitted descriptor (slices, generics,
// function types, ...); callers handle the empty case with a placeholder.
//
// classNames lists the names of non-generic user classes that will have
// descriptors emitted in this compilation.
func (g *gen) descRefForType(t ast.TypeExpr, classNames map[string]bool) string {
	switch v := t.(type) {
	case *ast.PrimitiveType:
		// rune and byte alias to int32 / uint8 at the Go-runtime level
		// (see the primitive descriptors above); the descriptors
		// collapse accordingly.
		switch v.Name {
		case "rune":
			return g.rt("DescInt32")
		case "byte":
			return g.rt("DescByte")
		default:
			if name, ok := primDescName[v.Name]; ok {
				return g.rt(name)
			}
			return ""
		}
	case *ast.NamedType:
		if len(v.QName) == 1 && classNames[v.QName[0]] {
			return "arilDesc_" + v.QName[0]
		}
	}
	return ""
}

// primDescName maps a primitive type name to its arilrt descriptor var.
var primDescName = map[string]string{
	"int":     "DescInt",
	"int64":   "DescInt64",
	"int32":   "DescInt32",
	"string":  "DescString",
	"bool":    "DescBool",
	"float64": "DescFloat64",
	"byte":    "DescByte",
}
