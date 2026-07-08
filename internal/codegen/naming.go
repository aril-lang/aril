package codegen

import (
	"strconv"
	"strings"

	"github.com/aril-lang/aril/internal/ast"
)

// This file holds the small naming / identifier helpers shared
// across codegen: payload-field and Go-identifier spelling, the
// Go-reserved-word table, JSON-tag and indentation writers, and
// the //line directive emitter. Split out of the codegen.go
// god-file; behaviour-preserving.

// methodRecvName is the Go variable emitted for a class method's implicit
// receiver (`func (_arilSelf *T) …`) and every implicit-receiver reference in
// the body (a bare field name / `this`). It carries the `_aril`-reserved
// prefix (E0107 rejects it in user source) so it can never collide with a
// user binding — e.g. a `match` arm binding `Some(t)` used to shadow the old
// hard-coded `t` receiver and mis-dispatch onto the bound value
// (lowering-go.md §Implicit receiver). The construction-time checker uses its
// own `_arilNew` temp (contract.go); both are the same reserved family.
const methodRecvName = "_arilSelf"

// payloadFieldName builds the Go struct field name for a payload
// field of a variant, per the lowering-go.md tagged-struct shape:
// `<VariantName><FieldName>` (both capitalised). E.g. variant
// `Just` with field `value` → `JustValue`.
func payloadFieldName(variantName, fieldName string) string {
	return capFirst(variantName) + capFirst(fieldName)
}

// isSelfRefField reports whether a payload field directly names the
// enclosing sum type `sumName` (`Tree` or `Tree<…>`). Such a field
// would make the lowered Go struct infinitely sized, so it is
// pointer-ized — `*Tree`, with `&` at construction and `*` at the
// match-binding deref (lowering-go.md §Recursive sum types).
// Indirection through a slice / map / channel is already a pointer in
// Go and needs no rewrite; only the direct-named case is detected
// (by-value recursion nested inside another type — `Option<Tree>` —
// is a v1 limitation). A nil DeclType (predeclared Option/Result
// payload registration) fails the assertion and returns false.
func isSelfRefField(f *ast.FieldDecl, sumName string) bool {
	nt, ok := f.DeclType.(*ast.NamedType)
	return ok && len(nt.QName) == 1 && nt.QName[0] == sumName
}

func capFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func lastSeg(q []string) string {
	if len(q) == 0 {
		return ""
	}
	return q[len(q)-1]
}

// goIdent maps a Aril identifier to its Go form. PR-C handles
// the common cases (no transform); future PRs add Go-reserved-
// word escaping ("type" → "aril_type") and the `$aril_NN` →
// `_aril_NN` rewrite for codegen-synthesised names.
func goIdent(name string) string {
	if isGoReserved(name) {
		return "aril_" + name
	}
	return name
}

// exportFieldName spells a Aril record/class field as an EXPORTED Go
// field name. encoding/json reflects from outside package main, so an
// unexported Go field is invisible to it; exporting is what makes JSON
// round-trip work (lowering-go.md §Record lowering). The Aril name is
// preserved verbatim in the field's `json:"…"` tag (field-name ==
// JSON-key, binding-surface.md §encoding/json), so the capitalised Go
// spelling is invisible at the Aril-source and wire levels. Exported
// names always start uppercase, so they can never be Go-reserved — no
// goIdent escaping needed.
//
// Identifiers are ASCII `[A-Za-z_][A-Za-z0-9_]*` (the lexer rejects the
// rest), so a single-byte uppercase suffices. A leading underscore can't
// be exported by capitalising, so it gets an `X` prefix. (Collision risk
// — two fields differing only in first-letter case, or `_x` vs `X_x` —
// is a documented limitation; Go rejects the duplicate field loudly, so
// it is never a silent miscompile. See lowering-go.md §Record / struct
// field lowering.)
func exportFieldName(name string) string {
	if name == "" {
		return name
	}
	c := name[0]
	if c >= 'a' && c <= 'z' {
		return string(c-'a'+'A') + name[1:]
	}
	if c >= 'A' && c <= 'Z' {
		return name
	}
	return "X" + name
}

// writeJSONTag emits the ` `+"`json:\"<arilName>\"`"+` ` struct tag that
// pins the JSON key to the Aril field name regardless of the exported Go
// spelling (binding-surface.md §encoding/json: field-name == JSON-key).
func (g *gen) writeJSONTag(arilName string) {
	g.b.WriteString(" `json:\"")
	g.b.WriteString(arilName)
	g.b.WriteString("\"`")
}

var goReserved = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true,
	"continue": true, "default": true, "defer": true, "else": true,
	"fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true,
	"map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true,
	"var": true,
}

func isGoReserved(name string) bool { return goReserved[name] }

// ---- helpers ----

func (g *gen) writeIndent() {
	for i := 0; i < g.indent; i++ {
		g.b.WriteByte('\t')
	}
}

// line emits a //line directive at the start of a statement
// boundary, mapping subsequent Go lines back to the Aril source
// line. Suppressed when no file path was supplied.
func (g *gen) line(srcLine int) {
	if g.file == "" || srcLine == g.emittedLine {
		return
	}
	g.writeIndent()
	g.b.WriteString("//line ")
	g.b.WriteString(g.file)
	g.b.WriteByte(':')
	g.b.WriteString(strconv.Itoa(srcLine))
	g.b.WriteString(":1\n")
	g.emittedLine = srcLine
}
