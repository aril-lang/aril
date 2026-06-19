package codegen

import (
	"fmt"
	"go/format"
	"strconv"
	"strings"

	"github.com/aril-lang/aril/internal/ast"
	"github.com/aril-lang/aril/internal/sema"
)

// Emit lowers the given Aril AST to a Go source string. The
// returned text is gofmt-stable (round-trips through gofmt -s).
// file is the source path embedded into //line directives;
// pass "" to suppress them.
func Emit(f *ast.File, file string) (string, error) {
	// Codegen reads variable / receiver types from the sema
	// side-table. When called standalone (tests, tooling) it
	// computes Info here; cmd/aril passes the Info it already
	// produced via EmitWithInfo. Diagnostics are the caller's
	// concern — codegen only needs the type side-table.
	info, _ := sema.Check(f, file)
	return EmitWithInfo(f, file, info)
}

// EmitWithInfo is Emit with a pre-computed sema side-table.
func EmitWithInfo(f *ast.File, file string, info *sema.Info) (string, error) {
	return EmitFilesWithInfo([]*ast.File{f}, []string{file}, info)
}

// EmitFilesWithInfo lowers a whole package — every `.aril` file in a
// directory shares one Go `package main` (RFC-0002 §"Package =
// directory"). The files are merged into one compile unit: the union of
// their imports plus the concatenation of their declarations. Each decl
// remembers its own source path so the //line directives attribute it
// to the right `.aril` file. `paths[i]` is the //line path for files[i]
// ("" suppresses directives for that file).
func EmitFilesWithInfo(files []*ast.File, paths []string, info *sema.Info) (string, error) {
	return EmitFilesWithOptions(files, paths, info, Options{})
}

// Options selects how the emitted program obtains the arilrt runtime
// (Block R, D18).
type Options struct {
	// Vendored emits `import <RuntimeImportPath>` plus qualified
	// `arilrt.X` references, with the runtime supplied by the imported
	// arilrt package (the build harness vendors it into the build
	// module). When false the runtime is emitted inline as a
	// self-contained single file. A reflection-using program always
	// emits inline for now (resolveRuntimeMode).
	Vendored bool
	// RuntimeImportPath is the Go import path of the vendored arilrt
	// package, e.g. "aril-output/arilrt"; required when Vendored.
	RuntimeImportPath string
}

// EmitFilesWithOptions is EmitFilesWithInfo with explicit runtime-mode
// Options.
func EmitFilesWithOptions(files []*ast.File, paths []string, info *sema.Info, opts Options) (string, error) {
	merged := &ast.File{}
	declFile := map[ast.Decl]string{}
	seenImport := map[string]bool{}
	firstFile := ""
	for i, src := range files {
		if i == 0 {
			firstFile = paths[i]
		}
		for _, im := range src.Imports {
			if !seenImport[im.Path] {
				seenImport[im.Path] = true
				merged.Imports = append(merged.Imports, im)
			}
		}
		for _, d := range src.Decls {
			merged.Decls = append(merged.Decls, d)
			declFile[d] = paths[i]
		}
	}
	f := merged
	file := firstFile
	g := &gen{
		file:              file,
		info:              info,
		vendoredRequested: opts.Vendored,
		runtimeImport:     opts.RuntimeImportPath,
		userTypeNames:     map[string]bool{},
		variant:           map[string]variantInfo{},
		class:             map[string]classInfo{},
		fieldTypes:        map[string]map[string]ast.TypeExpr{},
		usedGoPkgs:        map[string]bool{},
		externFunc:        map[string]*ast.ExternFuncDecl{},
		externType:        map[string]*ast.ExternTypeDecl{},
		externMethods:     map[string]map[string]*ast.ExternMethod{},
		externFields:      map[string]map[string]*ast.ExternField{},
		externPkgs:        map[string]bool{},
	}
	// Pre-scan foreign-binding decls (ffi.md) so call / type / member
	// lowering and the import pre-walk can resolve them.
	g.scanExterns(f)
	// Predeclared sum-type variants per `lang-spec/builtins.md`
	// §Option / §Result / §Variant constructors. Registered up
	// front so identifier resolution and match-arm payload
	// binding find them. Payload-field names match the
	// auto-emitted Go-side struct (e.g. `Some.value` → field
	// `SomeValue` per the `<Var><FieldName>` convention).
	g.variant["None"] = variantInfo{owner: "Option", tag: 0}
	g.variant["Some"] = variantInfo{owner: "Option", tag: 1, fields: []*ast.FieldDecl{{Name: "value"}}}
	g.variant["Ok"] = variantInfo{owner: "Result", tag: 0, fields: []*ast.FieldDecl{{Name: "value"}}}
	g.variant["Err"] = variantInfo{owner: "Result", tag: 1, fields: []*ast.FieldDecl{{Name: "err"}}}
	// Predeclared container classes per `lang-spec/builtins.md`
	// §Map / §Set / §Stack. Registered with static-method names
	// so `Map<K, V>.new()` lowers through the PR-G2
	// static-method-on-generic-class path. Instance method calls
	// (`m.get(k)`, `s.add(e)`, ...) lower to plain Go method
	// dispatch on the inline-emitted struct.
	g.class["Map"] = classInfo{
		generic: true,
		statics: map[string]bool{"new": true},
	}
	g.class["Set"] = classInfo{
		generic: true,
		statics: map[string]bool{"new": true, "from": true},
	}
	g.class["Stack"] = classInfo{
		generic: true,
		statics: map[string]bool{"new": true},
	}
	// `import reflect` is a Aril-internal module — signals that
	// codegen should emit the reflection layer (Dynamic struct,
	// TypeDescriptor, registry, helper funcs). It is NOT a
	// Go-stdlib binding (D6 / D18); the Go-side `import "reflect"`
	// added by writeHeader is for the descriptor registry's
	// runtime type lookup, not user code.
	for _, im := range f.Imports {
		if im.Path == "reflect" {
			g.usesReflect = true
		}
	}
	// Pre-walk: detect predeclared-sum / container usage so the
	// header emits only the corresponding Go-side definitions.
	// Programs that touch none of them emit identical Go to
	// pre-F5b (no fixture churn for unrelated tests).
	g.detectPredeclaredUsage(f)
	// Transitive deps: container methods produce Option / Result
	// values, so any use of those containers forces those
	// predeclared sums into the binary too.
	if g.usesMap || g.usesStack {
		g.usesOption = true
	}
	if g.usesStack {
		g.usesResult = true
	}
	// Fix the runtime mode now that usesReflect is final — it must be
	// resolved before the reflect descriptor pre-collection below, whose
	// descRefForType() consults rt().
	g.resolveRuntimeMode()
	// reflect.unbox<T> returns Result<T, error>; reflect.fields /
	// reflect.fieldValue (PR-R2) will return Result<Dynamic>, so
	// any reflection use pulls Result + Option into the binary.
	if g.usesReflect {
		g.usesResult = true
		g.usesOption = true
		// Pre-collect runtime descriptors for every user-declared
		// class and sum type so the reflection-prelude emitted
		// from writeHeader can include the init() registration
		// block.
		// Collect set of declared class names first so field-type
		// resolution can reference them (descRef for a field of
		// class-type X is `arilDesc_X`).
		classNames := map[string]bool{}
		for _, d := range f.Decls {
			if cd, ok := d.(*ast.ClassDecl); ok && len(cd.TypeParams) == 0 {
				classNames[cd.Name] = true
			}
		}
		for _, d := range f.Decls {
			switch v := d.(type) {
			case *ast.ClassDecl:
				if len(v.TypeParams) != 0 {
					continue // generic-instantiation descriptors land later
				}
				var fields []fieldDescInfo
				for _, cf := range v.Fields {
					fields = append(fields, fieldDescInfo{
						arilName: cf.Name,
						descRef:  g.descRefForType(cf.DeclType, classNames),
					})
				}
				g.descriptors = append(g.descriptors, descInfo{
					arilName: v.Name,
					goType:   "*main." + v.Name,
					kind:     "KindClass",
					fields:   fields,
				})
			case *ast.TypeDecl:
				if _, ok := v.Body.(*ast.SumTypeBody); ok {
					g.descriptors = append(g.descriptors, descInfo{
						arilName: v.Name,
						goType:   "main." + v.Name,
						kind:     "KindSum",
					})
				}
			}
		}
	}
	// First pass — register sum-type variants so later
	// expression / pattern lowering can qualify Variant idents
	// to their Go-side constants and tag numbers. Also register
	// classes (PR-F4) so Call/Field lowering can detect
	// constructor calls and static-method calls.
	for _, d := range f.Decls {
		if td, ok := d.(*ast.TypeDecl); ok {
			g.userTypeNames[td.Name] = true
			if sb, ok := td.Body.(*ast.SumTypeBody); ok {
				for i, v := range sb.Variants {
					g.variant[v.Name] = variantInfo{owner: td.Name, tag: i, fields: v.Fields, sumTypeParams: td.TypeParams}
				}
			}
			if rb, ok := td.Body.(*ast.RecordTypeBody); ok {
				fts := map[string]ast.TypeExpr{}
				for _, fd := range rb.Fields {
					fts[fd.Name] = fd.DeclType
				}
				g.fieldTypes[td.Name] = fts
			}
		}
		if cd, ok := d.(*ast.ClassDecl); ok {
			g.userTypeNames[cd.Name] = true
			ci := classInfo{
				statics: map[string]bool{},
				generic: len(cd.TypeParams) > 0,
			}
			for _, m := range cd.Methods {
				if m.IsStatic {
					ci.statics[m.Name] = true
				}
			}
			g.class[cd.Name] = ci
			fts := map[string]ast.TypeExpr{}
			for _, cf := range cd.Fields {
				fts[cf.Name] = cf.DeclType
			}
			g.fieldTypes[cd.Name] = fts
		}
	}
	// Register the predeclared Kind variants per
	// `lang-spec/builtins.md` §reflect AFTER user sum decls have
	// populated g.variant so collision is detectable. Once
	// `import reflect` is present, the names Primitive / Class /
	// Sum / Slice / Function / Unit are reserved at the variant
	// namespace; user sums sharing any of them yield E0104-style
	// ambiguity (sema PR moves this to a proper `.aril`-coordinate
	// diagnostic).
	if g.usesReflect {
		kindVariants := []string{"Primitive", "Class", "Sum", "Slice", "Function", "Unit"}
		for i, name := range kindVariants {
			if existing, ok := g.variant[name]; ok && existing.owner != "Kind" {
				return "", fmt.Errorf("codegen: variant name %q in user sum-type %q collides with predeclared reflect.Kind.%s — rename the variant or drop `import reflect`", name, existing.owner, name)
			}
			g.variant[name] = variantInfo{owner: "Kind", tag: i}
		}
	}
	g.writeHeader(f)
	for _, d := range f.Decls {
		// Attribute this decl's //line directives to its own source
		// file (multi-file package, RFC-0002). Reset the emitted-line
		// memo so a same-numbered line in a different file still emits
		// a fresh directive.
		if g.file != declFile[d] {
			g.file = declFile[d]
			g.emittedLine = 0
		}
		switch v := d.(type) {
		case *ast.FuncDecl:
			if err := g.emitFuncDecl(v); err != nil {
				return "", err
			}
		case *ast.TypeDecl:
			if err := g.emitTypeDecl(v); err != nil {
				return "", err
			}
		case *ast.ClassDecl:
			if err := g.emitClassDecl(v); err != nil {
				return "", err
			}
		case *ast.InterfaceDecl:
			if err := g.emitInterfaceDecl(v); err != nil {
				return "", err
			}
		case *ast.TopLevelLet:
			// Module-level constant → package-level `var Name [T] =
			// value`. Go resolves package-var init order, so source
			// order need not be topological. Indent is 0 at package
			// scope; emitLetOrVar emits the same `var` form as a
			// body-level `let` (lowering-go.md §TopLevelLet).
			if err := g.emitLetOrVar(v.Span, v.Name, v.DeclType, v.Value); err != nil {
				return "", err
			}
		case *ast.ExternTypeDecl, *ast.ExternFuncDecl, *ast.ExternImplDecl:
			// Foreign bindings emit no Go of their own — the binding is
			// lowered at each call / type / member use site (ffi.md,
			// lowering-go.md §ForeignCall). The declarations are pure
			// signature metadata, pre-scanned by scanExterns.
		default:
			return "", fmt.Errorf("codegen: unhandled top-level decl %T", d)
		}
	}
	// gofmt -s pass — guarantees the output round-trips through
	// gofmt to itself (test-contract.md §GO, lowering-go.md
	// §Output formatting).
	out, err := format.Source([]byte(g.b.String()))
	if err != nil {
		// E0801 internal: codegen emitted malformed Go. This
		// should never reach a user under correct sema; if it
		// does, it's a compiler bug and the raw buffer is
		// included for compiler-developer triage only.
		return "", fmt.Errorf("internal[E0801]: codegen produced unparseable Go (please file a bug): %w\n--- raw output ---\n%s", err, g.b.String())
	}
	return string(out), nil
}

