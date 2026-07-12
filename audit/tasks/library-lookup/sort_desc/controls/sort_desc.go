//go:build ignore

package main

import (
	"fmt"
	"sort"
)

func main() {
	xs := []int{3, 1, 4, 1, 5, 9, 2, 6}
	sort.Sort(sort.Reverse(sort.IntSlice(xs)))
	for _, x := range xs {
		fmt.Println(x)
	}
}
