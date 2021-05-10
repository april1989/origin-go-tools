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

Check console-ethereum-dist-cb-lmt10.txt: we have the following founds: 

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


**Update for go-ethereum 2:**

Config 1. github.com/ethereum/go-ethereum/cmd/geth  (No PTSLimit, use 59m1.704662239s)
(#total:  10386 , #compiled:  559 , #analyzed:  5553 , #analyzed$:  586 , #others:  258 )

Config 2. github.com/ethereum/go-ethereum/cmd/geth  (PTSLimit = 10, use 12m46.374579834s)  
(#total:  10386 , #compiled:  559 , #analyzed:  5553 , #analyzed$:  586 , #others:  258 )

Config 3. github.com/ethereum/go-ethereum/cmd/geth  (PTSLimit = 10, exclude fmt and error pkg, use 9m31.996348357s)  
(#total:  8755 , #compiled:  559 , #analyzed:  5545 , #analyzed$:  585 , #others:  105 )


**Update for go-ethereum 3:**

Previously, we handle pts limit like this: we copy the points-to set change then check its size, in order to keep 
most of points-to sets of receiver pointers, like shown below: 

```go
n.solve.prevPTS.Copy(&n.solve.pts.Sparse) //bz: copy then check
if n.solve.pts.Len() >= ptsLimit { //bz: check ptsLimit here
	skipIDs[x] = x
}
```

This config requires 12m46.374579834s (Config 2) or 9m31.996348357s (Config 3). 

Now we check the size of points-to set, then decided if we copy them or not, like shown below: 

```go
if n.solve.pts.Len() >= ptsLimit { //bz: check then copy
	skipIDs[x] = x
	n.solve.prevPTS.Clear()
	continue
}else {
	n.solve.prevPTS.Copy(&n.solve.pts.Sparse)
}
```

And its result is:
 
Config 4. github.com/ethereum/go-ethereum/cmd/geth  (PTSLimit = 10, exclude fmt and error pkg, check then copy, use 45.261569861s)  
(#total:  8755 , #compiled:  559 , #analyzed:  5553 , #analyzed$:  586 , #others:  105 )


### Questions Now

#### Imprecise Call Graph Due to Missing Call Edges
Due to the algorithm of callback, we are not missing targets. However, the decreased runtime of pointer analysis from different configs 
significantly decreases the number of call edges:

*#call edge: 
835652 (Config 1: no pts limit) 
-> 504519 (Config 2: limit pts to 10, copy then check) 
-> 502706 (Config 3: exclude pkgs) 
-> 112221 (Config 4: check then copy)*

We are missing targets, but what are they? Where do the missing targets come from? Are they over-approximate or real targets? Can we ignore them? 
Ignore to which extend (which config is acceptable)?

###### What are missing targets?
One type of missing target is from the init function of main entry, e.g., 
```go
github.com/ethereum/go-ethereum/cmd/geth.init
```
which includes a set of function calls to other init functions, e.g., 

```go
Generating constraints for cg218:github.com/ethereum/go-ethereum/cmd/geth.init@[0:shared contour; ], shared contour
# Synthetic: package initializer
func init():
0:                                                                entry P:0 S:2
	t0 = *init$guard                                                   bool
	if t0 goto 2 else 1
1:                                                           init.start P:1 S:1
	*init$guard = true:bool
	t1 = fmt.init()                                                      ()
	t2 = io/ioutil.init()                                                ()
	t3 = github.com/ethereum/go-ethereum/accounts.init()                 ()
	t4 = github.com/ethereum/go-ethereum/accounts/keystore.init()        ()
	t5 = github.com/ethereum/go-ethereum/cmd/utils.init()                ()
	t6 = github.com/ethereum/go-ethereum/crypto.init()                   ()
	t7 = github.com/ethereum/go-ethereum/log.init()                      ()
	t8 = gopkg.in/urfave/cli.v1.init()                                   ()
	t9 = encoding/json.init()                                            ()
	t10 = os.init()                                                      ()
	t11 = runtime.init()                                                 ()
	t12 = strconv.init()                                                 ()
	t13 = sync/atomic.init()                                             ()
	t14 = time.init()                                                    ()
    ...
```

However, these init function calls can reach some functions that are ONLY reachable from those init functions, 
but not reachable from app functions starting from main entry, e.g., the target ```github.com/ethereum/go-ethereum/metrics.NewRegistry``` below  

```go
Generating constraints for cg3805:github.com/ethereum/go-ethereum/metrics.init@[0:shared contour; ], shared contour
# Synthetic: package initializer
func init():
  ...
; t45 = NewRegistry()
	---- makeFunctionObjectWithContext (kcfa) github.com/ethereum/go-ethereum/metrics.NewRegistry
     K-CALLSITE -- [0:shared contour; ]
	create n11039 func() github.com/ethereum/go-ethereum/metrics.Registry for func.cgnode
	create n11040 github.com/ethereum/go-ethereum/metrics.Registry for func.results
	----
	NewRegistry()@github.com/ethereum/go-ethereum/metrics.init -> cg11039:github.com/ethereum/go-ethereum/metrics.NewRegistry@[0:shared contour; ]
	copy n10995 <- n11040
	NewRegistry()@github.com/ethereum/go-ethereum/metrics.init to targets n0 from cg3805:github.com/ethereum/go-ethereum/metrics.init@[0:shared contour; ]
; *DefaultRegistry = t45
  ...
```

So, how many such function (non init function that are only reachable by init functions) exist? 
do we need to analyze them? how much precision do we lose if we ignore them? 







###### Where do the missing targets come from?


###### Are they over-approximate or real targets?


###### Can we ignore them? Ignore to which extend (which config is acceptable)?   