type gen struct {
	b      strings.Builder
	file   string
	info   *sema.Info
	indent int
	// Runtime emission mode (Block R, D18). vendoredRequested records
	// the caller's choice; runtimePrefix is the package selector
	// resolved after the predeclared-usage pre-walk ("arilrt." in
	// effective vendored mode, "" inline) and prepended to every runtime
	// symbol via rt(); runtimeImport is the Go import path of the
	// vendored arilrt package. A reflection-using program falls back to
	// inline (the reflect layer is not vendored yet), so runtimePrefix
	// stays "" even when vendoredRequested — see resolveRuntimeMode.
	vendoredRequested bool
	runtimePrefix     string
	runtimeImport     string
	// userTypeNames holds the names of user-declared types (classes,
	// sums, records) — used to detect a user type that shadows a runtime
	// type name (e.g. a user `class Map` or `type Dynamic`), so
	// emitTypeExpr emits the user's own type rather than arilrt.X. Unlike
	// g.class, it excludes the codegen-injected predeclared Map/Set/Stack.
	userTypeNames map[string]bool
	// emittedLine tracks the source line whose //line directive
	// has most recently been written, so we avoid emitting the
	// same directive twice in a row.
	emittedLine int
	// variant maps a variant identifier (e.g. "Red") to its
	// owning sum-type and declaration-order tag (per
	// lowering-go.md §Variant-tag numbering). Populated during
	// the first decl pass in Emit and consumed by expression /
	// pattern lowering.
	variant map[string]variantInfo
	// class maps a class name (e.g. "Counter") to its static
	// methods. Populated during the first decl pass in Emit.
	// emitCall uses this to detect constructor calls
	// (`Counter(...)` → `&Counter{...}`) and static-method
	// calls (`Counter.make(...)` → `counterMake(...)`).
	class map[string]classInfo
	// fieldTypes maps a record/class name to its fields' declared Aril
	// TypeExprs. emitBraceLit sets expectType from it so a constructor
	// field value whose type Go can't infer — a bare `None` / `Ok` in
	// `Envelope{ q: None, … }` — gets its type args stamped from the
	// field's declared type (§Constructor type-argument stamping).
	fieldTypes map[string]map[string]ast.TypeExpr
	// matchTempCounter generates unique temp names for the
	// subject of a `match` when any arm binds payload fields.
	// Per `lowering-go.md` §MatchIR — capture subject once to
	// keep side-effects from re-running per arm.
	matchTempCounter int
	// usesOption / usesResult — set by the pre-walk in Emit
	// when the program references the corresponding predeclared
	// sum type, either by NamedType ("Option"/"Result") or by
	// variant constructor / pattern (Some/None/Ok/Err). The
	// header emits the Go-side definitions only when set.
	usesOption    bool
	usesResult    bool
	usesMap       bool
	usesSet       bool
	usesStack     bool
	usesReflect   bool
	usesMakeSlice bool
	usesScan      bool
	// usesScan2 / usesScan3 — the multi-value stdin bindings
	// `fmt.scan2<A,B>()` / `fmt.scan3<A,B,C>()`, lowered to the
	// Scan2 / Scan3 helpers (Result<(A,B[,C]), error>).
	usesScan2    bool
	usesScan3    bool
	usesResultOf bool
	// usesResultUnit — an extern referent returning a bare Go `error`
	// (Aril `Result<unit, error>`) is lifted via the ResultUnit
	// helper (lowering-go.md §ForeignCall).
	usesResultUnit bool
	// usesJSON — any json.* binding is used, so Go's encoding/json is
	// imported and (with usesOption) the Option ⇄ null/value JSON
	// methods are emitted. usesJSONParse additionally forces the
	// JSONParse helper (json.parse<T>).
	usesJSON      bool
	usesJSONParse bool
	usesTryRecv   bool
	usesScope     bool
	// usesSortSorted — `sort.sorted(s, less)` is used, so its inline
	// Sorted helper (copy + sort.SliceStable) and Go's "sort"
	// import are needed.
	usesSortSorted bool
	// usesErrorCtor — the `error(msg)` free constructor (builtins.md)
	// is used, so its lowering `errors.New(msg)` needs Go's "errors".
	usesErrorCtor bool
	// groupVars is the stack of structured-concurrency group binding
	// names, one per enclosing `scope` IIFE. A `spawn` registers on
	// the innermost (top-of-stack). inSpawnBody flags that `return
	// Ok(())` / `return Err(e)` must lower to the group's error
	// channel (`return nil` / `return <e>`) rather than a Result.
	groupVars   []string
	inSpawnBody bool
	// usedGoPkgs — Go stdlib packages actually referenced in the
	// emitted output (a `pkg.Sym`). Populated by the pre-walk; the
	// import block is this set ∩ the .aril imports, so a binding that
	// lowers to a Go conversion (strings.fromBytes → string(...))
	// does not drag in an unused import.
	usedGoPkgs map[string]bool
	// Foreign bindings (ffi.md), pre-scanned from the file's extern
	// decls before the import pre-walk. externFunc/externType key on
	// the Aril name; externMethods/externFields key handle→member.
	// externPkgs collects the Go import paths the emitted bindings
	// reference, added to the import block by writeHeader (these come
	// from `@go`, not the .aril imports).
	externFunc    map[string]*ast.ExternFuncDecl
	externType    map[string]*ast.ExternTypeDecl
	externMethods map[string]map[string]*ast.ExternMethod
	externFields  map[string]map[string]*ast.ExternField
	externPkgs    map[string]bool
	// descriptors collected during emit — for each user-declared
	// type that has a Aril-side descriptor, we emit a
	// `arilDesc_<Name>` package-level var plus an init()
	// registration into the descriptor map keyed by the Go-side
	// type name. Consumed by reflect.box runtime lookup.
	descriptors []descInfo
	// curFuncReturn — the Aril return TypeExpr of the function /
	// method currently being emitted. Consumed by TryExpr
	// lowering to know whether the early-return target is
	// `Option<U>` or `Result<U, E>` and to extract U / E for
	// the wrapped return value.
	curFuncReturn ast.TypeExpr
	// expectType — the Aril TypeExpr the next emitted expression is
	// expected to produce, set at return / typed-binding positions and
	// consumed (then cleared) by emitCall to supply explicit type args
	// to predeclared Result/Option constructors whose un-constrained
	// type parameter Go cannot infer from the argument alone (`Ok(v)`
	// leaves E open, `Err(e)` leaves T open). nil when no context flows.
	expectType ast.TypeExpr
	// sumCtorArgs — Go type-arg strings for the generic sum currently
	// being constructed, threaded through a payload-variant ctor call's
	// arguments (§Generics). A nested *nullary* variant (`Leaf`) has no
	// argument for Go to infer from, so it stamps these explicitly
	// (`TreeLeaf[int]()`). nil outside a generic-sum ctor call.
	sumCtorArgs []string
	// tryTempCounter generates unique temp names for `try`
	// emission. Same hygiene as matchTempCounter.
	tryTempCounter int
	// tryHoist maps a `try` expression that has been pre-emitted as a
	// statement preamble (by hoistExprTries) to its temp identifier.
	// emitExpr substitutes `<tmp>.V` for the node, enabling `try` in
	// expression position (call args, operands) — desugaring.md §T-Try.
	tryHoist map[*ast.TryExpr]string
	// destructureTempCounter generates unique temp names for the
	// `let (a, b) = e` tuple-destructuring binding.
	destructureTempCounter int
	// loopTempCounter generates unique throwaway counter names for
	// `for _ in low..high` (a wildcard loop var over a numeric range,
	// where Go's `i++` form needs a named — not `_` — counter).
	loopTempCounter int
}

