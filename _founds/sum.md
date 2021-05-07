### from Jeff: "im not expecting len(pts) > 1000, ... ", so let's set ptsLimit 

To estimate the size of points-to set, did a statistic survey on the mains of these benchmarks:
Check console-grpc-dist.txt, console-ethereum-dist.txt (callback), console-kubernetes-cb-dist.txt (callback):
1. 96% has <10 obj in a pts;
2. 92% has idx < 5000 as the min obj idx;
3. 90% has < 10 distance between max and min obj idx

Similar trends in other benchmarks


Specifically in grpc, the size of pts is [2500, 3000] (from console-grpc-dist.txt):
```go
Distribution:
# < 10: 96.9087162435092 %
# < 100:  2.6219196003402803 %
# < 200:  0.36921968189782906 %
# < 500:  0.08869036816747551 %
# < 700:  0.002441796657034023 %
# < 1000: 0.0006778489142555016 %
# < 1500: 0.006778489142555016 %
# < 2000: 0.001193938428518213 %
# < 2500: 0.000346627285698836 %
# < 3000: 1.540565714217049e-05 %
# < 3500: 0 %
# < 4000: 0 %
# others: 0 %
```

#### Why exists pts > 1000?
Those pointers are all interface related types: interface{}, *interface{}, []interface{}
Check console-grpc-dist-detail.txt
There are three reasons where those pointers are created:
1. function parameter is of type interface{}, *interface{}, []interface{}, or will be used as such a function parameter
   e.g., (*fmt.pp).printArg, fmt.intFromArg
2. reflect.ValueOf() and reflect.TypeOf()
3. nil check or type cast


#### Can those pointers/their points-to sets invoke app functions/propagated to app function pointers?
Theoretically, if there is no callback functions that use them, this will not happen.
But there can be return values.


#### How about 100 < pts < 1000 ?
Check console-grpc-dist-detail2.txt
Except for interface related types, those pointers are of type:
1. error: underlying type is interface{Error() string}
2. *sync.Mutex
3. fmt.Stringer: underlying type is interface{String() string}
4. *unicode.RangeTable
5. *math/big.nat, *math/big.Int
6. basic types: e.g., *int32, *uint32, *byte, []byte

Continue
- 1 error type pointers are create as return value: e.g., n7075 error for func.results
- 2 *sync.Mutex type pointers are create as function receiver: e.g., n17856 *sync.Mutex for func.recv
- 3 ~ 6 are mostly used by library functions


#### How about in Kubernetes?
Check console-kubernetes-dist-cb-detail.txt
Similar to grpc, all pts > 1000 are interface related types. One diff is:
```go
pts(n56491 : func(a interface{}, b interface{}, scope k8s.io/apimachinery/pkg/conversion.Scope) error): underlying func(a interface{}, b interface{}, scope k8s.io/apimachinery/pkg/conversion.Scope) error
```
This is a lib type:
```go
 type ConversionFunc func(a, b interface{}, scope Scope) error //from k8s.io/apimachinery/pkg/conversion/converter.go
```
It is used as a parameter passed to function, e.g.,
```go
 func (c *Converter) RegisterUntypedConversionFunc(a interface{}, b interface{}, fn ConversionFunc) error
```
which requires to create an anonymous function pointer each time.


#### How about in go-ethereum?
The on-the-fly and callback analysis cannot stop after 30min for main: so cannot compare the diff.
Check console-ethereum-dist-lmt10.txt: we still have pts > 100 even though we limit the size of pts.
1. 100 < pts < 1000 ?
Except for the ones explained above, there are some pointers of app types, e.g.,
- (0) a lot of pointers of type []string
- (1) github.com/ethereum/go-ethereum/trie.node
- (2) *github.com/ethereum/go-ethereum/core/vm.operation
- (3) *github.com/ethereum/go-ethereum/rlp.typeinfo
- (4) *github.com/ethereum/go-ethereum/rlp.listhead
- (5) func(reflect.Value, *github.com/ethereum/go-ethereum/rlp.encbuf) error)

2. pts > 1000 ?
Check console-ethereum-dist-lmt10-2.txt
The same as other benchmarks, all pointers have interface related types.


**Update for go-ethereum:**

The callback analysis can finish more than 1hour to analyze 18 main entries. 
There is no function coverage change (specific to app function) when we turn on or off the 
pts limit in callback impl, because of the callbacl algorithm (we presolve all constraints 
for invoke functions with receiver object of types declared in the app), which is proved by 
the result below.

Check console-ethereum-dist-cb.txt: we have the following founds: 

- The biggest main entry (go-ethereum/cmd/geth) uses 14min when we set the pts limit to 10 
and 59min without the limit, however, the function coverage are the same 53.46%, which means
they cover the same set of app functions. The uncovered functions are lib functions, such as:

  - (*sync.Once).Do
  - sort.Search, sort.Slice
  - time.AfterFunc
  - (*github.com/huin/goupnp.Device).VisitServices
  - expvar.Do
  
- Similar conditions appear in the comparison for other main entries, but not too much performance 
gain, since they have small code base. The uncovered functions are also similar.

- The distribution of pts shift a bit: more pointers in the group of # < 100 as shown below, this shift 
will be bigger if we remove the limit.
```go
Distribution:  
# <  10 : 89.16735545776517 % 
# < 100 :  8.958305435369846 % 
# < 200 :  1.2615266689138958 % 
# < 500 :  0.45223560811403607 % 
# < 700 :  0.06822495070718451 % 
# < 1000 : 0.09200555958312362 % 
# < 1500 : 0 % 
# < 2000 : 0 % 
# < 2500 : 0 % 
# < 3000 : 0.0003463195467369772 % 
# < 3500 : 0 % 
# < 4000 : 0 % 
# others: 0 %
```

- pts > 2000 still exist, but for interface related types. 















 


