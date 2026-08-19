---
title: Decorator syntax
---

[← back to index](index.html)

## Decorator syntax (`@decorator`) {#decorators}

**Landed:** `71aa26f4bd` — 2026-08-18 (initial fork commit)

Python-style decorators on function declarations, stackable, with
optional arguments:

```go
@timed
@logged("greet")
@repeat(3)
func greet(name string) {
    fmt.Println("hello", name)
}
```

### Implementation

Rewritten pre-typecheck in `noder.rewriteDecorators`
(`src/cmd/compile/internal/noder/noder.go`) into true nested closure
wrapping — no runtime magic, just source-to-source desugaring into
ordinary Go before the rest of the compiler ever sees it. Decorators
apply innermost-first, matching Python semantics.

Decorator signature:

- no-arg: `func(func()) func()`
- parameterized (curried): `func(...) func(func()) func()`

Because this is a pure pre-typecheck syntax rewrite, it needs no changes
to `types2`, `walk`, or the IR — by the time typecheck runs, the source
already looks like ordinary nested closures.

### Known limitations

Silently no-ops on methods (functions with a receiver) rather than
erroring — a rough edge worth fixing before this is relied on broadly.

### See also

`test_decorator_syntax.go` in the `gpp` wrapper repo for the full
working set (`@timed`, `@logged`, `@cached`, `@repeat`, `@retried`,
`@throttled`, and stacked combinations).