type variantInfo struct {
	owner         string           // owning sum-type name (e.g. "Color")
	tag           int              // declaration order, used for the Tag field
	fields        []*ast.FieldDecl // payload fields, nil/empty for nullary variants
	sumTypeParams []string         // owning sum's type params (`Tree<T>`); nil for non-generic
}

type classInfo struct {
	statics map[string]bool // names of `static` methods
	generic bool            // true iff the class has type parameters
}

// descInfo records one runtime type descriptor that codegen
// will emit at the bottom of the prelude (per
// `lang-spec/builtins.md` §reflect / `lang-spec/lowering-go.md`
// §Container types). `goType` is the Go-side type spelling used
// as the registry key (`*main.Counter` for classes,
// `main.Color` for sum types, etc.); `kind` is the spec's Kind
// enum value (KindClass / KindSum / KindPrimitive ...).
// `fields` carries per-class field metadata for PR-R2's
// `reflect.fields(t)` and `reflect.fieldValue(v, name)`. Empty
// for non-class descriptors.
type descInfo struct {
	arilName string
	goType   string
	kind     string // "KindClass" / "KindSum" / etc.
	fields   []fieldDescInfo
}

// fieldDescInfo is one entry in a class descriptor's field
// list. `arilName` is the Aril-source spelling (also the
// Go-side struct field name — emitted lowercase per the
// class-field lowering convention); `descRef` is the Go-side
// var name pointing at the field's type descriptor (e.g.,
// "arilDesc_int" for int, "arilDesc_Counter" for a class
// instance). When the field's static type has no resolvable
// descriptor (slices, generics, ...) descRef is the empty
// string and reflect.fields synthesises a placeholder.
type fieldDescInfo struct {
	arilName string
	descRef  string
}

// rt qualifies a runtime symbol with the active package selector:
// "Option" → "arilrt.Option" in vendored mode, "Option" in inline
// single-file mode (runtimePrefix == ""). Every emission of an
// arilrt-provided type / constructor / helper routes through here so the
// vendored/inline choice is a single prefix toggle (Block R, D18).
func (g *gen) rt(name string) string { return g.runtimePrefix + name }

// sumOwnerName spells a sum-type owner for constructor / type emission:
// the predeclared Option / Result are runtime-provided and take the
// package selector in vendored mode; a user sum keeps its plain Go name.
func (g *gen) sumOwnerName(owner string) string {
	if owner == "Option" || owner == "Result" {
		return g.rt(owner)
	}
	return goIdent(owner)
}

// isRuntimeTypeName reports whether name is a predeclared runtime type
// emitted from the arilrt package (Option / Result / Map / Set / Stack).
// isRuntimeTypeName reports whether name is an arilrt runtime type that
// can appear in a user type annotation (so emitTypeExpr must qualify it
// in vendored mode). Limited to the names sema actually exposes as types:
// the sums/containers (Option/Result/Map/Set/Stack) and the reflect
// Dynamic wrapper. TypeDescriptor/FieldInfo/Kind are NOT here — sema does
// not let user code name them in type position, and the reflect lowering
// qualifies them directly via rt(); listing them would mis-qualify a
// user type that merely shares the name.
func isRuntimeTypeName(name string) bool {
	switch name {
	case "Option", "Result", "Map", "Set", "Stack", "Dynamic":
		return true
	}
	return false
}

// isShadowedRuntimeType reports whether a user-declared type (class, sum,
// or record) shadows the runtime type `name` — in which case emitTypeExpr
// must emit the user's own (unqualified) type, not the arilrt one. Keyed
// on userTypeNames, which (unlike g.class) excludes the codegen-injected
// predeclared Map/Set/Stack, so the predeclared containers stay
// qualified while a genuine user `class Map` / `type Dynamic` does not.
func (g *gen) isShadowedRuntimeType(name string) bool {
	return g.userTypeNames[name]
}

// resolveRuntimeMode fixes the effective runtime mode now that the
// predeclared-usage pre-walk (incl. usesReflect) has run: "arilrt." in
// vendored mode, "" inline. The whole runtime — sums, containers, and
// the reflection layer — is qualified uniformly under vendored mode.
func (g *gen) resolveRuntimeMode() {
	if g.vendoredRequested {
		g.runtimePrefix = "arilrt."
	}
}

func (g *gen) vendored() bool { return g.runtimePrefix != "" }

