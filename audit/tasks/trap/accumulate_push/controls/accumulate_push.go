//go:build ignore

package main

import "fmt"

func main() {
	var xs []int
	for i := 1; i <= 5; i++ {
		xs = append(xs, i*i)
	}
	for _, x := range xs {
		fmt.Println(x)
	}
}
