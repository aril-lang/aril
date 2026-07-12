//go:build ignore

// Go has no contract construct — this control implements the function only;
// the contract is the Aril-specific delta the task measures (methodology §5).
package main

import "fmt"

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func main() {
	fmt.Println(abs(-5))
	fmt.Println(abs(3))
	fmt.Println(abs(0))
}