// usesRuntime reports whether the program references any arilrt-provided
// symbol — the gate for emitting the arilrt import in vendored mode.
// usesReflect is excluded: a reflection program emits inline (see
// resolveRuntimeMode) and never reaches vendored().
func (g *gen) usesRuntime() bool {
	return g.usesOption || g.usesResult || g.usesMap || g.usesSet || g.usesStack ||
		g.usesMakeSlice || g.usesScan || g.usesScan2 || g.usesScan3 ||
		g.usesResultOf || g.usesResultUnit || g.usesJSONParse || g.usesTryRecv ||
		g.usesScope || g.usesSortSorted
}

func (g *gen) writeHeader(f *ast.File) {
	g.b.WriteString("package main\n\n")
	// PR-C bindings shortcut: every Aril import resolves to the
	// matching Go stdlib package by the same name. fmt → "fmt".
	// strconv → "strconv". etc. Sorted for determinism.
	//
	// `reflect` is Aril-internal (D6 / D18 — runtime-supplied,
	// not a Go-stdlib binding); it does NOT translate to a Go
	// import for the user, but if usesReflect is set we add Go's
	// `import "reflect"` for the descriptor registry's internal
	// runtime type lookup.
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	for _, im := range f.Imports {
		if im.Path == "reflect" {
			continue // Aril-internal
		}
		// Drop a stdlib import the generated Go never references —
		// e.g. a program whose only `strings` use is the
		// conversion-binding `strings.fromBytes`. Non-stdlib paths
		// (user modules) are always kept.
		if isStdlibNamespaceName(im.Path) && !g.usedGoPkgs[im.Path] {
			continue
		}
		// Aril import name → Go import path (json → encoding/json); all
		// other stdlib bindings share the name.
		add(goImportPath(im.Path))
	}
	if g.usesReflect && !g.vendored() {
		// reflect.TypeOf for the descriptor registry lookup, plus strconv
		// for the show-helper's primitive formatting — both used only by
		// the inline reflect prelude; vendored mode carries them in arilrt.
		add("reflect")
		add("strconv")
	}
	if g.usesScope {
		// Structured-concurrency scopes lower onto the Group helper. The
		// scope IIFE in main still spells context.Background() directly,
		// so context is needed in both modes; sync is used only by the
		// Group implementation, which lives in arilrt under vendored mode.
		add("context")
		if !g.vendored() {
			add("sync")
		}
	}
	if g.usesErrorCtor {
		// `error(msg)` lowers to errors.New(msg) (builtins.md §error).
		add("errors")
	}
	if g.usesSortSorted && !g.vendored() {
		// sort.sorted lowers onto the Sorted helper (sort.SliceStable),
		// which lives in arilrt under vendored mode — no sort in main.
		add("sort")
	}
	// The scan helpers (fmt.scan* → Scan*) are emitted inline in main in
	// both modes (their anonymous tuple-payload struct must be declared
	// in main so the user's `Ok((a, b))` destructure can read its
	// unexported _0/_1 fields — those are not reachable across the arilrt
	// package boundary), so main always needs fmt for them. json.parse →
	// JSONParse lives in arilrt under vendored mode (single-type payload,
	// no boundary issue), so its encoding/json need is inline-only.
	if g.usesScan || g.usesScan2 || g.usesScan3 {
		add("fmt")
	}
	if g.usesJSONParse && !g.vendored() {
		add("encoding/json")
	}
	if g.vendored() && g.usesRuntime() {
		// The runtime is the imported arilrt package, not inline defs.
		// Only import it when the program actually references a runtime
		// symbol — a runtime-free program (hello world) imports nothing.
		add(g.runtimeImport)
	}
	// Foreign-binding packages (ffi.md) — the Go import paths named by
	// `@go` attributes of the extern funcs/handles actually used. These
	// come from `@go`, not the .aril imports, so they are added directly.
	for p := range g.externPkgs {
		add(p)
	}
	// Sort for determinism.
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && paths[j-1] > paths[j]; j-- {
			paths[j-1], paths[j] = paths[j], paths[j-1]
		}
	}
	if len(paths) == 1 {
		g.b.WriteString("import \"")
		g.b.WriteString(paths[0])
		g.b.WriteString("\"\n\n")
	} else if len(paths) > 1 {
		g.b.WriteString("import (\n")
		for _, p := range paths {
			g.b.WriteString("\t\"")
			g.b.WriteString(p)
			g.b.WriteString("\"\n")
		}
		g.b.WriteString(")\n\n")
	}
	// The scan helpers are emitted inline in both modes (see the fmt note
	// above); their Result references route through rt() so they read
	// from arilrt under vendored mode.
	g.writePredeclaredScan()
	g.writePredeclaredScan2()
	g.writePredeclaredScan3()
	// Reflection: the fixed runtime is inline-only (writePredeclaredReflect
	// self-gates it on !vendored), but the per-program descriptors and
	// init registration are emitted into main in both modes (qualified via
	// rt() under vendored mode).
	g.writePredeclaredReflect()
	// Everything else: in vendored mode the runtime is the imported arilrt
	// package, so emit no inline definitions. In inline mode each helper
	// is emitted only when used (the writePredeclared* usage gates).
	if g.vendored() {
		return
	}
	g.writePredeclaredSums()
	g.writePredeclaredContainers()
	g.writePredeclaredMakeSlice()
	g.writePredeclaredResultOf()
	g.writePredeclaredResultUnit()
	g.writePredeclaredJSONParse()
	g.writeOptionJSONMethods()
	g.writePredeclaredSortSorted()
	g.writePredeclaredTryRecv()
	g.writePredeclaredGroup()
}

func (g *gen) emitBlockBody(b *ast.Block) error {
	for _, s := range b.Stmts {
		if err := g.emitStmt(s); err != nil {
			return err
		}
	}
	if b.Trailing != nil {
		// Statement-context block: the trailing value is discarded.
		// Value-context blocks are lowered to an IIFE by
		// emitBlockAsExpr, which never calls this.
		return g.emitStmt(&ast.ExprStmt{Span: b.Trailing.NodeSpan(), Expr: b.Trailing})
	}
	return nil
}

// emitFuncBody lowers a function / method / closure body. Unlike a
// statement-position block, the trailing expression of a body whose
// result is a value is an *implicit return* (block-as-expression value
// rule; lowering-go.md §"Implicit tail return"): it is emitted in tail
// position (emitTailReturn) so a trailing match/if distributes the
// `return` into its leaves and the declared return type flows down for
// constructor type-arg stamping. A unit-returning body keeps the
// statement-position discard; a body with no trailing (ends in explicit
// `return`s) emits nothing extra. isUnit is passed explicitly because a
// closure's unit-ness can come from sema inference, not just a nil
// annotation.
func (g *gen) emitFuncBody(b *ast.Block, ret ast.TypeExpr, isUnit bool) error {
	for _, s := range b.Stmts {
		if err := g.emitStmt(s); err != nil {
			return err
		}
	}
	if b.Trailing == nil {
		return nil
	}
	if isUnit {
		return g.emitStmt(&ast.ExprStmt{Span: b.Trailing.NodeSpan(), Expr: b.Trailing})
	}
	prev := g.expectType
	g.expectType = ret
	err := g.emitTailReturn(b.Trailing)
	g.expectType = prev
	return err
}

// isUnitReturn reports whether a declared return type carries no Go
// value: a nil annotation (the implicit unit return) or an explicit
// `unit`.
func isUnitReturn(t ast.TypeExpr) bool {
	if t == nil {
		return true
	}
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "unit"
}

