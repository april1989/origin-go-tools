# Go Tools -> Go Pointer Analysis 

Git clone from https://github.com/golang/tools

Start from commit 146a0deefdd11b942db7520f68c117335329271a

## Current Problems
1. api.Result.Queries: seems like we cannot query after the pointer analysis, it requires the input before running pointer analysis, not useful -> dump all result out
2. callgraph.Graph: Node does not contain context info, not useful -> add it on

## kCFA
Stable version: ```v3```

### Main Changes
- Create k-callsite-sensitive contexts for static/invoke calls
- Generate constraints/cgnode online for invoke calls and targets when it is necessary
- Currently, skip the creation of reflection and dynamic calls due to the huge number

## Origin-sensitive
Stable version: ```v4```

### What is Origin? 
We treat a go routine instruction as an origin entry point, and all variables/function calls inside this go rountine share the same context as their belonging go routine.

### Regarding the Code/SSA IR
For origin-sensitive in Go, we have two cases (the following are IR instructions):
- Case 1: no ```make closure```, Go directly invokes a static function: e.g., 

```go Producer(t0, t1, t3)```

we create a new origin context for it.

- Case 2: a go routine requires a ```make closure```: e.g., 

```t37 = make closure (*B).RunParallel$1 [t35, t29, t6, t0, t1] ```

```go t37() ``` 

the make closure has been treated with origin-sensitive and its origin context has been created earlier, here we find the çreated obj and use its context to use here.
