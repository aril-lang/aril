package codegen

import "github.com/aril-lang/aril/internal/ast"

// This file holds the predeclared-runtime layer of codegen: the
// detection pre-walk (detectPredeclaredUsage) that decides which
// predeclared sum types / containers a program references, and the
// emitters that write their Go-side definitions (inline, or as
// references to the vendored arilrt package in vendored mode; the
// authoritative source is the arilrt package). Split out of the
// codegen.go god-file; behaviour-preserving.

// writePredeclaredSums emits Go-side definitions for Option<T>
// and Result<T, E> per `lang-spec/builtins.md` §Option / §Result,
// **only** for the sum types the program actually references
// (per the pre-walk in `detectPredeclaredUsage`). Reflection-free
// programs that touch neither pay zero — same emitted Go as
// pre-F5b.
//
// Constructor functions take type-args explicitly so the user
// can write `Some(42)` (Go infers T from the arg), `None<int>()`,
// or `Err<int, string>("boom")` at sites where inference can't
// proceed. The bare-identifier shape `let x: Option<int> = None`
// (no parens) needs sema-driven type inference and lands later;
// until then the user writes the call form.
func (g *gen) writePredeclaredSums() {
	// Struct shape exactly per `lang-spec/lowering-go.md`
	// §Container types — runtime representation: `Option[T]`
	// fields `Tag` + `V`; `Result[T, E]` fields `Tag` + `V` + `E`.
	// These mirror arilrt's exported definitions; this inline form is
	// the single-file (--inline-runtime) emission, used when the runtime
	// is not vendored (writeHeader gates it on !vendored).
	if g.usesOption {
		g.b.WriteString("type Option[T any] struct {\n\tTag uint8\n\tV   T\n}\n")
		g.b.WriteString("func OptionSome[T any](value T) Option[T] {\n\treturn Option[T]{Tag: 1, V: value}\n}\n")
		g.b.WriteString("func OptionNone[T any]() Option[T] {\n\treturn Option[T]{Tag: 0}\n}\n")
	}
	if g.usesResult {
		g.b.WriteString("type Result[T any, E any] struct {\n\tTag uint8\n\tV   T\n\tE   E\n}\n")
		g.b.WriteString("func ResultOk[T any, E any](value T) Result[T, E] {\n\treturn Result[T, E]{Tag: 0, V: value}\n}\n")
		g.b.WriteString("func ResultErr[T any, E any](err E) Result[T, E] {\n\treturn Result[T, E]{Tag: 1, E: err}\n}\n")
	}
}

// writePredeclaredMakeSlice emits the inline helper for the
// `makeSlice<T>(n: int): []T` predeclared builtin (per
// `lang-spec/builtins.md` §makeSlice). Returns a fresh slice
// of length n with every element initialised to T's Go
// zero-value — which for a sum type spelt
// `type S = | First | ...` is the first variant (tag 0), so
// `makeSlice<S>(n)` naturally yields `[First, First, ...]`.
// Conditional on usage.
func (g *gen) writePredeclaredMakeSlice() {
	if !g.usesMakeSlice {
		return
	}
	g.b.WriteString(`func MakeSlice[T any](n int) []T { return make([]T, n) }
`)
}

// writePredeclaredScan emits the Scan helper backing the
// `fmt.scan<T>()` binding (binding-surface.md §fmt). It wraps Go's
// pointer-mutation `fmt.Scan(&v)` into Result<T, error>: a read error
// becomes Err, a successful parse becomes Ok(v). Requires the
// predeclared Result sum (usesResult, forced alongside usesScan).
func (g *gen) writePredeclaredScan() {
	if !g.usesScan {
		return
	}
	res, ok, er := g.rt("Result"), g.rt("ResultOk"), g.rt("ResultErr")
	g.b.WriteString("func Scan[T any]() " + res + "[T, error] {\n")
	g.b.WriteString("\tvar v T\n")
	g.b.WriteString("\tif _, err := fmt.Scan(&v); err != nil {\n")
	g.b.WriteString("\t\treturn " + er + "[T, error](err)\n")
	g.b.WriteString("\t}\n")
	g.b.WriteString("\treturn " + ok + "[T, error](v)\n")
	g.b.WriteString("}\n")
}