func (g *gen) emitStmt(s ast.Stmt) error {
	switch v := s.(type) {
	case *ast.ExprStmt:
		// ReturnExpr (DivergingExpr): lower to Go `return` stmt.
		if r, ok := v.Expr.(*ast.ReturnExpr); ok {
			// Inside a `spawn` body the func returns `error`, so a
			// `return Ok(())` / `return Err(e)` (Result<unit, E>) is
			// converted to the group's error channel (lowering-go.md
			// §SpawnIR).
			if g.inSpawnBody {
				return g.emitSpawnReturn(r)
			}
			// `return try e` — emit the try preamble, then
			// `return tmp.V`.
			if try, ok := r.Value.(*ast.TryExpr); ok {
				tmp, err := g.emitTryPreamble(try)
				if err != nil {
					return err
				}
				g.line(r.Span.StartLine)
				g.writeIndent()
				g.b.WriteString("return ")
				g.b.WriteString(tmp)
				g.b.WriteString(".V\n")
				return nil
			}
			if r.Value == nil {
				g.line(v.Span.StartLine)
				g.writeIndent()
				g.b.WriteString("return\n")
				return nil
			}
			// `return f(try g())` / `return a + try b()` — hoist the
			// nested try preambles before the `return` line.
			if err := g.hoistTriesIfSafe(r.Value); err != nil {
				return err
			}
			g.line(v.Span.StartLine)
			g.writeIndent()
			g.b.WriteString("return ")
			// The returned value is expected to be the function's
			// declared return type; thread it so a predeclared
			// Result/Option constructor gets explicit type args.
			prevExpect := g.expectType
			g.expectType = g.curFuncReturn
			err := g.emitExpr(r.Value)
			g.expectType = prevExpect
			if err != nil {
				return err
			}
			g.b.WriteByte('\n')
			return nil
		}
		// Bare `try e` as a discarded expression statement.
		if try, ok := v.Expr.(*ast.TryExpr); ok {
			_, err := g.emitTryPreamble(try)
			return err
		}
		// Diverging loop expressions lower to Go statements.
		if _, ok := v.Expr.(*ast.BreakExpr); ok {
			g.line(v.Span.StartLine)
			g.writeIndent()
			g.b.WriteString("break\n")
			return nil
		}
		if _, ok := v.Expr.(*ast.ContinueExpr); ok {
			g.line(v.Span.StartLine)
			g.writeIndent()
			g.b.WriteString("continue\n")
			return nil
		}
		// MatchExpr: lower to Go `switch` statement.
		if m, ok := v.Expr.(*ast.MatchExpr); ok {
			return g.emitMatchAsStmt(m)
		}
		// Block-as-expression in statement position: run the
		// statements inline, discarding the trailing value.
		if blk, ok := v.Expr.(*ast.Block); ok {
			return g.emitBlockBody(blk)
		}
		// IfExpr in statement position: same shape as an if-statement.
		if ie, ok := v.Expr.(*ast.IfExpr); ok {
			return g.emitIfExprAsStmt(ie)
		}
		// `spawn { … }` registers a goroutine on the enclosing
		// scope's group (lowering-go.md §SpawnIR).
		if sp, ok := v.Expr.(*ast.SpawnExpr); ok {
			return g.emitSpawnStmt(sp)
		}
		// Expression-statement (`stack.push(try …)`) — hoist any
		// nested `try` to preambles before emitting the call.
		if err := g.hoistTriesIfSafe(v.Expr); err != nil {
			return err
		}
		g.line(v.Span.StartLine)
		g.writeIndent()
		if err := g.emitExpr(v.Expr); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	case *ast.IfStmt:
		return g.emitIfStmt(v)
	case *ast.WhileStmt:
		return g.emitWhileStmt(v)
	case *ast.SelectStmt:
		return g.emitSelectStmt(v)
	case *ast.DeferStmt:
		// lowering-go.md §Defer: `defer call(args)` → Go `defer
		// call(args)` directly (G27 — adopted from Go).
		g.line(v.Span.StartLine)
		g.writeIndent()
		g.b.WriteString("defer ")
		if err := g.emitExpr(v.Call); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	case *ast.ForStmt:
		return g.emitForStmt(v)
	case *ast.LetStmt:
		switch pat := v.Pattern.(type) {
		case *ast.IdentPat:
			return g.emitLetOrVar(v.Span, pat.Name, v.DeclType, v.Value)
		case *ast.TuplePat:
			return g.emitDestructureLet(v.Span, pat, v.Value)
		default:
			return fmt.Errorf("codegen: unsupported `let` pattern %T", v.Pattern)
		}
	case *ast.VarStmt:
		return g.emitLetOrVar(v.Span, v.Name, v.DeclType, v.Value)
	case *ast.AssignStmt:
		// `total = total + try f()` / `m[try k()] = v` — hoist nested
		// try preambles before any of the assignment is emitted. LValue
		// then Value as one frame, so the order check spans both.
		if err := g.hoistTriesIfSafe(v.LValue, v.Value); err != nil {
			return err
		}
		g.line(v.Span.StartLine)
		g.writeIndent()
		// `m[k] = val` where m is a Map<K, V> lowers to
		// `m.set(k, val)` — the wrapper's set() updates both the
		// internal map and the insertion-order slice. Direct
		// `m.m[k] = val` would bypass that and break iteration
		// order for any later `.entries()`/`.keys()` call.
		if idx, ok := v.LValue.(*ast.Index); ok {
			if id, ok := idx.Receiver.(*ast.Ident); ok && g.varKindOf(id) == "Map" {
				if err := g.emitExpr(id); err != nil {
					return err
				}
				g.b.WriteString(".Set(")
				if err := g.emitExpr(idx.Idx); err != nil {
					return err
				}
				g.b.WriteString(", ")
				if err := g.emitExpr(v.Value); err != nil {
					return err
				}
				g.b.WriteString(")\n")
				return nil
			}
		}
		if err := g.emitExpr(v.LValue); err != nil {
			return err
		}
		g.b.WriteString(" = ")
		if err := g.emitExpr(v.Value); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	}
	return fmt.Errorf("codegen: unhandled stmt %T", s)
}

// emitBraceLit lowers a brace literal. A record literal becomes a Go
// struct literal `TypeName{ field: value, … }` (same-package field
// names map directly). Map / Set / Stack literals lower to the
// predeclared container helpers, sharing the `.new()` / `.from()`
// representation (builtins.md §Map / §Set / §Stack).
func (g *gen) emitBraceLit(b *ast.BraceLit) error {
	if len(b.TypeName.QName) == 1 {
		switch b.TypeName.QName[0] {
		case "Map":
			return g.emitMapBraceLit(b)
		case "Set":
			return g.emitSetBraceLit(b)
		case "Stack":
			return g.emitStackBraceLit(b)
		}
	}
	if b.Kind != ast.BraceRecord {
		return fmt.Errorf("codegen: %s brace literal not yet supported — use the container constructor / `.new()`", b.Kind)
	}
	if len(b.TypeName.QName) != 1 {
		return fmt.Errorf("codegen: qualified record type name not supported")
	}
	name := b.TypeName.QName[0]
	ci, isClass := g.class[name]
	if isClass && ci.generic && len(b.TypeName.Args) == 0 {
		return fmt.Errorf("codegen: brace literal on generic class %s needs explicit type arguments — write %s<T>{…}", name, name)
	}
	// A class is a reference type — `Bar{ x: 6 }` constructs `&Bar{…}`
	// so its methods (declared on `*Bar`) are reachable.
	if isClass {
		g.b.WriteByte('&')
	}
	g.b.WriteString(goIdent(name))
	// Generic record/class literal `Box<int>{…}` lowers to the
	// instantiated Go type `Box[int]{…}` — Go cannot infer struct
	// type parameters from a composite literal.
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteByte('{')
	for i, e := range b.Entries {
		re, ok := e.(*ast.RecordEntry)
		if !ok {
			return fmt.Errorf("codegen: non-record entry %T in record literal", e)
		}
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString(exportFieldName(re.Name))
		g.b.WriteString(": ")
		// Flow the field's declared type as the expected type so a
		// constructor value Go can't infer — `q: None` /
		// `r: Ok(v)` — gets its type args stamped (§Constructor
		// type-argument stamping). nil when the field type is unknown.
		prevExpect := g.expectType
		if fts, ok := g.fieldTypes[name]; ok {
			g.expectType = fts[re.Name]
		} else {
			g.expectType = nil
		}
		err := g.emitExpr(re.Value)
		g.expectType = prevExpect
		if err != nil {
			return err
		}
	}
	g.b.WriteByte('}')
	return nil
}

