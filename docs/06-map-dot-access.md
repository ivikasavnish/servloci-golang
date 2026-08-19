---
title: Map dot-access sugar
---

[← back to index](index.html)

## Map dot-access sugar (`m.foo`) {#map-dot-access}

**Landed:** `c02aa91156` — 2026-08-19

`m.foo` on a map with string-kind keys desugars to `m["foo"]`, both as a
read and as an assignment target:

```go
r := map[string]any{"name": "vikas"}
fmt.Println(r.name)   // "vikas"
r.city = "blr"         // same as r["city"] = "blr"
fmt.Println(r.missing) // nil -- zero value, same as normal map index
```

Real fields and methods always win: a named map type with a `Sum()`
method or a real struct field of the same name is resolved normally,
never shadowed by the sugar. Nested maps chain (`nested.user.name`), and
named string key types (`type Key string; map[Key]V`) work too.

### Why this couldn't be a pre-typecheck rewrite

Every other feature in this book ([decorators](01-decorators.html),
[`?`](03-try-operator.html), [`@codec`](04-codec-decorator.html),
[`@rpc`](05-rpc-decorator.html)) is a pure source-to-source rewrite done in
`noder`, before typecheck ever runs — cheap and safe because the
rewrite's precondition (is this a decorator, is this a same-file
function call) is decidable from syntax alone. `m.foo` doesn't have that
property: deciding whether `.foo` means "map key" or "undefined
field/method" requires knowing `m`'s real type, which only exists after
the type-checker runs — same category of problem as
[`?.`](02-nil-safe-selector.html), which is why this follows that
feature's "flag the node, resolve downstream" shape rather than a
straight source rewrite. But `?.` never has an actual lookup *failure*
to recover from (a `?.` selector must already name a real field/method;
the operator only changes what happens if the base is nil at runtime),
so it never needed to touch `types2` itself. `m.foo` does have a real
lookup failure — there's no field named `foo` on a map — so the fix has
to live inside the type-checker's failure path, not after it.

### Implementation

- `types2.(*Checker).selector` (`src/cmd/compile/internal/types2/call.go`):
  when normal field/method lookup fails and the base type's underlying
  type is `map[K]V` with `K` a string-kind type, resolves the selector
  as a `V`-typed variable instead of erroring.
- `syntax.SelectorExpr` (`src/cmd/compile/internal/syntax/nodes.go`)
  gets a new `MapDot bool` field, alongside the existing `NilSafe`, set
  by `types2` (not the parser — the parser can't know this either) to
  flag the node for the writer stage.
- `noder/writer.go` checks `expr.MapDot` before its normal
  `Selections[expr]` lookup (which would `assert(ok)`-panic, since
  there's no real `types2.Selection` for a synthesized map key) and
  instead emits a plain index expression: `exprIndex` bytecode plus a
  synthesized string constant (`exprConst`) for the key.
- `typecheck.tcDot` (`src/cmd/compile/internal/typecheck/expr.go`) got
  the same fallback added as a safety net — it's the *legacy*
  type-checker, dead for normal source now that the unified frontend
  (`types2`) handles real user code, but still exercised by the
  reparse-based features ([`@codec`](04-codec-decorator.html),
  [`@rpc`](05-rpc-decorator.html)) for their generated code text, so it's
  worth keeping in sync.

No parser or gofmt changes needed — `.` is already valid Go syntax here,
only its type-checking meaning changes. This is notably *less* invasive
than `?.`, `?`, `@codec`, or `@rpc`, none of which could say that.

### Known gap

A typo (`r.nmae`) silently returns the zero value instead of erroring,
same footgun as raw map indexing — not new, but the sugar makes it
easier to typo a field-like name by accident, since it now *looks* like
a struct field access that the compiler would normally catch. A linter
pass against known map-literal keys would be the place to catch that,
not the compiler.

### See also

`test_map_dot.go` / `test_map_dot2.go` (session scratchpad, not yet
copied into the `gpp` wrapper repo's test set the way earlier features
were — worth doing before this ships as a documented feature there).