// writePredeclaredScan2 / writePredeclaredScan3 emit the multi-value
// stdin helpers backing `fmt.scan2<A,B>()` / `fmt.scan3<A,B,C>()`
// (binding-surface.md §fmt). Each wraps one `fmt.Scan(&a, &b, …)` of N
// pointers into Result<(A, B[, C]), error>, the tuple lowered to the
// anonymous `struct { _0 A; _1 B[; _2 C] }` codegen spells everywhere
// (matching goTypeFromSema), so the Ok payload destructures through the
// normal tuple-in-variant-payload match path. Conditional on usage;
// pulls in Result.
func (g *gen) writePredeclaredScan2() {
	if !g.usesScan2 {
		return
	}
	res, ok, er := g.rt("Result"), g.rt("ResultOk"), g.rt("ResultErr")
	tup := "struct { _0 A; _1 B }"
	g.b.WriteString("func Scan2[A any, B any]() " + res + "[" + tup + ", error] {\n")
	g.b.WriteString("\tvar a A\n\tvar b B\n")
	g.b.WriteString("\tif _, err := fmt.Scan(&a, &b); err != nil {\n")
	g.b.WriteString("\t\treturn " + er + "[" + tup + ", error](err)\n")
	g.b.WriteString("\t}\n")
	g.b.WriteString("\treturn " + ok + "[" + tup + ", error](" + tup + "{a, b})\n")
	g.b.WriteString("}\n")
}

func (g *gen) writePredeclaredScan3() {
	if !g.usesScan3 {
		return
	}
	res, ok, er := g.rt("Result"), g.rt("ResultOk"), g.rt("ResultErr")
	tup := "struct { _0 A; _1 B; _2 C }"
	g.b.WriteString("func Scan3[A any, B any, C any]() " + res + "[" + tup + ", error] {\n")
	g.b.WriteString("\tvar a A\n\tvar b B\n\tvar c C\n")
	g.b.WriteString("\tif _, err := fmt.Scan(&a, &b, &c); err != nil {\n")
	g.b.WriteString("\t\treturn " + er + "[" + tup + ", error](err)\n")
	g.b.WriteString("\t}\n")
	g.b.WriteString("\treturn " + ok + "[" + tup + ", error](" + tup + "{a, b, c})\n")
	g.b.WriteString("}\n")
}

// writePredeclaredResultOf emits the ResultOf helper backing the
// `(T, error)` → Result<T, error> stdlib bindings (bindings.go —
// `strconv.atoi`, `os.readFile`, …). A non-nil error becomes Err, a
// successful value becomes Ok. Requires the predeclared Result sum
// (usesResult, forced alongside usesResultOf). Conditional on usage.
func (g *gen) writePredeclaredResultOf() {
	if !g.usesResultOf {
		return
	}
	g.b.WriteString(`func ResultOf[T any](v T, err error) Result[T, error] {
	if err != nil {
		return ResultErr[T, error](err)
	}
	return ResultOk[T, error](v)
}
`)
}

// writePredeclaredResultUnit emits the ResultUnit helper backing
// the bare-`error` → Result<unit, error> boundary lift for extern
// referents that return only an `error` (`os.Chdir`, `os.WriteFile`,
// …). `unit` lowers to Go's zero-byte struct{} (lowering-go.md
// §ForeignCall). Requires the predeclared Result sum (usesResult,
// forced alongside usesResultUnit). Conditional on usage.
func (g *gen) writePredeclaredResultUnit() {
	if !g.usesResultUnit {
		return
	}
	g.b.WriteString(`func ResultUnit(err error) Result[struct{}, error] {
	if err != nil {
		return ResultErr[struct{}, error](err)
	}
	return ResultOk[struct{}, error](struct{}{})
}
`)
}

// writePredeclaredSortSorted emits the Sorted helper backing
// `sort.sorted(s, less)` (binding-surface.md §sort): a comparator sort
// that returns a NEW slice (Aril preserves the input's immutability),
// built on Go's sort.SliceStable for a stable order. Conditional on use.
func (g *gen) writePredeclaredSortSorted() {
	if !g.usesSortSorted {
		return
	}
	g.b.WriteString(`func Sorted[T any](s []T, less func(T, T) bool) []T {
	out := make([]T, len(s))
	copy(out, s)
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}
`)
}

