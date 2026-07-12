//go:build ignore

package main

import "fmt"

type Shape interface{ area() int }

type Circle struct{ r int }

func (c Circle) area() int { return 3 * c.r * c.r }

type Rect struct{ w, h int }

func (r Rect) area() int { return r.w * r.h }

func main() {
	shapes := []Shape{Circle{2}, Rect{3, 5}}
	for _, s := range shapes {
		fmt.Println(s.area())
	}
}
