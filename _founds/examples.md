### Grpc:

#### Example 1.  pts(n123924 : interface{}) with 1119 objects

This pointer is an empty interface that may hold values of any type (https://tour.golang.org/methods/14).
It is created in main entry: google.golang.org/grpc/examples/features/multiplex/client,
and the corresponding IR, pointers and constraints are:
```go
Generating constraints for cg16751:(*fmt.pp).doPrintf@[0:shared contour; ], shared contour
    ...
	create n123924 interface{} for t86
	val[t86] = n123924  (*ssa.UnOp)
	...
; t86 = *t85
	load n123924 <- n123923[0]
; t87 = convert rune <- uint8 (t22)
; t88 = (*pp).printArg(p, t86, t87)
	---- makeFunctionObject (*fmt.pp).printArg
	create n124145 func(arg interface{}, verb rune) for func.cgnode
	create n124146 *fmt.pp for func.recv
	create n124147 interface{} for func.params#0
	create n124148 rune for func.params#1
	----
```

t86 (n123924) is used as a parameter of function call to (*pp).printArg(), where p is the receiver,
t86, t87 are actual parameters.

The source code of (*fmt.pp).printArg() is at https://github.com/golang/go/blob/d2fd503f687ca686cb8fbee0b29e64ba529038fe/src/fmt/print.go#L634,
I only paste the key part here:
```go
func (p *pp) printArg(arg interface{}, verb rune) {
	p.arg = arg
	p.value = reflect.Value{}
    ...
```
The arg here corresponds to t86 in the IR. This print function is a super heavy used function in almost all programs,
not directly called in program code but called by public methods from the same package, such as:
- fmt.Fprintf()
- fmt.Sprintf()
- fmt.Fprint()
- fmt.Sprint()
- fmt.Fprintln()
- fmt.Sprintln()

(for the source code of the above functions: https://github.com/golang/go/blob/d2fd503f687ca686cb8fbee0b29e64ba529038fe/src/fmt/print.go)

One usage of such public print functions is to trigger panics or errors. There are 273 .go files in grpc
imported package fmt. One thing to mention is the logger of grpc (/grpc-go/grpclog/glogger/glogger.go), which
uses the above public fmt functions a lot.

Except for this parameter pointer in (*fmt.pp).printArg(), there are other print functions with also requires such
an empty interface type parameter(s) that contain(s) a large points-to set. They are on the call chain between
the above public print functions and (*fmt.pp).printArg(), which are also heavily used in programs, such as:

1. (*fmt.pp).doPrintf() with call chain:
    fmt.Fprintf() -> (*fmt.pp).doPrintf() -> (*fmt.pp).printArg()
2. (*fmt.pp).intFromArg() with call chain:
    fmt.Fprintf() -> (*fmt.pp).doPrintf() -> (*fmt.pp).intFromArg()
3. (*fmt.pp).handleMethods() with call chain:
    fmt.Fprintf() -> (*fmt.pp).doPrintf() -> (*fmt.pp).printArg() -> (*fmt.pp).handleMethods()
4. (*fmt.pp).catchPanic() with call chain:
    fmt.Fprintf() -> (*fmt.pp).doPrintf() -> (*fmt.pp).printArg() -> (*fmt.pp).handleMethods() -> (*fmt.pp).catchPanic()

Most of the pointers with pts > 1000 in grpc have the similar condition with Example 1: those pointers
are from the print functions in package fmt.



#### Example 2.  pts(n14383 : interface{}) with 1224 objects

It is created in main entry: google.golang.org/grpc/examples/features/multiplex/client,
and the corresponding IR, pointers and constraints are:
```go
Generating constraints for cg768:net/http.init@[0:shared contour; ], shared contour
...
; t548 = reflect.TypeOf(t547)
	---- makeFunctionObject reflect.TypeOf
	create n14382 func(i interface{}) reflect.Type for func.cgnode
	create n14383 interface{} for func.params
	create n14384 reflect.Type for func.results
	----
...
```
I turned off reflection when analyzing main entries, so no constraints are created when encountering
function reflect.TypeOf(). Its IR is shown below:
```go
Generating constraints for cg53397:reflect.TypeOf@[0:shared contour; ], shared contour
# Name: reflect.TypeOf
# Package: reflect
# Location: /usr/local/go15/src/reflect/type.go:1366:6
func TypeOf(i interface{}) Type:
	(external)
```
Seems like the turn-off-reflection is not a fully turn-off, since parameter and return value constraints are
still created for reflection function calls. Will update the code to fully turn-off.


