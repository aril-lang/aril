//go:build ignore

// Go has no contract construct — this control implements the function only;
// the contract is the Aril-specific delta the task measures (methodology §5).
package main

import "fmt"

func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func main() {
	fmt.Println(clamp(5, 0, 10))
	fmt.Println(clamp(-3, 0, 10))
	fmt.Println(clamp(15, 0, 10))
}
