package sema

// SymKind classifies what a Symbol stands for. See docs/internals/sema.md §1.
type SymKind uint8

const (
	SymInvalid SymKind = iota
	SymBuiltinType
	SymBuiltinFunc
	SymBuiltinVariant
	SymBuiltinModule
	SymTypeDecl
	SymClass
	SymInterface
	SymFunc
	SymUserVariant
	SymTypeParam // generic type parameter (T in func f<T>(...))
	SymLocal
	SymTopLevelLet // module-level `let` constant (name-resolution.md §File scope)
	SymField       // class field accessible via implicit receiver
	SymMethod      // class method accessible via implicit receiver
	SymExternType  // opaque foreign handle (extern type T) — ffi.md
	SymExternFunc  // package-level foreign function (extern func) — ffi.md
)

// Symbol is the resolution result attached to every name-position node.
type Symbol struct {
	Name string
	Kind SymKind
	Type Type // Unknown until Sema-2 fills it
	Decl any  // *ast.TypeDecl / *ast.ClassDecl / *ast.FuncDecl / *ast.LetStmt / *ast.Param / *ast.Variant / nil
	// Used: a value reference resolved to this binding. Drives the
	// bind-and-ignore guard (lowering-go.md §MatchIR). A reference inside
	// a contract predicate (loop `invariant`, `requires`/`ensures`) does
	// NOT set Used — that predicate is elided under --contracts=off, so
	// the guard must still fire — it sets UsedInContract instead.
	Used bool
	// UsedInContract: a reference resolved to this binding from within a
	// contract predicate. Kept apart from Used so the codegen bind-and-
	// ignore guard is unaffected, while the unused-local check (E0221)
	// still counts a binding a contract mentions as referenced.
	UsedInContract bool
}

// Scope is one frame in the lexical-scope chain.
type Scope struct {
	parent *Scope
	names  map[string]*Symbol
}

func newScope(parent *Scope) *Scope {
	return &Scope{parent: parent, names: map[string]*Symbol{}}
}

// declare adds sym; returns the prior occupant for duplicate-decl reporting.
func (s *Scope) declare(sym *Symbol) *Symbol {
	prev := s.names[sym.Name]
	s.names[sym.Name] = sym
	return prev
}

// lookup walks the scope chain.
func (s *Scope) lookup(name string) *Symbol {
	for f := s; f != nil; f = f.parent {
		if sym, ok := f.names[name]; ok {
			return sym
		}
	}
	return nil
}
