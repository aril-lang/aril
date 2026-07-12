//go:build ignore

package main

import (
	"fmt"
	"strconv"
	"strings"
)

func main() {
	total := 0
	for _, p := range strings.Split("10 20 30 40", " ") {
		n, _ := strconv.Atoi(p)
		total += n
	}
	fmt.Println(total)
}
