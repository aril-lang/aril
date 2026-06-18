package arilrt

import "sort"

// sort.go — Sorted backs sort.sorted(s, less) (binding-surface.md
// §sort): a comparator sort returning a NEW slice (Aril preserves the
// input's immutability), stable via sort.SliceStable.
func Sorted[T any](s []T, less func(T, T) bool) []T {
	out := make([]T, len(s))
	copy(out, s)
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}
