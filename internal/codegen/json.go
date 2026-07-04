package codegen

import "github.com/aril-lang/aril/internal/ast"

// json.go — the `encoding/json` binding lowering (binding-surface.md
// §encoding/json) and the JSON representation of Option. The keystone
// (exported struct fields + `json:"…"` tags, codegen.go) is what lets
// Go's reflection-based json reach a Aril record's fields; this file
// wires the actual parse/serialize calls and the Option ⇄ null/value
// round-trip on top of it.

// emitJSONCall lowers a `json.parse<T>(data)` / `json.serialize(v)` /
// `json.serializeIndent(v, prefix, indent)` call. Returns handled=false
// (untouched) when the call is not a json binding, so emitCall continues
// its normal dispatch. The receiver is gated on the sema symbol
// (isBuiltinModule), not the spelling — a user value named `json` keeps
// its ordinary method dispatch (the recurring name-match footgun).
func (g *gen) emitJSONCall(c *ast.Call) (bool, error) {
	f, ok := c.Callee.(*ast.Field)
	if !ok {
		return false, nil
	}
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok || recv.Name != "json" || !g.isBuiltinModule(recv) {
		return false, nil
	}
	switch f.Name {
	case "parse":
		// json.parse<T>(data) → JSONParse[T](data): a generic
		// helper around json.Unmarshal's pointer-mutation API, wrapped
		// into Result<T, error>.
		g.b.WriteString(g.rt("JSONParse"))
		if err := g.emitTypeArgs(c.TypeArgs); err != nil {
			return true, err
		}
		return true, g.emitArgList(c.Args)
	case "serialize":
		// json.serialize(v) → ResultOf(json.Marshal(v)): Marshal's
		// ([]byte, error) is exactly the ResultOf shape.
		return true, g.emitResultOfCall("json.Marshal", c.Args)
	case "serializeIndent":
		// json.serializeIndent(v, prefix, indent) → MarshalIndent.
		return true, g.emitResultOfCall("json.MarshalIndent", c.Args)
	}
	return false, nil
}

// emitErrorsAsCall lowers `errors.as<T>(e)` → `ErrorsAs[T](e)` (the pointer-out
// helper, into Option<T>). handled=false when the call is not this binding. The
// receiver is gated on the sema symbol (isBuiltinModule), not the spelling.
func (g *gen) emitErrorsAsCall(c *ast.Call) (bool, error) {
	f, ok := c.Callee.(*ast.Field)
	if !ok {
		return false, nil
	}
	recv, ok := f.Receiver.(*ast.Ident)
	if !ok || recv.Name != "errors" || f.Name != "as" || !g.isBuiltinModule(recv) {
		return false, nil
	}
	g.b.WriteString(g.rt("ErrorsAs"))
	if err := g.emitTypeArgs(c.TypeArgs); err != nil {
		return true, err
	}
	return true, g.emitArgList(c.Args)
}

// emitResultOfCall emits `ResultOf(<goFn>(<args>))` — the wrap for a
// Go `(T, error)`-returning binding into Result<T, error>.
func (g *gen) emitResultOfCall(goFn string, args []ast.Expr) error {
	g.b.WriteString(g.rt("ResultOf") + "(")
	g.b.WriteString(goFn)
	if err := g.emitArgList(args); err != nil {
		return err
	}
	g.b.WriteByte(')')
	return nil
}

// goImportPath maps a Aril import name to its Go import path. Almost all
// stdlib bindings share the name (fmt → "fmt"); `json` is the first
// where they differ — the Aril module `json` binds Go's "encoding/json"
// (whose package selector is still `json`, so call sites are unaffected).
func goImportPath(arilName string) string {
	if arilName == "json" {
		return "encoding/json"
	}
	return arilName
}

// writePredeclaredJSONParse emits the JSONParse helper backing
// json.parse<T> (binding-surface.md §encoding/json). json.Unmarshal
// populates through a pointer, so the generic helper allocates a T,
// unmarshals into it, and folds the (value, error) into Result.
// Conditional on usage (usesJSONParse, forced alongside usesResult).
func (g *gen) writePredeclaredJSONParse() {
	if !g.usesJSONParse {
		return
	}
	g.b.WriteString(`func JSONParse[T any](data []byte) Result[T, error] {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return Result[T, error]{Tag: 1, E: err}
	}
	return Result[T, error]{Tag: 0, V: v}
}
`)
}

// writeOptionJSONMethods emits MarshalJSON/UnmarshalJSON on Option[T] so
// an Option-typed record field round-trips with *bare* JSON — None ⇄
// `null`, Some(v) ⇄ the JSON of v — rather than exposing the internal
// Tag/V struct shape (lowering-go.md §Container types). Emitted only
// when both Option and a json binding are used: a program with Option
// but no json needs neither the methods nor the encoding/json import.
func (g *gen) writeOptionJSONMethods() {
	if !(g.usesOption && g.usesJSON) {
		return
	}
	g.b.WriteString(`func (o Option[T]) MarshalJSON() ([]byte, error) {
	if o.Tag == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(o.V)
}
func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		var zero T
		o.Tag, o.V = 0, zero
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Tag, o.V = 1, v
	return nil
}
`)
}
