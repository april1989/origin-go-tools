// +build ignore

package main

import (
	"fmt"
	"github.tamu.edu/April1989/go_tools/_tests_callback/lib"
	rand2 "math/rand"
)


func getCallBack(b *lib.Wrapper) func() {// @pointsto b@getCallBack=t2@main
	return func() { fmt.Println(b.B == true) }
}

func main() {
	var abort *lib.Wrapper
	rand := rand2.Int()
	if rand > 1 {
		abort = &lib.Wrapper{ B: true }
	} else {
		abort = &lib.Wrapper{ B: false}
	}
	f := getCallBack(abort)
	lib.Level1(f, abort) //pass as a pointer
}
