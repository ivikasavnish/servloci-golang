## Nil-safe selector (`?.`) {#nil-safe-selector}

**Landed:** `71aa26f4bd` — 2026-08-18 (initial fork commit)

Kotlin/JS-style nil-safe chaining — not Go's usual `(value, ok)` idiom:

```go
var user *User
city := user?.Address?.City   // "" — no panic, not a comma-ok tuple
```

`a?.b?.c` evaluates to the zero value of the final field's type if any
pointer/interface link in the chain is nil, otherwise the real value.

### Implementation

Unlike decorators, this can't be a pre-typecheck rewrite — synthesizing
the correct zero value for `a?.b?.c` needs to know the real type of the
final field, which isn't available until after typecheck. So it's
lowered post-typecheck in `walk.walkNilSafeDot`
(`src/cmd/compile/internal/walk/expr.go`), once real `*types.Type` info
is available, into a temp + nil-guard:

```go
tmp := zero(ResultType)
if X != nil {
    tmp = X.Sel
}
```

The parser (`src/cmd/compile/internal/syntax/parser.go`,
`scanner.go`, `tokens.go`) recognizes `?.` as a token and sets a new
`NilSafe bool` field on `syntax.SelectorExpr`
(`src/cmd/compile/internal/syntax/nodes.go`); that flag rides through
`types2` unchanged (a `?.` selector is checked exactly like `.` — same
field/method lookup, `?.` only changes what happens if the base is nil)
and through to `ir.SelectorExpr.NilSafe`
(`src/cmd/compile/internal/ir/expr.go`), where `walk` reads it.

Composes correctly for chained selectors (each link's guard collapses to
a temp before the next link sees it), evaluates the base expression
exactly once even with side effects (rides the compiler's existing
order-of-evaluation pass), and is restricted to pointer/interface base
types — `?.` on a plain value type is a compile error, not silently
ignored.

### See also

`test_nil_safety.go` and `NIL_SAFE_OPERATOR_STATUS.md` in the `gpp`
wrapper repo.

This is the reference pattern later features that need real type info
follow — see [map dot-access sugar](06-map-dot-access.md), which needed
the *types2*-level version of this same "flag the node, handle it
downstream" approach because, unlike `?.`, its lookup genuinely fails at
the types2 level and needs a different resolution, not just a
different runtime guard.
