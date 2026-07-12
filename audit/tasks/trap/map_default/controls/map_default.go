//go:build ignore

package main

import (
	"fmt"
	"strings"
)

func main() {
	counts := map[string]int{}
	for _, w := range strings.Split("a b a c b a", " ") {
		counts[w]++
	}
	fmt.Println(counts["a"])
	fmt.Println(counts["b"])
	fmt.Println(counts["c"])
	fmt.Println(counts["z"])
}
