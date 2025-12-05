# Dependency Update Documentation

## Summary
Updated all Go dependencies to their latest versions and upgraded the Go version from 1.17 to 1.23.

## Changes Made

### Go Version
- **Before:** Go 1.17
- **After:** Go 1.23

### Dependencies Updated

1. **pg_query_go** (Major upgrade)
   - **Before:** github.com/pganalyze/pg_query_go/v2 v2.2.0
   - **After:** github.com/pganalyze/pg_query_go/v5 v5.1.0
   - **Breaking Changes:** Upgraded from v2 to v5, which required code changes to handle API differences

2. **google.golang.org/protobuf**
   - **Before:** v1.28.1
   - **After:** v1.36.10

3. **github.com/golang/protobuf**
   - **Before:** v1.5.2 (indirect)
   - **After:** v1.5.4 (indirect)

## Code Changes Required for pg_query_go v5

The upgrade from pg_query_go v2 to v5 introduced several breaking changes that required code modifications:

### 1. String Field Access
- **v2:** `n.String_.Str`
- **v5:** `n.String_.Sval`
- **Files affected:** `printer/printer.go`

### 2. A_Const Value Handling
In v5, the `Val` field of `A_Const` changed from a `*Node` to a oneof interface type. This required implementing a type switch to handle different value types:

```go
// v2 approach
output.writeNode(n.AConst.Val)

// v5 approach
switch v := n.AConst.Val.(type) {
case *pg_query.A_Const_Ival:
    output.writeNode(&pg_query.Node{Node: &pg_query.Node_Integer{Integer: v.Ival}})
case *pg_query.A_Const_Fval:
    output.Builder.WriteString(v.Fval.Fval)
// ... other cases
}
```

### 3. Null Value Checking
- **v2:** `stmt.LimitCount.GetAConst().GetVal().GetNull()`
- **v5:** `stmt.LimitCount.GetAConst().GetIsnull()`

### 4. Integer Value Access
- **v2:** `stmt.Typmods[0].GetAConst().GetVal().GetInteger().GetIval()`
- **v5:** `stmt.Typmods[0].GetAConst().GetIval().GetIval()`

### 5. Removed Enum Values
The following `A_Expr_Kind` enum values were removed in v5:
- `AEXPR_OF`
- `AEXPR_PAREN`

## Testing
All tests pass successfully after the update:
```
go test -v ./...
=== RUN   TestPrintFixtures
--- PASS: TestPrintFixtures (0.08s)
PASS
ok      github.com/middle-management/pgfmt     0.096s
```

## Build Verification
The project builds successfully with all updated dependencies:
```
go build
```

## Compatibility
- Minimum Go version: 1.23
- All existing functionality preserved
- Tests continue to pass