// writePredeclaredTryRecv emits the inline helper backing
// `ch.tryRecv()` (lowering-go.md §Channel lowering): a non-blocking
// receive that returns Some(v) when a value is ready, None when the
// channel buffer is empty. Conditional on usage; pulls in Option.
func (g *gen) writePredeclaredTryRecv() {
	if !g.usesTryRecv {
		return
	}
	g.b.WriteString(`func TryRecv[T any](ch <-chan T) Option[T] {
	select {
	case v := <-ch:
		return OptionSome[T](v)
	default:
		return OptionNone[T]()
	}
}
`)
}

// writePredeclaredChanContract emits the inline channel trace-contract monitor
// (RFC-0007) — the byte-equivalent of arilrt/contract.go for inline mode.
// Conditional on usage; pulls in sync + fmt. Enforces the definitive local
// subset: double close (E1202), send after close (E1203), drain-at-boundary
// (E1207), keyed by the channel value across goroutines (mutex-guarded).
func (g *gen) writePredeclaredChanContract() {
	if !g.usesChanContract {
		return
	}
	g.b.WriteString(`type chanContractState struct {
	mu     sync.Mutex
	name   string
	closed bool
}

var chanContracts sync.Map

func RegisterChan(ch any, name string) {
	chanContracts.LoadOrStore(ch, &chanContractState{name: name})
}

func chanContractOf(ch any) *chanContractState {
	if v, ok := chanContracts.Load(ch); ok {
		return v.(*chanContractState)
	}
	return nil
}

func ChanSend[T any](ch chan T, v T, loc string) {
	if s := chanContractOf(ch); s != nil {
		s.mu.Lock()
		closed, name := s.closed, s.name
		s.mu.Unlock()
		if closed {
			panic(chanViolation("E1203", name, "send after close", loc))
		}
	}
	ch <- v
}

func ChanClose[T any](ch chan T, loc string) {
	if s := chanContractOf(ch); s != nil {
		s.mu.Lock()
		if s.closed {
			name := s.name
			s.mu.Unlock()
			panic(chanViolation("E1202", name, "double close", loc))
		}
		s.closed = true
		s.mu.Unlock()
	}
	close(ch)
}

func ChanCheckDrained(ch any, loc string) {
	if s := chanContractOf(ch); s != nil {
		s.mu.Lock()
		closed, name := s.closed, s.name
		s.mu.Unlock()
		if !closed {
			panic(chanViolation("E1207", name, "not closed before its owning boundary", loc))
		}
	}
}

func chanViolation(code, name, what, loc string) string {
	return fmt.Sprintf("aril: channel contract violated [%s] at %s: channel ` + "`" + `%s` + "`" + ` — %s", code, loc, name, what)
}
`)
}

// writePredeclaredGroup emits the inline structured-concurrency
// group helper backing `scope` / `spawn` (lowering-go.md §ScopeIR /
// §SpawnIR). It replicates errgroup.WithContext semantics with only
// `sync` + `context` (generated modules carry no external deps): the
// first spawned func to return a non-nil error stores it and cancels
// the derived context; Wait blocks for all spawns and returns that
// error. Conditional on usage.
func (g *gen) writePredeclaredGroup() {
	if !g.usesScope {
		return
	}
	g.b.WriteString(`type Group struct {
	wg     sync.WaitGroup
	once   sync.Once
	err    error
	cancel context.CancelFunc
}

func NewGroup(parent context.Context) (*Group, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	return &Group{cancel: cancel}, ctx
}

func (g *Group) Go(f func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := f(); err != nil {
			g.once.Do(func() {
				g.err = err
				g.cancel()
			})
		}
	}()
}

func (g *Group) Wait() error {
	g.wg.Wait()
	g.cancel()
	return g.err
}
`)
}

