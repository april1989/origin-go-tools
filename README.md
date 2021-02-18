# Go Tools -> Go Pointer Analysis 

Git clone from https://github.com/golang/tools, start from commit 146a0deefdd11b942db7520f68c117335329271a (around v0.5.0-pre1).

The default go pointer analysis algorithm (v0.5.0-pre1) is at ```go_tools/go/pointer_default```.

For any panic, please submit an issue with copy/paste crash stack. Thanks.

## How to Use?
Go to ```go_tools/main```, and run ```go build```. Then, run ```./main``` with the following flags and 
the directory of the go project that you want to analyze.
It will go through all of your main files and analyze them one by one.

#### Flags

- *path*: default value = "", Designated project filepath. 
- *doLog*: default value = false, Do a log to record all procedure, so verbose. 
- *doCompare*: default value = false, Do comparison with default pta about performance and result.

For example,
 
```./main -doLog -doCompare ../grpc-go/benchmark/server```

This will run the origin-sensitive pointer analysis on all main files under directory ```../grpc-go/benchmark/server```,
as well as generate a full log and a comparison with the default algorithm about performance and result.

## User APIs (for detector) 
Go to https://github.tamu.edu/April1989/go_tools/main/main.go, check how to use the callgraph and queries. 

## Origin-sensitive

#### What is Origin? 
We treat a go routine instruction as an origin entry point, and all variables/function calls inside this go rountine share the same context as their belonging go routine.

#### Main Changes from Default
Instead of pre-computing all cgnodes and their constraints before actually propagating changes among points-to constraints,
we start from the reachable cgnodes ```init``` and ```main``` and gradually compute reachable cgnodes and their constraints. 

## kCFA

#### Main Changes from Default
- Create k-callsite-sensitive contexts for static/invoke calls
- Generate constraints/cgnode online for invoke calls and targets when it is necessary
- Currently, skip the creation of reflection and dynamic calls due to the huge number


## Why are the cgs from default and my pta different?

The default algorithm create cgnodes for functions that are not reachable from the main entry.
For example, when analyzing the main entry ```google.golang.org/grpc/benchmark/server```,
the default algorithm pre-generate constraints and cgnodes for function:
```go
(*google.golang.org/grpc/credentials.tlsCreds).ServerHandshake
``` 
which is not reachable from the main entry (it has no caller in cg).

This can be reflected in the analysis data: 
```
#cgnodes (totol num):  20932
#Nodes:  14401 (Call Graph)
```
it generates 20932 cgnodes and their constraints, however, only 14401 of them can be reachable from the main.

All CG DIFFs from comparing my with default result are due to this reason.


## Why the unreachable function/cgnode will be generated?

This is because the default algorithm creates nodes and constraints for all methods of all types
that are dynamically accessible via reflection or interfaces ().



