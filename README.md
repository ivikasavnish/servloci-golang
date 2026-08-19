# servloci-golang

A custom fork of the Go compiler (go1.27-devel) adding two language
extensions on top of upstream Go. Everything below the next section is
stock upstream Go documentation.

## Additions

### Decorator syntax (`@decorator`)

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

Rewritten pre-typecheck (`noder.rewriteDecorators`) into true nested
closure wrapping — no runtime magic, just source-to-source desugaring
into ordinary Go before the rest of the compiler ever sees it. Decorators
apply innermost-first, matching Python semantics. Signature:
`func(func()) func()` for no-arg decorators, curried
`func(...) func(func()) func()` for parameterized ones. See
`test_decorator_syntax.go` for the full working set (`@timed`,
`@logged`, `@cached`, `@repeat`, `@retried`, `@throttled`, and stacked
combinations).

Known limitation: silently no-ops on methods (functions with a receiver)
rather than erroring.

### Nil-safe selector (`?.`)

Kotlin/JS-style nil-safe chaining — not Go's usual `(value, ok)` idiom:

```go
var user *User
city := user?.Address?.City   // "" — no panic, not a comma-ok tuple
```

`a?.b?.c` evaluates to the zero value of the final field's type if any
pointer/interface link in the chain is nil, otherwise the real value.
Lowered post-typecheck in `walk.walkDot`, once real `*types.Type` info is
available, into a temp + nil-guard:

```go
tmp := zero(ResultType)
if X != nil {
    tmp = X.Sel
}
```

Composes correctly for chained selectors (each link's guard collapses to
a temp before the next link sees it), evaluates the base expression
exactly once even with side effects (rides the compiler's existing
order-of-evaluation pass), and is restricted to pointer/interface base
types — `?.` on a plain value type is a compile error, not silently
ignored. See `test_nil_safety.go` and `NIL_SAFE_OPERATOR_STATUS.md`.

### Error-propagating call (`?`, `?[i]`)

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

Rewritten pre-typecheck (`noder.rewriteTryOperator`), same source-to-source
approach as decorators: `x := f()?` becomes a temp multi-assignment plus an
`if err != nil { return <zero>..., err }` spliced in before the statement.
Non-selected results are discarded to `_`.

**Scope (v1):** `Fun` in `Fun(...)?` must be a plain identifier naming a
package-level function declared in the *same file*, so its result
signature is known from the syntax tree alone, without running the
typechecker. This means it does **not** chain through method calls —
`foo()?.Bar()?` only lowers the first `?`; the second one sits on a method
call whose signature isn't known pre-typecheck, so it's left untouched and
surfaces as a normal Go compile error (`multiple-value ... in single-value
context`), never a miscompile. Full method-chaining support would need the
operator taught to `types2` (so chained calls typecheck against the
post-`?` result type) and lowered post-typecheck in `walk`, mirroring how
`?.` is done — a substantially bigger change, not yet implemented. See
`test_try_operator.go`.

### Format-agnostic serialization (`@serde`)

Rust-serde-style codegen, adapted to Go: one generated method pair per
struct, decoupled from any specific wire format via two small interfaces
(`Encoder`/`Decoder`) that the user's package must declare (same
same-package convention as `@timed`/`@logged`):

```go
@serde
type User struct {
    Name string
    Age  int
    Tags []string
}
```

generates real `SerdeEncode(Encoder) error` / `SerdeDecode(Decoder) error`
methods on `User`, dispatched per field from the field's *syntax* shape
(string/int/float/bool, `*T`, `[]T`/`[N]T`, `map[string]T`, or an assumed
nested `@serde` struct) — no reflection. Both methods work against *any*
backend implementing the two interfaces: a `JSONEncoder`/`JSONDecoder` and
a length-prefixed positional `BinaryEncoder`/`BinaryDecoder` (a minimal
wire protocol for talking between your own services) ship as plain Go in
`test_serde/`, and adding a third format later — YAML, MessagePack, a
different wire protocol — costs zero new codegen: just implement the
interface once.

Rewritten pre-typecheck (`noder.rewriteSerdeDecorators`), but unlike the
other two features, the generated method bodies are assembled as Go
*source text* and reparsed with `syntax.Parse` into a synthetic file
whose declarations get spliced into the real one — far less error-prone
than hand-building dozens of statement/expression AST node types for
every field-type shape. A field type this pass can't handle (channels,
funcs, `interface{}`, non-string map keys) is a real compile error at
that field's position; a field naming another type is assumed to be
another `@serde` struct, and if it isn't, the generated
`(v.Field).SerdeEncode(e)` call simply fails to compile with an ordinary
"undefined method" error (position points at the generated code, not the
field — a known rough edge of the reparse approach) rather than silently
dropping the field. See `test_serde/` (`serde.go` for the interfaces,
`serde_json.go` / `serde_binary.go` for the two backends, `main.go` for a
full round-trip demo including nested structs, slices, maps, and
pointers).

Known limitations (v1): struct types only, no generics, no
embedded/anonymous fields, `map` keys must be `string`.

---

# The Go Programming Language

Go is an open source programming language that makes it easy to build simple,
reliable, and efficient software.

![Gopher image](https://golang.org/doc/gopher/fiveyears.jpg)
*Gopher image by [Renee French][rf], licensed under [Creative Commons 4.0 Attribution license][cc4-by].*

Our canonical Git repository is located at https://go.googlesource.com/go.
There is a mirror of the repository at https://github.com/golang/go.

Unless otherwise noted, the Go source files are distributed under the
BSD-style license found in the LICENSE file.

### Download and Install

#### Binary Distributions

Official binary distributions are available at https://go.dev/dl/.

After downloading a binary release, visit https://go.dev/doc/install
for installation instructions.

#### Install From Source

If a binary distribution is not available for your combination of
operating system and architecture, visit
https://go.dev/doc/install/source
for source installation instructions.

### Contributing

Go is the work of thousands of contributors. We appreciate your help!

To contribute, please read the contribution guidelines at https://go.dev/doc/contribute.

Note that the Go project uses the issue tracker for bug reports and
proposals only. See https://go.dev/wiki/Questions for a list of
places to ask questions about the Go language.

[rf]: https://reneefrench.blogspot.com/
[cc4-by]: https://creativecommons.org/licenses/by/4.0/
