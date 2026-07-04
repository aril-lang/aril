package codegen

import (
	"fmt"
	"go/format"
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

	// ContractMode selects contract enforcement (RFC-0006): "off" (default,
	// checks elided → byte-identical) or "panic" (a violated obligation
	// aborts). warn/stats are not lowered yet. Empty means "off".
	ContractMode string
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
		contractMode:      opts.ContractMode,
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
	// An `Ordered` type-param bound lowers to `cmp.Ordered`, so the program
	// needs `import "cmp"`. Detected before writeHeader (which emits imports).
	g.detectOrderedBound(f)
	// An enforced channel trace contract (RFC-0007, panic mode) pulls in the
	// arilrt monitor — detected before writeHeader so its import / inline
	// prelude is in place (the per-site emit also sets the flag, but imports
	// are computed before bodies).
	g.detectChannelContracts()
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
					g.variant[v.Name] = variantInfo{owner: td.Name, tag: i, fields: v.Fields, sumTypeParams: ast.TypeParamNames(td.TypeParams)}
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
	// Contract re-entrancy guard (RFC-0006, panic mode): a contract
	// predicate may call the contracted (or a mutually-contracted) function
	// — `ensures setEq(result, union(b, a))` inside `union`. Checking those
	// nested calls' contracts would recurse without bound, so contract
	// checks are disabled while a predicate is being evaluated. v1 is a
	// package-level flag (single-threaded contract checking; a goroutine-
	// local form is future work).
	if g.contractMode == "panic" && (len(g.info.FuncContracts) > 0 || len(g.info.TypeInvariants) > 0) {
		g.b.WriteString("var _arilInContract bool\n\n")
	}
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
	// contractMode is the RFC-0006 enforcement mode ("off" / "panic"); ""
	// is treated as "off". Under off, contract checks are not emitted.
	contractMode string
	// contractResultVar / contractEntryVars carry the predicate-emission
	// substitution while emitting a requires/ensures predicate: `result` →
	// the named return, an entry-binding name → its entry temp. Both empty
	// outside contract-predicate emission.
	contractResultVar string
	contractEntryVars map[string]string
	// contractReceiver is the Go receiver a type-invariant predicate's bare
	// field names lower against: the method receiver `t` at a method-exit
	// check (the empty default), or the construction temp `_arilNew` at a
	// brace-literal construction check.
	contractReceiver string
	runtimePrefix    string
	runtimeImport    string
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
	usesOption bool
	usesResult bool
	// usesOptionMethods / usesResultMethods — a query/defaulting method
	// (isSome/isNone/unwrapOr, isOk/isErr) is called, so the inline prelude
	// emits the method set (builtins.md §Option methods / §Result methods).
	// Gated separately from the type flag so existing programs that never
	// call a method keep their prelude (and goldens) unchanged; vendored
	// mode always has the methods on arilrt. Over-detection (a same-named
	// user method) only emits dead code — never a miscompile.
	usesOptionMethods bool
	usesResultMethods bool
	usesMap           bool
	usesSet           bool
	usesStack         bool
	usesReflect       bool
	usesCmp           bool // an `Ordered` type-param bound → import "cmp" (G3b)
	usesMakeSlice     bool
	usesScan          bool
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
	// usesOptionOf — a comma-ok `(T, bool)` stdlib binding (os.lookupEnv)
	// lifts to Option<T> via the OptionOf helper (the Option mirror of
	// ResultOf's `(T, error)` lift). Forces usesOption.
	usesOptionOf bool
	usesScope    bool
	// usesChanContract — a channel trace contract (RFC-0007) installs the
	// arilrt monitor (RegisterChan / ChanSend / ChanClose / ChanCheckDrained),
	// set only under `--contracts=panic` when an enforced clause fires.
	usesChanContract bool
	// usesSortSorted — `sort.sorted(s, less)` is used, so its inline
	// Sorted helper (copy + sort.SliceStable) and Go's "sort"
	// import are needed.
	usesSortSorted bool
	// usesSlicesReverse — slices.reverse(xs) lowers to the SlicesReverse helper.
	usesSlicesReverse bool
	// usesSortedBy — sort.sortedBy(s, key) lowers to the SortedBy helper.
	usesSortedBy bool
	// usesMapErr — r.mapErr(f) lowers to the MapErr helper (a free function,
	// since a Go method cannot introduce the fresh E2 type param).
	usesMapErr bool
	// usesSlicesDedup — slices.dedup(xs) lowers to the SlicesDedup helper.
	usesSlicesDedup bool
	// usesBigInt — the `big` value-handle (BigInt) is used, so its runtime
	// wrapper is emitted (inline prelude in single-file mode, arilrt import in
	// vendored mode). Set by the pre-walk (a big.fromInt* constructor or a
	// method on a big.BigInt-typed receiver).
	usesBigInt bool
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
	// ctxVars parallels groupVars: the derived-context binding name for
	// each enclosing scope, or "" when that scope's body never reads
	// `scope.context` (then the context is discarded as `_` to avoid an
	// unused variable). A `scope.context` ScopeRef lowers to the
	// top-of-stack name (the nearest enclosing scope).
	ctxVars []string
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
	// loopFrames is the stack of enclosing loops being emitted. Each
	// frame carries the Go label assigned to its `for` (when a `break`
	// inside a lowered `switch`/`select` needs to target the loop
	// rather than the switch — see §LabeledBreak / lowering-go.md) and
	// the current nesting depth of such switch frames within that loop.
	loopFrames []*loopFrame
	// loopLabelCounter generates unique loop label names.
	loopLabelCounter int
}

