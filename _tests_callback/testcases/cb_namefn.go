// +build ignore

package main

import (
	"fmt"
	"github.tamu.edu/April1989/go_tools/_tests_callback/lib"
)

func Square(num int) int {// @pointsto num@main.Square=t5@main.main
	return num * num
}

func main() {
	alist := []int{4, 5, 6, 7}
	result := lib.Mapper(Square, alist)
	fmt.Println(result)
}
