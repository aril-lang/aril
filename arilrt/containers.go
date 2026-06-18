package arilrt

// containers.go — the predeclared container types Map / Set / Stack
// (lang-spec/builtins.md §Map / §Set / §Stack, lowering-go.md §Container
// types). Insertion order is preserved for deterministic iteration.
// Methods are exported so vendored-mode code in another package can call
// them; inline single-file mode uses the same spellings without the
// package selector.

// Map is an insertion-ordered map.
type Map[K comparable, V any] struct {
	m     map[K]V
	order []K
}

// NewMap builds an empty Map.
func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{m: map[K]V{}, order: nil}
}

func (m *Map[K, V]) Len() int     { return len(m.order) }
func (m *Map[K, V]) Has(k K) bool { _, ok := m.m[k]; return ok }

// At is the raw index read backing `m[k]`: V's zero value for a missing
// key (Go map semantics). Get is the Option-returning form.
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

// Set is an insertion-ordered set.
type Set[T comparable] struct {
	m     map[T]struct{}
	order []T
}

// NewSet builds an empty Set.
func NewSet[T comparable]() *Set[T] {
	return &Set[T]{m: map[T]struct{}{}, order: nil}
}

// SetFrom builds a Set from a slice, preserving first-seen order.
func SetFrom[T comparable](elems []T) *Set[T] {
	s := NewSet[T]()
	for _, e := range elems {
		s.Add(e)
	}
	return s
}

func (s *Set[T]) Len() int     { return len(s.order) }
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

// Stack is a LIFO stack.
type Stack[T any] struct {
	xs []T
}

// NewStack builds an empty Stack.
func NewStack[T any]() *Stack[T] {
	return &Stack[T]{xs: nil}
}

func (s *Stack[T]) Len() int { return len(s.xs) }
func (s *Stack[T]) Push(e T) { s.xs = append(s.xs, e) }

func (s *Stack[T]) Pop() Result[T, error] {
	n := len(s.xs)
	if n == 0 {
		var zero T
		return Result[T, error]{Tag: 1, E: emptyStack, V: zero}
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

var emptyStack = emptyStackError{}

type emptyStackError struct{}

func (emptyStackError) Error() string { return "empty stack" }