// emitSetBraceLit lowers `Set<T>{}` → `NewSet[T]()` and
// `Set<T>{e1,…}` → `SetFrom([]T{e1,…})`, reusing the predeclared Set
// helpers (Go infers `SetFrom`'s `T` from the slice literal).
func (g *gen) emitSetBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) == 0 {
		g.b.WriteString(g.rt("NewSet"))
		if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
			return err
		}
		g.b.WriteString("()")
		return nil
	}
	if len(b.TypeName.Args) != 1 {
		return fmt.Errorf("codegen: Set literal needs an element type argument — write Set<T>{…}")
	}
	g.b.WriteString(g.rt("SetFrom") + "([]")
	if err := g.emitTypeExpr(b.TypeName.Args[0]); err != nil {
		return err
	}
	g.b.WriteByte('{')
	for i, e := range b.Entries {
		se, ok := e.(*ast.SetEntry)
		if !ok {
			return fmt.Errorf("codegen: non-set entry %T in Set literal", e)
		}
		if i > 0 {
			g.b.WriteString(", ")
		}
		if err := g.emitExpr(se.Value); err != nil {
			return err
		}
	}
	g.b.WriteString("})")
	return nil
}

// emitMapBraceLit lowers `Map<K,V>{}` → `NewMap[K,V]()` and a
// non-empty `Map<K,V>{ k: v, … }` to an insertion IIFE
// (`func() *Map[K,V] { m := NewMap[K,V](); m.set(k, v); …; return m }()`)
// — Map has no construct-from-entries helper, and an IIFE keeps the
// literal a single Go expression.
func (g *gen) emitMapBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) == 0 {
		g.b.WriteString(g.rt("NewMap"))
		if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
			return err
		}
		g.b.WriteString("()")
		return nil
	}
	if len(b.TypeName.Args) != 2 {
		return fmt.Errorf("codegen: Map literal needs key and value type arguments — write Map<K,V>{…}")
	}
	g.b.WriteString("func() *Map")
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteString(" { m := NewMap")
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteString("(); ")
	for _, e := range b.Entries {
		me, ok := e.(*ast.MapEntry)
		if !ok {
			return fmt.Errorf("codegen: non-map entry %T in Map literal", e)
		}
		g.b.WriteString("m.Set(")
		if err := g.emitExpr(me.Key); err != nil {
			return err
		}
		g.b.WriteString(", ")
		if err := g.emitExpr(me.Value); err != nil {
			return err
		}
		g.b.WriteString("); ")
	}
	g.b.WriteString("return m }()")
	return nil
}

// emitStackBraceLit lowers `Stack<T>{}` → `NewStack[T]()`. A Stack
// literal is always empty (ast.md §BraceLit); sema rejects entries.
func (g *gen) emitStackBraceLit(b *ast.BraceLit) error {
	if len(b.Entries) != 0 {
		return fmt.Errorf("codegen: Stack literal must be empty — push elements after construction")
	}
	g.b.WriteString(g.rt("NewStack"))
	if err := g.emitTypeArgs(b.TypeName.Args); err != nil {
		return err
	}
	g.b.WriteString("()")
	return nil
}

// emitTupleLit lowers a tuple literal to an anonymous-struct literal.
// The struct type comes from sema's inferred Tuple so the literal
// shares its Go type with any matching annotation / field access
// (structural equivalence).
func (g *gen) emitTupleLit(t *ast.TupleLit) error {
	var structType string
	if g.info != nil {
		if tt, ok := g.info.Type[t].(*sema.Tuple); ok {
			if s, ok := g.goTypeFromSema(tt); ok {
				structType = s
			}
		}
	}
	if structType == "" {
		return fmt.Errorf("codegen: cannot infer Go type for tuple literal — annotate the binding")
	}
	g.b.WriteString(structType)
	g.b.WriteByte('{')
	for i, ce := range t.Components {
		if i > 0 {
			g.b.WriteString(", ")
		}
		g.b.WriteString("_")
		g.b.WriteString(strconv.Itoa(i))
		g.b.WriteString(": ")
		if err := g.emitExpr(ce); err != nil {
			return err
		}
	}
	g.b.WriteByte('}')
	return nil
}

// emitReflectCall lowers a `reflect.X(args)` call to the
// corresponding inline arilrt helper emitted by
// `writePredeclaredReflect`. Current surface: box / unbox /
// typeOf / typeName / kind / fields / fieldValue / show
// (PR-R1 .. PR-R3). Variants / methods / typeArgs / elementType
// land with later Block-R PRs.
// reflectFuncName maps a reflect.* method name to its arilrt export.
// kind → KindOf avoids clashing with the Kind type.
var reflectFuncName = map[string]string{
	"typeOf":     "TypeOf",
	"typeName":   "TypeName",
	"kind":       "KindOf",
	"fields":     "Fields",
	"fieldValue": "FieldValue",
	"show":       "Show",
}

func (g *gen) emitReflectCall(name string, typeArgs []ast.TypeExpr, args []ast.Expr) error {
	switch name {
	case "box":
		g.b.WriteString(g.rt("Box"))
		if len(typeArgs) > 0 {
			g.b.WriteByte('[')
			if err := g.emitTypeExpr(typeArgs[0]); err != nil {
				return err
			}
			g.b.WriteByte(']')
		}
		g.b.WriteByte('(')
		if len(args) != 1 {
			return fmt.Errorf("codegen: reflect.box expects exactly one argument, got %d", len(args))
		}
		if err := g.emitExpr(args[0]); err != nil {
			return err
		}
		g.b.WriteByte(')')
		return nil
	case "unbox":
		if len(typeArgs) != 1 {
			return fmt.Errorf("codegen: reflect.unbox requires exactly one explicit type argument `reflect.unbox<T>(d)`")
		}
		g.b.WriteString(g.rt("Unbox") + "[")
		if err := g.emitTypeExpr(typeArgs[0]); err != nil {
			return err
		}
		g.b.WriteString("](")
		if len(args) != 1 {
			return fmt.Errorf("codegen: reflect.unbox expects exactly one argument, got %d", len(args))
		}
		if err := g.emitExpr(args[0]); err != nil {
			return err
		}
		g.b.WriteByte(')')
		return nil
	case "typeOf", "typeName", "kind", "fields", "fieldValue", "show":
		g.b.WriteString(g.rt(reflectFuncName[name]))
		g.b.WriteByte('(')
		for i, a := range args {
			if i > 0 {
				g.b.WriteString(", ")
			}
			if err := g.emitExpr(a); err != nil {
				return err
			}
		}
		g.b.WriteByte(')')
		return nil
	}
	return fmt.Errorf("codegen: reflect.%s is not yet supported (methods / variants / variantOf / typeArgs / elementType land later)", name)
}

// semaSliceElem returns the Go element type for an inferred slice
// literal from sema's side-table — used when literal-only inference
// (inferSliceElemType) can't see the element type (e.g. `[v]` with an
// Ident / call element). Returns ("", false) when no usable sema type
// is available, so the caller falls back to literal inference.
func (g *gen) semaSliceElem(lit *ast.SliceLit) (string, bool) {
	if g.info == nil {
		return "", false
	}
	st, ok := g.info.Type[lit].(*sema.Slice)
	if !ok {
		return "", false
	}
	return g.goTypeFromSema(st.Elem)
}

// inferSliceElemType returns the Go-side element type for an
// inferred slice literal. PR-F3 supports Int / String / Bool
// literal elements; anything else returns an error.
func inferSliceElemType(items []ast.Expr) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("codegen: empty inferred-type slice literal — annotate with `[]T{}`")
	}
	switch items[0].(type) {
	case *ast.IntLitExpr:
		return "int", nil
	case *ast.FloatLitExpr:
		return "float64", nil
	case *ast.StringLitExpr:
		return "string", nil
	case *ast.BoolLitExpr:
		return "bool", nil
	}
	return "", fmt.Errorf("codegen: cannot infer element type from %T — annotate the slice literal", items[0])
}

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

// emitDestructureLet lowers `let (a, b) = e` (lowering-go.md
// §Tuple destructuring). The value is bound to a fresh temp once (so a
// side-effecting RHS runs exactly once), then each component is bound
// positionally via bindSubPattern (`a := tmp._0`, …), recursing for
// nested tuples; a `_` component binds nothing. When every component is
// `_` the value is discarded (`_ = e`) so Go sees no unused temp.
func (g *gen) emitDestructureLet(span ast.Span, pat *ast.TuplePat, value ast.Expr) error {
	g.line(span.StartLine)
	g.writeIndent()
	if patternBindsNothing(pat) {
		g.b.WriteString("_ = ")
		if err := g.emitExpr(value); err != nil {
			return err
		}
		g.b.WriteByte('\n')
		return nil
	}
	tmp := g.nextDestructureTemp()
	g.b.WriteString(tmp)
	g.b.WriteString(" := ")
	if err := g.emitExpr(value); err != nil {
		return err
	}
	g.b.WriteByte('\n')
	return g.bindSubPattern(pat, tmp)
}

