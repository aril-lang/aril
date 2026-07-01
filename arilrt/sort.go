package arilrt

import (
	"cmp"
	"sort"
)

// sort.go — Sorted backs sort.sorted(s, less) (binding-surface.md
// §sort): a comparator sort returning a NEW slice (Aril preserves the
// input's immutability), stable via sort.SliceStable.
func Sorted[T any](s []T, less func(T, T) bool) []T {
	out := make([]T, len(s))
	copy(out, s)
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// SortedBy backs sort.sortedBy(s, key) (binding-surface.md §sort): a stable sort
// by an Ordered key extractor, returning a NEW slice.
func SortedBy[T any, K cmp.Ordered](s []T, key func(T) K) []T {
	out := make([]T, len(s))
	copy(out, s)
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out
}

// SlicesDedup backs slices.dedup(xs): a NEW slice with duplicates removed,
// preserving first-occurrence order (full dedup, not adjacent-only). T must be
// comparable. binding-surface.md §slices.
func SlicesDedup[T comparable](xs []T) []T {
	seen := make(map[T]bool, len(xs))
	out := make([]T, 0, len(xs))
	for _, v := range xs {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
