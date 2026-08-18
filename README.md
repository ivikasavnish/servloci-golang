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