// writePredeclaredContainers emits the Go-side definitions for
// the predeclared container types Map / Set / Stack per
// `lang-spec/builtins.md` §Map / §Set / §Stack and
// `lang-spec/lowering-go.md` §Container types. Conditional —
// programs that don't reference a container emit no Go-side
// noise for it. Like writePredeclaredSums, these mirror arilrt's
// exported definitions; this is the inline single-file emission
// (vendored mode imports them from arilrt instead).
func (g *gen) writePredeclaredContainers() {
	if g.usesMap {
		g.b.WriteString(`type Map[K comparable, V any] struct {
	m     map[K]V
	order []K
}
func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{m: map[K]V{}, order: nil}
}
func (m *Map[K, V]) Len() int { return len(m.order) }
func (m *Map[K, V]) Has(k K) bool { _, ok := m.m[k]; return ok }
func (m *Map[K, V]) At(k K) V { return m.m[k] }
func (m *Map[K, V]) Get(k K) Option[V] {
	if v, ok := m.m[k]; ok {
		return Option[V]{Tag: 1, V: v}
	}
	return Option[V]{Tag: 0}
}
func (m *Map[K, V]) Set(k K, v V) {
	if _, ok := m.m[k]; !ok {
		m.order = append(m.order, k)
	}
	m.m[k] = v
}
func (m *Map[K, V]) Delete(k K) {
	if _, ok := m.m[k]; !ok {
		return
	}
	delete(m.m, k)
	for i, kk := range m.order {
		if kk == k {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}
func (m *Map[K, V]) Keys() []K {
	out := make([]K, len(m.order))
	copy(out, m.order)
	return out
}
func (m *Map[K, V]) Values() []V {
	out := make([]V, 0, len(m.order))
	for _, k := range m.order {
		out = append(out, m.m[k])
	}
	return out
}
`)
	}
	if g.usesSet {
		g.b.WriteString(`type Set[T comparable] struct {
	m     map[T]struct{}
	order []T
}
func NewSet[T comparable]() *Set[T] {
	return &Set[T]{m: map[T]struct{}{}, order: nil}
}
func SetFrom[T comparable](elems []T) *Set[T] {
	s := NewSet[T]()
	for _, e := range elems {
		s.Add(e)
	}
	return s
}
func (s *Set[T]) Len() int { return len(s.order) }
func (s *Set[T]) Has(e T) bool { _, ok := s.m[e]; return ok }
func (s *Set[T]) Add(e T) {
	if _, ok := s.m[e]; ok {
		return
	}
	s.m[e] = struct{}{}
	s.order = append(s.order, e)
}
func (s *Set[T]) Delete(e T) {
	if _, ok := s.m[e]; !ok {
		return
	}
	delete(s.m, e)
	for i, ee := range s.order {
		if ee == e {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}
func (s *Set[T]) ToSlice() []T {
	out := make([]T, len(s.order))
	copy(out, s.order)
	return out
}
`)
	}
	if g.usesStack {
		g.b.WriteString(`type Stack[T any] struct {
	xs []T
}
func NewStack[T any]() *Stack[T] {
	return &Stack[T]{xs: nil}
}
func (s *Stack[T]) Len() int { return len(s.xs) }
func (s *Stack[T]) Push(e T) {
	s.xs = append(s.xs, e)
}
func (s *Stack[T]) Pop() Result[T, error] {
	n := len(s.xs)
	if n == 0 {
		var zero T
		return Result[T, error]{Tag: 1, E: arilEmptyStack, V: zero}
	}
	v := s.xs[n-1]
	s.xs = s.xs[:n-1]
	return Result[T, error]{Tag: 0, V: v}
}
func (s *Stack[T]) Peek() Option[T] {
	n := len(s.xs)
	if n == 0 {
		return Option[T]{Tag: 0}
	}
	return Option[T]{Tag: 1, V: s.xs[n-1]}
}

var arilEmptyStack = arilEmptyStackError{}

type arilEmptyStackError struct{}

func (arilEmptyStackError) Error() string { return "empty stack" }
`)
	}
}

// predeclaredPayloadField returns the Go-side struct field name
// for a predeclared sum-type variant's payload, per spec
// (`lang-spec/lowering-go.md` §Container types). Returns the
// empty string for non-predeclared variants; callers fall back
// to the PR-F5a `<Variant><FieldName>` convention.
func predeclaredPayloadField(variantName string) string {
	switch variantName {
	case "Some", "Ok":
		return "V"
	case "Err":
		return "E"
	}
	return ""
}