// patternBindsNothing reports whether an irrefutable let pattern
// introduces no binding at all (every leaf is `_`), so the temp would
// be unused — the value is discarded instead.
func patternBindsNothing(p ast.Pattern) bool {
	switch v := p.(type) {
	case *ast.WildcardPat:
		return true
	case *ast.TuplePat:
		for _, sub := range v.Sub {
			if !patternBindsNothing(sub) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// nextDestructureTemp returns a fresh Go identifier for a
// tuple-destructuring temp, sharing the runtime-prefix convention with
// the other codegen-internal temps.
func (g *gen) nextDestructureTemp() string {
	g.destructureTempCounter++
	return fmt.Sprintf("__aril_destructure_%d", g.destructureTempCounter)
}

// emitLetOrVar lowers both `let` and `var` to Go's `var name [T] = value`.
// Immutability of `let` is a sema concern (not yet implemented); the
// generated Go is identical for both keywords.
func (g *gen) emitLetOrVar(span ast.Span, name string, declType ast.TypeExpr, value ast.Expr) error {
	// `let x = try e` / `var x = try e` — emit the try
	// preamble, then bind the unwrapped value.
	if try, ok := value.(*ast.TryExpr); ok {
		tmp, err := g.emitTryPreamble(try)
		if err != nil {
			return err
		}
		g.line(span.StartLine)
		g.writeIndent()
		g.b.WriteString("var ")
		g.b.WriteString(goIdent(name))
		if declType != nil {
			g.b.WriteByte(' ')
			if err := g.emitTypeExpr(declType); err != nil {
				return err
			}
		}
		g.b.WriteString(" = ")
		g.b.WriteString(tmp)
		g.b.WriteString(".V\n")
		return nil
	}
	// `let x = f(try g())` / `let x = a + try b()` — hoist nested try
	// preambles before the binding line.
	if err := g.hoistTriesIfSafe(value); err != nil {
		return err
	}
	g.line(span.StartLine)
	g.writeIndent()
	g.b.WriteString("var ")
	g.b.WriteString(goIdent(name))
	if declType != nil {
		g.b.WriteByte(' ')
		if err := g.emitTypeExpr(declType); err != nil {
			return err
		}
	}
	g.b.WriteString(" = ")
	// A type annotation gives the value an expected type — thread it
	// so a predeclared Result/Option constructor gets explicit type
	// args (Go does not infer a generic call's type params from the
	// assignment LHS). nil annotation leaves inference unchanged.
	prevExpect := g.expectType
	g.expectType = declType
	err := g.emitExpr(value)
	g.expectType = prevExpect
	if err != nil {
		return err
	}
	g.b.WriteByte('\n')
	return nil
}

// ---- expressions ----

func (g *gen) emitExpr(e ast.Expr) error {
	switch v := e.(type) {
	case *ast.IntLitExpr:
		g.b.WriteString(strconv.FormatInt(v.Value, 10))
		return nil
	case *ast.FloatLitExpr:
		// Re-emit source text; Go accepts the same `3.14` / `1e3`
		// float syntax for its float64.
		g.b.WriteString(v.RawText)
		return nil
	case *ast.StringLitExpr:
		g.b.WriteString(strconv.Quote(v.Value))
		return nil
	case *ast.RuneLitExpr:
		// Re-emit the source text; Go accepts the same `'a'`
		// rune-literal syntax for its rune (int32) type.
		g.b.WriteString(v.RawText)
		return nil
	case *ast.BoolLitExpr:
		if v.Value {
			g.b.WriteString("true")
		} else {
			g.b.WriteString("false")
		}
		return nil
	case *ast.UnitLit:
		// The unit value `()` is Go's zero-byte composite literal
		// (lowering-go.md §Primitive type lowering).
		g.b.WriteString("struct{}{}")
		return nil
	case *ast.ScopeExpr:
		// Value-position structured-concurrency scope → IIFE
		// returning Result[T, error] (lowering-go.md §ScopeIR).
		return g.emitScopeExpr(v)
	case *ast.ThisExpr:
		// lowering-go.md §Implicit receiver — the receiver is
		// named `t` consistently in generated method bodies.
		g.b.WriteString("t")
		return nil
	case *ast.Ident:
		// Variant identifiers (declared in any sum type in the
		// same file) get qualified to their Go-side variable:
		// `Red` → `ColorRed`.
		if info, ok := g.variant[v.Name]; ok {
			// A bare nullary constructor of a *generic* sum is a
			// parameterless generic call Go can't infer — stamp explicit
			// type args (`OptionNone[T]()`, `TreeLeaf[int]()`;
			// lowering-go.md §Container types / §Generics). The args come
			// from the enclosing ctor call's inferred instantiation
			// (g.sumCtorArgs, set while emitting a `Node(…)`'s arguments),
			// else the expected type. User sums with no type params and
			// all other variants emit bare.
			if len(info.sumTypeParams) > 0 {
				if len(g.sumCtorArgs) == len(info.sumTypeParams) {
					g.b.WriteString(g.sumOwnerName(info.owner))
					g.b.WriteString(goIdent(v.Name))
					g.b.WriteByte('[')
					g.b.WriteString(strings.Join(g.sumCtorArgs, ", "))
					g.b.WriteString("]()")
					return nil
				}
				if targs, ok := g.userSumCtorArgsFromExpect(info, g.expectType); ok {
					g.b.WriteString(g.sumOwnerName(info.owner))
					g.b.WriteString(goIdent(v.Name))
					if err := g.emitTypeArgs(targs); err != nil {
						return err
					}
					g.b.WriteString("()")
					return nil
				}
			}
			if targs, _, ok := g.predeclaredCtorTypeArgs(v.Name, g.expectType); ok {
				g.b.WriteString(g.sumOwnerName(info.owner))
				g.b.WriteString(goIdent(v.Name))
				if err := g.emitTypeArgs(targs); err != nil {
					return err
				}
				g.b.WriteString("()")
				return nil
			}
			g.b.WriteString(g.sumOwnerName(info.owner))
			g.b.WriteString(goIdent(v.Name))
			return nil
		}
		// A bare ident that sema resolved to a class field (not a
		// shadowing local/param) is an implicit-receiver reference
		// (name-resolution §Implicit receiver) — emit `t.<field>`,
		// since the Go field lives on the method receiver `t`.
		if g.info != nil {
			if sym := g.info.Symbol[v]; sym != nil && sym.Kind == sema.SymField {
				g.b.WriteString("t.")
				g.b.WriteString(exportFieldName(v.Name))
				return nil
			}
		}
		g.b.WriteString(goIdent(v.Name))
		return nil
	case *ast.SliceLit:
		// Annotated form `[]T{...}` → `[]T{...}` directly.
		// Inferred form `[e_1, ..., e_n]` → `[]TInferred{...}`.
		// PR-F3 infers from the first element when it's an Int /
		// String / Bool literal; otherwise rejects (no sema yet).
		if v.ElemType != nil {
			g.b.WriteString("[]")
			if err := g.emitTypeExpr(v.ElemType); err != nil {
				return err
			}
		} else if elem, ok := g.semaSliceElem(v); ok {
			// Sema typed the literal (e.g. `[v]` from an Ident /
			// call element): use its element type directly.
			g.b.WriteString("[]")
			g.b.WriteString(elem)
		} else {
			// No sema info — fall back to first-literal inference.
			elem, err := inferSliceElemType(v.Items)
			if err != nil {
				return err
			}
			g.b.WriteString("[]")
			g.b.WriteString(elem)
		}
		g.b.WriteByte('{')
		for i, it := range v.Items {
			if i > 0 {
				g.b.WriteString(", ")
			}
			if err := g.emitExpr(it); err != nil {
				return err
			}
		}
		g.b.WriteByte('}')
		return nil
	case *ast.Index:
		// `m[k]` where m is a Map<K, V> lowers to the wrapper's
		// internal `m.m[k]` direct map access — returns V's
		// Go zero value for a missing key (mirrors Go's map
		// semantics). `m.Get(k)` is the explicit-Option form
		// when the user wants the missing case to surface. The raw read
		// goes through the exported At accessor (not the unexported `m`
		// field) so the same emission works across the arilrt package
		// boundary in vendored mode.
		if id, ok := v.Receiver.(*ast.Ident); ok && g.varKindOf(id) == "Map" {
			if err := g.emitExpr(id); err != nil {
				return err
			}
			g.b.WriteString(".At(")
			if err := g.emitExpr(v.Idx); err != nil {
				return err
			}
			g.b.WriteByte(')')
			return nil
		}
		if err := g.emitExpr(v.Receiver); err != nil {
			return err
		}
		g.b.WriteByte('[')
		if err := g.emitExpr(v.Idx); err != nil {
			return err
		}
		g.b.WriteByte(']')
		return nil
	case *ast.Slice:
		if err := g.emitExpr(v.Receiver); err != nil {
			return err
		}
		g.b.WriteByte('[')
		if v.Low != nil {
			if err := g.emitExpr(v.Low); err != nil {
				return err
			}
		}
		g.b.WriteByte(':')
		if v.High != nil {
			if err := g.emitExpr(v.High); err != nil {
				return err
			}
		}
		g.b.WriteByte(']')
		return nil
	case *ast.MatchExpr:
		return g.emitMatchAsExpr(v)
	case *ast.Block:
		return g.emitBlockAsExpr(v)
	case *ast.IfExpr:
		return g.emitIfExprAsValue(v)
	case *ast.ParenExpr:
		// Reproduce the author's grouping so Go preserves the same
		// operator precedence (`a * (b + c)` must not re-associate).
		g.b.WriteByte('(')
		if err := g.emitExpr(v.Inner); err != nil {
			return err
		}
		g.b.WriteByte(')')
		return nil
	case *ast.BraceLit:
		return g.emitBraceLit(v)
	case *ast.ClosureLit:
		return g.emitClosure(v)
	case *ast.TupleLit:
		return g.emitTupleLit(v)
	case *ast.TupleField:
		if err := g.emitExpr(v.Receiver); err != nil {
			return err
		}
		g.b.WriteString("._")
		g.b.WriteString(strconv.Itoa(v.Position))
		return nil
	case *ast.BreakExpr, *ast.ContinueExpr:
		// Diverging loop expressions lower to statements, not Go
		// expressions — they're handled in emitStmt. Reaching here
		// means one was used in value position (e.g. a value-arm
		// `match x { A => break }`), which v1 codegen does not lower.
		return fmt.Errorf("codegen: `break`/`continue` is not usable in value position")
	case *ast.Field:
		return g.emitField(v)
	case *ast.Call:
		return g.emitCall(v)
	case *ast.SpreadArg:
		// `...xs` lowers to Go's trailing `xs...` spread (ffi.md §Variadic).
		if err := g.emitExpr(v.Inner); err != nil {
			return err
		}
		g.b.WriteString("...")
		return nil
	case *ast.Binary:
		if err := g.emitExpr(v.Left); err != nil {
			return err
		}
		g.b.WriteByte(' ')
		g.b.WriteString(v.Op)
		g.b.WriteByte(' ')
		return g.emitExpr(v.Right)
	case *ast.Unary:
		g.b.WriteString(v.Op)
		return g.emitExpr(v.Operand)
	case *ast.ReturnExpr:
		// ReturnExpr is a DivergingExpr; in Go it must appear as
		// a statement (`return [value]`), not in an expression
		// context. The ExprStmt wrapper emitter writes the
		// statement form via emitReturnAsStatement directly, so
		// reaching this branch means a misuse (return in a
		// non-statement context) — emit clearly.
		return fmt.Errorf("codegen: return-expression used outside statement position")
	case *ast.TryExpr:
		// Expression-position `try` (call arg, operand, …) is lowered
		// by hoistExprTries, which pre-emits the early-return preamble
		// as a statement and records the unwrap temp here. The node's
		// value is the temp's payload `<tmp>.V`. A `try` that wasn't
		// hoisted sits in an unsupported frame (value-position
		// match/if/closure arm — a different return frame): error.
		if tmp, ok := g.tryHoist[v]; ok {
			g.b.WriteString(tmp)
			g.b.WriteString(".V")
			return nil
		}
		return g.tryExprErr()
	}
	return fmt.Errorf("codegen: unhandled expression %T", e)
}

func (g *gen) emitField(f *ast.Field) error {
	if err := g.emitExpr(f.Receiver); err != nil {
		return err
	}
	g.b.WriteByte('.')
	// Foreign-handle field access (ffi.md §ExternImpl) takes the Go
	// field name from its `@go` attribute, not the exported-Aril form.
	if fld, ok := g.externFieldOf(f); ok {
		g.b.WriteString(goRefMember(fld.Go, fld.Name))
		return nil
	}
	g.b.WriteString(g.goFieldName(f.Receiver, f.Name))
	return nil
}

// goFieldName maps a Aril *field-value* access `recv.name` to its Go
// spelling. A genuine user record/class field is EXPORTED
// (exportFieldName) so encoding/json can reach it; a stdlib-namespace
// value access (`os.args` → `os.Args`) keeps its binding rename, and
// `.error()` on the predeclared `error` builtin maps to Go's
// `error.Error()` (the PascalCase↔lowerCamel boundary; D14 footnote).
// Method-call selectors do NOT come through here — they use goMethodName
// (call.go), which stays lowercase, so methods remain unexported.
//
// The package-namespace check gates on the receiver's sema *symbol*
// (SymBuiltinModule), not its spelling — a local value that shadows a
// package name (`let sort = Sorter{…}`) is a user value whose fields
// must still export (the recurring name-match footgun: dispatch on the
// resolved symbol, never the spelling).
func (g *gen) goFieldName(receiver ast.Expr, name string) string {
	if name == "error" && g.isErrorBuiltinReceiver(receiver) {
		return "Error"
	}
	if id, ok := receiver.(*ast.Ident); ok && g.isBuiltinModule(id) {
		return mapFieldName(receiver, name)
	}
	return exportFieldName(name)
}

// isDataFieldSelector reports whether `recv.name` names a *data field*
// (as opposed to a method) of recv's record/class type. A func-typed
// data field can be *called* — `handler.fn(x)` — and the callee is then
// an `*ast.Field` whose name must take the exported field spelling
// (goFieldName), not the lowercase method spelling, or it would not
// match the exported Go struct field. Records have only data fields;
// classes split fields vs methods; interfaces/containers/stdlib have
// no data fields reachable this way (→ false, method spelling).
func (g *gen) isDataFieldSelector(receiver ast.Expr, name string) bool {
	if g.info == nil {
		return false
	}
	named, ok := g.info.Type[receiver].(*sema.Named)
	if !ok {
		return false
	}
	switch d := named.Decl.(type) {
	case *ast.ClassDecl:
		for _, fld := range d.Fields {
			if fld.Name == name {
				return true
			}
		}
	case *ast.TypeDecl:
		if rb, ok := d.Body.(*ast.RecordTypeBody); ok {
			for _, fld := range rb.Fields {
				if fld.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// goMethodName maps a Aril method-call selector `recv.name(...)` to its
// Go spelling. Container methods (Map / Set / Stack) take the EXPORTED
// spelling (`.get` → `.Get`) so vendored-mode code can call them across
// the arilrt package boundary; the same exported spelling is used inline
// for a single naming scheme. Other methods keep the pre-export
// behaviour (stdlib renames + the `error`→`Error` boundary, otherwise
// the verbatim lowercase name).
func (g *gen) goMethodName(receiver ast.Expr, name string) string {
	if name == "error" && g.isErrorBuiltinReceiver(receiver) {
		return "Error"
	}
	if g.isContainerTypedExpr(receiver) {
		return exportFieldName(name)
	}
	return mapFieldName(receiver, name)
}

// isContainerTypedExpr reports whether sema typed receiver as one of the
// predeclared container types (Map / Set / Stack), whose methods are
// emitted with their exported Go spelling. Unlike isContainerReceiver
// (Ident-only, container-or-channel), this matches any expression and
// excludes channels.
func (g *gen) isContainerTypedExpr(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	switch g.info.Type[receiver].(type) {
	case *sema.Map, *sema.Set, *sema.Stack:
		return true
	}
	return false
}

// isErrorBuiltinReceiver reports whether sema typed receiver as the
// predeclared `error` type (the Go-error binding boundary).
func (g *gen) isErrorBuiltinReceiver(receiver ast.Expr) bool {
	if g.info == nil {
		return false
	}
	b, ok := g.info.Type[receiver].(*sema.Builtin)
	return ok && b.N == "error"
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