// loopFrame tracks one enclosing loop during emission. label is "" when
// the loop needs no Go label (no break crosses a switch/select to reach
// it); switchDepth counts the match/select switch frames currently open
// inside this loop, so a `break` knows whether Go's `switch`/`select`
// would capture it.
type loopFrame struct {
	label       string
	switchDepth int
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
		g.usesScope || g.usesSortSorted || g.usesSlicesReverse || g.usesSortedBy || g.usesSlicesDedup || g.usesChanContract ||
		g.usesBigInt || g.usesMapErr
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
		if im.Path == "reflect" || im.Path == "big" {
			// Aril-internal / runtime-backed: `big` maps to the arilrt BigInt
			// wrapper (usesBigInt drives its import / inline prelude), not a Go
			// `big` package (D37).
			continue
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
	if g.usesChanContract && !g.vendored() {
		// The inline channel-contract monitor (RFC-0007) uses sync.Map for the
		// per-channel registry and fmt for the violation message; vendored mode
		// carries both in arilrt.
		add("sync")
		add("fmt")
	}
	if g.usesCmp {
		// An `Ordered` type-param bound lowers to `cmp.Ordered`; the
		// constraint appears in the user's generic signature in main, so
		// `cmp` is needed in both inline and vendored modes (G3b).
		add("cmp")
	}
	if g.usesSortSorted && !g.vendored() {
		// sort.sorted lowers onto the Sorted helper (sort.SliceStable),
		// which lives in arilrt under vendored mode — no sort in main.
		add("sort")
	}
	if g.usesSortedBy && !g.vendored() {
		// sort.sortedBy → the SortedBy helper (sort.SliceStable + a cmp.Ordered
		// key); inline mode needs both imports (vendored carries them in arilrt).
		add("sort")
		add("cmp")
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
	if g.usesBigInt && !g.vendored() {
		// The inline BigInt wrapper (writePredeclaredBigInt) is built on
		// math/big; vendored mode carries it in arilrt.
		add("math/big")
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
	g.writePredeclaredMapErr()
	g.writePredeclaredResultUnit()
	g.writePredeclaredOptionOf()
	g.writePredeclaredJSONParse()
	g.writeOptionJSONMethods()
	g.writePredeclaredSortSorted()
	g.writePredeclaredSlicesReverse()
	g.writePredeclaredSortedBy()
	g.writePredeclaredSlicesDedup()
	g.writePredeclaredTryRecv()
	g.writePredeclaredGroup()
	g.writePredeclaredChanContract()
	g.writePredeclaredBigInt()
}