// detectPredeclaredUsage walks the file AST and sets
// g.usesOption / g.usesResult when any reference is found —
// type position (NamedType), variant constructor (Ident lookup
// in g.variant), or match-arm pattern (VariantPat or
// IdentPat-bound-to-variant). Conservative: any of the four
// constructor names (Some/None/Ok/Err) flips the corresponding
// flag even if the reference is later determined to be a
// shadow. Acceptable for v1; sema will tighten.
func (g *gen) detectPredeclaredUsage(f *ast.File) {
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *ast.NamedType:
			if len(v.QName) == 1 {
				switch v.QName[0] {
				case "Option":
					g.usesOption = true
				case "Result":
					g.usesResult = true
				case "Map":
					g.usesMap = true
				case "Set":
					g.usesSet = true
				case "Stack":
					g.usesStack = true
				}
				// A handle named in a type position (a param / return /
				// field annotation) lowers to `*pkg.Sym` and so needs the
				// package imported, even when no constructor from that
				// package is called in this file (ffi.md §ForeignCall).
				if etd, isHandle := g.externType[v.QName[0]]; isHandle {
					if pkg, _ := goRefPkgSym(etd.Go, etd.Name); pkg != "" {
						g.externPkgs[pkg] = true
					}
				}
			}
			for _, a := range v.Args {
				walk(a)
			}
		case *ast.Ident:
			switch v.Name {
			case "None", "Some":
				g.usesOption = true
			case "Ok", "Err":
				g.usesResult = true
			case "Map":
				g.usesMap = true
			case "Set":
				g.usesSet = true
			case "Stack":
				g.usesStack = true
			case "reflect":
				g.usesReflect = true
			case "makeSlice":
				g.usesMakeSlice = true
			}
		case *ast.VariantPat:
			if len(v.QName) > 0 {
				switch v.QName[len(v.QName)-1] {
				case "None", "Some":
					g.usesOption = true
				case "Ok", "Err":
					g.usesResult = true
				case "Primitive", "Class", "Sum", "Slice", "Function", "Unit":
					// Kind variants used in a match arm. The
					// match subject must be a reflect.kind() call
					// for sema; here we conservatively flag
					// usesReflect so the predeclared Kind variant
					// table is populated.
					g.usesReflect = true
				}
			}
			for _, s := range v.Sub {
				walk(s)
			}
		case *ast.IdentPat:
			switch v.Name {
			case "None", "Some":
				g.usesOption = true
			case "Ok", "Err":
				g.usesResult = true
			}
		case *ast.FuncDecl:
			for _, p := range v.Params {
				walk(p)
			}
			walk(v.ReturnType)
			walk(v.Body)
		case *ast.ClassDecl:
			for _, fd := range v.Fields {
				walk(fd)
			}
			for _, m := range v.Methods {
				walk(m)
			}
		case *ast.InterfaceDecl:
			for _, e := range v.Extends {
				walk(e)
			}
			for _, m := range v.Methods {
				for _, prm := range m.Params {
					walk(prm.DeclType)
				}
				walk(m.ReturnType)
			}
		case *ast.ClassField:
			walk(v.DeclType)
		case *ast.Method:
			for _, p := range v.Params {
				walk(p)
			}
			walk(v.ReturnType)
			walk(v.Body)
		case *ast.Param:
			walk(v.DeclType)
		case *ast.TypeDecl:
			walk(v.Body)
		case *ast.TopLevelLet:
			walk(v.DeclType)
			walk(v.Value)
		case *ast.AliasBody:
			walk(v.Aliased)
		case *ast.SumTypeBody:
			for _, vr := range v.Variants {
				for _, fd := range vr.Fields {
					walk(fd)
				}
			}
		case *ast.RecordTypeBody:
			for _, fd := range v.Fields {
				walk(fd)
			}
		case *ast.FieldDecl:
			walk(v.DeclType)
		case *ast.Block:
			for _, s := range v.Stmts {
				walk(s)
			}
			if v.Trailing != nil {
				walk(v.Trailing)
			}
		case *ast.ExprStmt:
			walk(v.Expr)
		case *ast.LetStmt:
			walk(v.Pattern)
			walk(v.DeclType)
			walk(v.Value)
		case *ast.VarStmt:
			walk(v.DeclType)
			walk(v.Value)
		case *ast.AssignStmt:
			walk(v.LValue)
			walk(v.Value)
		case *ast.IfStmt:
			walk(v.Cond)
			walk(v.ThenBlock)
			walk(v.Else)
		case *ast.IfExpr:
			walk(v.Cond)
			walk(v.ThenBlock)
			walk(v.Else)
		case *ast.ForStmt:
			walk(v.Pattern)
			walk(v.Iterable)
			walk(v.Body)
		case *ast.WhileStmt:
			walk(v.Cond)
			walk(v.Body)
		case *ast.DeferStmt:
			walk(v.Call)
		case *ast.ScopeExpr:
			// A scope evaluates to Result<T, error> and lowers onto
			// the inline group helper — pull both into the binary.
			g.usesScope = true
			g.usesResult = true
			for _, ta := range v.TypeArgs {
				walk(ta)
			}
			walk(v.Parent)
			walk(v.Body)
		case *ast.SpawnExpr:
			walk(v.Body)
		case *ast.SelectStmt:
			for _, sc := range v.Cases {
				switch cse := sc.(type) {
				case *ast.SelectRecv:
					walk(cse.Channel)
					walk(cse.Body)
				case *ast.SelectSend:
					walk(cse.Channel)
					walk(cse.Value)
					walk(cse.Body)
				case *ast.SelectDefault:
					walk(cse.Body)
				}
			}
		case *ast.ReturnExpr:
			walk(v.Value)
		case *ast.TryExpr:
			// `try e` — recurse into the wrapped expression so a
			// binding nested under it (e.g. `try strconv.atoi(s)`)
			// still registers its package import + helper usage.
			walk(v.Inner)
		case *ast.MatchExpr:
			walk(v.Subject)
			for _, arm := range v.Arms {
				walk(arm.Pattern)
				walk(arm.Body)
			}
		case *ast.Call:
			// `fmt.scan<T>()` lowers to the Scan helper, which
			// returns Result<T, error> — pull both into the binary.
			if isFmtScan(v.Callee) {
				g.usesScan = true
				g.usesResult = true
			}
			// `fmt.scan2`/`fmt.scan3` lower to the Scan2/Scan3
			// helpers, which return Result<(…), error> — pull both in.
			if n := fmtScanMultiArity(v.Callee); n == 2 {
				g.usesScan2 = true
				g.usesResult = true
			} else if n == 3 {
				g.usesScan3 = true
				g.usesResult = true
			}
			// A `(T, error)` stdlib binding (`strconv.atoi`,
			// `os.readFile`, …) lowers via the ResultOf helper,
			// which returns Result<T, error> — pull both in.
			if f, ok := v.Callee.(*ast.Field); ok {
				if recv, ok := f.Receiver.(*ast.Ident); ok {
					if _, isWrap := stdlibResultWrapOf(recv.Name, f.Name); isWrap {
						g.usesResultOf = true
						g.usesResult = true
					}
				}
			}
			// `ch.tryRecv()` lowers to the TryRecv helper, which
			// returns Option<T> — pull both into the binary. Keyed on
			// the method name (the receiver's channel kind is a sema
			// fact); a same-named user method would over-pull the
			// helper, harmless dead code.
			// `error(msg)` free constructor → errors.New(msg).
			if g.isErrorCtorCall(v) {
				g.usesErrorCtor = true
			}
			// `sort.sorted(s, less)` lowers to the inline Sorted
			// helper, which needs Go's "sort". Gated on the sema symbol
			// (as the emitCall intercept is) so a user `sort` value
			// doesn't drag in the import + helper.
			if f, ok := v.Callee.(*ast.Field); ok && f.Name == "sorted" {
				if recv, ok := f.Receiver.(*ast.Ident); ok && recv.Name == "sort" && g.isBuiltinModule(recv) {
					g.usesSortSorted = true
				}
			}
			// json.* bindings (binding-surface.md §encoding/json). Gated
			// on the sema symbol like sort.sorted. parse<T> needs the
			// JSONParse helper; serialize/serializeIndent reuse
			// ResultOf. Either marks usesJSON so the Option JSON
			// methods + encoding/json import are pulled in.
			if f, ok := v.Callee.(*ast.Field); ok {
				if recv, ok := f.Receiver.(*ast.Ident); ok && recv.Name == "json" && g.isBuiltinModule(recv) {
					switch f.Name {
					case "parse":
						g.usesJSON = true
						g.usesJSONParse = true
						g.usesResult = true
					case "serialize", "serializeIndent":
						g.usesJSON = true
						g.usesResultOf = true
						g.usesResult = true
					}
				}
			}
			if f, ok := v.Callee.(*ast.Field); ok && f.Name == "tryRecv" {
				g.usesTryRecv = true
				g.usesOption = true
			}
			// Foreign bindings (ffi.md): an extern func call pulls its
			// `@go` package into the import block, and a `Result<…>`
			// return (func or handle method) pulls the ResultOf helper.
			if id, ok := v.Callee.(*ast.Ident); ok {
				if efd, isExtern := g.externFunc[id.Name]; isExtern {
					if pkg, _ := goRefPkgSym(efd.Go, efd.Name); pkg != "" {
						g.externPkgs[pkg] = true
					}
					g.markExternLift(externResultKindOf(efd.ReturnType))
				}
			}
			if f, ok := v.Callee.(*ast.Field); ok {
				if m, isExtern := g.externMethodOf(f); isExtern {
					g.markExternLift(externResultKindOf(m.ReturnType))
				}
			}
			walk(v.Callee)
			for _, ta := range v.TypeArgs {
				walk(ta)
			}
			for _, a := range v.Args {
				walk(a)
			}
		case *ast.SpreadArg:
			// A spread arg `...e` carries its expression in Inner; the
			// pre-walk must descend so an import/helper used only inside
			// the spread is registered (else the emitted Go drops it).
			walk(v.Inner)
		case *ast.Field:
			// A `pkg.method` reference marks the Go package used —
			// unless the (pkg, method) pair lowers to a Go conversion
			// rather than a package call (strings.fromBytes →
			// string(...)), which needs no import.
			if recv, ok := v.Receiver.(*ast.Ident); ok && isStdlibNamespaceName(recv.Name) {
				// A conversion binding lowers to a Go cast (no import); a
				// runtime-helper binding (sort.sorted, fmt.scan*,
				// json.parse) lowers to an arilrt helper, so its stdlib
				// package is referenced only by the inline helper body —
				// not by main, and never in vendored mode. Both are
				// excluded from the used-package set here; the inline
				// helpers add their own stdlib needs in writeHeader.
				if !isConversionBinding(recv.Name, v.Name) && !isRuntimeHelperBinding(recv.Name, v.Name) {
					g.usedGoPkgs[recv.Name] = true
				}
			}
			walk(v.Receiver)
		case *ast.ParenExpr:
			walk(v.Inner)
		case *ast.TupleLit:
			for _, ce := range v.Components {
				walk(ce)
			}
		case *ast.TupleField:
			walk(v.Receiver)
		case *ast.BraceLit:
			walk(v.TypeName)
			for _, e := range v.Entries {
				switch en := e.(type) {
				case *ast.RecordEntry:
					walk(en.Value)
				case *ast.MapEntry:
					walk(en.Key)
					walk(en.Value)
				case *ast.SetEntry:
					walk(en.Value)
				}
			}
		case *ast.Binary:
			walk(v.Left)
			walk(v.Right)
		case *ast.Unary:
			walk(v.Operand)
		case *ast.Index:
			walk(v.Receiver)
			walk(v.Idx)
		case *ast.Slice:
			walk(v.Receiver)
			walk(v.Low)
			walk(v.High)
		case *ast.SliceLit:
			walk(v.ElemType)
			for _, it := range v.Items {
				walk(it)
			}
		case *ast.SliceType:
			walk(v.Elem)
		case *ast.TupleType:
			for _, ct := range v.Components {
				walk(ct)
			}
		case *ast.FuncType:
			for _, pt := range v.Params {
				walk(pt)
			}
			walk(v.ReturnType)
		case *ast.ClosureLit:
			for _, prm := range v.Params {
				walk(prm.DeclType)
			}
			walk(v.ReturnType)
			walk(v.Body)
		case *ast.RangeExpr:
			walk(v.Low)
			walk(v.High)
		}
	}
	for _, d := range f.Decls {
		walk(d)
	}
}
