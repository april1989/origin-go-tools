package main

type I interface {
	f()
}

type C int

func (C) f() {}

type D int

func (D) f() {}

func main() {
	var i I = C(0)
	i.f() // dynamic call; f()@main()

	main2() //main2()@main()
}

func main2() { //main2()@main()
	var i I = D(0)
	i.f() // dynamic call; f()@main2(),main2()@main()
}
