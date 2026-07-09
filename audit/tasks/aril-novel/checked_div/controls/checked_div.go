//go:build ignore

package main

import (
	"errors"
	"fmt"
)

func safeDiv(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

func main() {
	cases := [][2]int{{10, 2}, {7, 0}, {9, 3}}
	for _, c := range cases {
		q, err := safeDiv(c[0], c[1])
		if err != nil {
			fmt.Println("err", err)
		} else {
			fmt.Println("ok", q)
		}
	}
}
