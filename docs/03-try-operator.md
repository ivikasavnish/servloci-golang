---
title: Error-propagating call
---

[← back to index](index.html)

## Error-propagating call (`?`, `?[i]`) {#try-operator}

**Landed:** `01b9444681` — 2026-08-19

Rust-style `?`, adapted for Go's multi-value returns:

```go
func divide(a, b int) (int, error) { ... }

func safeDivide(a, b int) (int, error) {
    q := divide(a, b)?      // q is int; on error, returns (0, err) immediately
    return q, nil
}
```

For functions returning more than one non-error value, pick which one to
keep with `?[i]`:

```go
func splitAdd(a, b int) (int, int, error) { ... }

quot := splitAdd(a, b)?[0]
rem  := splitAdd(a, b)?[1]
```

### Implementation

Rewritten pre-typecheck in `noder.rewriteTryOperator`
(`src/cmd/compile/internal/noder/noder.go`), same source-to-source
approach as decorators: `x := f()?` becomes a temp multi-assignment plus
an `if err != nil { return <zero>..., err }` spliced in before the
statement. Non-selected results are discarded to `_`. Parser support in
`src/cmd/compile/internal/syntax/parser.go` and a new node shape in
`nodes.go`.

### Scope (v1)

`Fun` in `Fun(...)?` must be a plain identifier naming a package-level
function declared in the **same file**, so its result signature is
known from the syntax tree alone, without running the typechecker. This
means it does **not** chain through method calls — `foo()?.Bar()?` only
lowers the first `?`; the second one sits on a method call whose
signature isn't known pre-typecheck, so it's left untouched and surfaces
as a normal Go compile error (`multiple-value ... in single-value
context`), never a miscompile.

### What full method-chaining support would take

Would need the operator taught to `types2` (so chained calls typecheck
against the post-`?` result type) and lowered post-typecheck in `walk`,
mirroring how [`?.`](02-nil-safe-selector.html) is done — a substantially
bigger change than this v1, not yet implemented.

### See also

`test_try_operator.go` in the `gpp` wrapper repo.
