# servloci-golang

A custom fork of the Go compiler (go1.27-devel) adding six
language/toolchain extensions on top of upstream Go: decorators, a
nil-safe selector, an error-propagating call operator, format-agnostic
serialization codegen, native gRPC codegen, and map dot-access sugar.
Everything below the "The Go Programming Language" section is stock
upstream Go documentation.

Full write-up of each addition, with implementation details and known
limitations: **[change book / rendered site](https://ivikasavnish.github.io/servloci-golang/)**
(source: [`doc/servloci/`](doc/servloci/)).

## Table of contents

- [Install](#install)
- [Quick example](#quick-example)
- Additions
  - [Decorator syntax (`@decorator`)](#decorator-syntax-decorator)
  - [Nil-safe selector (`?.`)](#nil-safe-selector-)
  - [Error-propagating call (`?`, `?[i]`)](#error-propagating-call--i)
  - [Format-agnostic serialization (`@codec`)](#format-agnostic-serialization-codec)
  - [Native gRPC services (`@rpc`)](#native-grpc-services-rpc)
  - [Map dot-access sugar (`m.foo`)](#map-dot-access-sugar-mfoo)

## Install

**Prebuilt binary** (Linux AMD64), via the [`gpp`](https://github.com/ivikasavnish/gpp)
wrapper repo:

```bash
git clone https://github.com/ivikasavnish/gpp
cd gpp
./install.sh
./gpp run test_decorator_syntax.go
```

`install.sh` pulls the latest tagged release
(`gpp-linux-amd64.tar.gz`) and drops it into `gpp/compiler`; `./gpp`
is a thin wrapper that sets `GOROOT` and execs the real `go` binary
inside it, so every `go` subcommand works normally (`./gpp build`,
`./gpp test`, ...), just with these extensions available.

**Note:** the latest tagged release (`v0.2.0`) predates the
error-propagating call, `@codec`, `@rpc`, and map dot-access sugar
additions below — it only has decorators and the nil-safe selector.
For the full current feature set, build from source instead:

```bash
git clone https://github.com/ivikasavnish/servloci-golang
cd servloci-golang/src
./make.bash               # builds cmd/go + cmd/compile
export GOROOT=$(cd .. && pwd)
$GOROOT/bin/go run yourfile.go
```

(Iterating on the compiler itself is faster: after editing
`src/cmd/compile/...`, `go build -o $GOROOT/pkg/tool/linux_amd64/compile cmd/compile`
rebuilds just the compiler in a few seconds, no full `make.bash` needed.)

## Quick example

```go
package main

import "fmt"

type Address struct{ City string }
type User struct{ Address *Address }

@timed
func greet(name string) map[string]any {
    r := map[string]any{"name": name}
    r.greeting = "hello, " + name // map dot-access sugar
    return r
}

func main() {
    r := greet("vikas")
    fmt.Println(r.greeting) // dot-access read too

    var u *User
    fmt.Println(u?.Address?.City) // nil-safe selector: "" not a panic
}
```

`@timed` needs a same-package `timed(func()) func()` decorator function
in scope — see [`test_decorator_syntax.go`](https://github.com/ivikasavnish/gpp/blob/main/test_decorator_syntax.go)
for a working one. Each addition below has its own focused example.

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

### Format-agnostic serialization (`@codec`)

Rust-serde-style codegen, adapted to Go: one generated method pair per
struct, decoupled from any specific wire format via two small interfaces
(`Encoder`/`Decoder`) that the user's package must declare (same
same-package convention as `@timed`/`@logged`):

```go
@codec
type User struct {
    Name string
    Age  int
    Tags []string
}
```

generates real `CodecEncode(Encoder) error` / `CodecDecode(Decoder) error`
methods on `User`, dispatched per field from the field's *syntax* shape
(string/int/float/bool, `*T`, `[]T`/`[N]T`, `map[string]T`, or an assumed
nested `@codec` struct) — no reflection. Both methods work against *any*
backend implementing the two interfaces: a `JSONEncoder`/`JSONDecoder` and
a length-prefixed positional `BinaryEncoder`/`BinaryDecoder` (a minimal
wire protocol for talking between your own services) ship as plain Go in
`test_codec/`, and adding a third format later — YAML, MessagePack, a
different wire protocol — costs zero new codegen: just implement the
interface once.

Rewritten pre-typecheck (`noder.rewriteCodecDecorators`), but unlike the
other two features, the generated method bodies are assembled as Go
*source text* and reparsed with `syntax.Parse` into a synthetic file
whose declarations get spliced into the real one — far less error-prone
than hand-building dozens of statement/expression AST node types for
every field-type shape. A field type this pass can't handle (channels,
funcs, `interface{}`, non-string map keys) is a real compile error at
that field's position; a field naming another type is assumed to be
another `@codec` struct, and if it isn't, the generated
`(v.Field).CodecEncode(e)` call simply fails to compile with an ordinary
"undefined method" error (position points at the generated code, not the
field — a known rough edge of the reparse approach) rather than silently
dropping the field. See `test_codec/` (`codec.go` for the interfaces,
`codec_json.go` / `codec_binary.go` for the two backends, `main.go` for a
full round-trip demo including nested structs, slices, maps, and
pointers).

Known limitations (v1): struct types only, no generics, no
embedded/anonymous fields, `map` keys must be `string`.

### Native gRPC services (`@rpc`)

Real gRPC — `google.golang.org/grpc`, actual HTTP/2 transport, deadlines,
interceptors, all four RPC shapes — with no `.proto` file, no `protoc`,
no generated `_pb.go`. A plain Go interface *is* the service definition:

```go
@rpc
type OrderService interface {
    PlaceOrder(ctx context.Context, req *PlaceOrderReq) (*PlaceOrderResp, error)
    WatchTicks(req *WatchReq, stream *rpc.ServerStream[Tick]) error
    UploadFills(stream *rpc.ClientStream[Tick]) (*PlaceOrderResp, error)
    Chat(stream *rpc.BidiStream[Tick, Tick]) error
}
```

`noder.rewriteRPCDecorators` (also reparse-generated, like `@codec`)
pattern-matches each method against the four gRPC shapes — unary,
server-streaming, client-streaming, bidi — purely from the syntax tree,
and generates a client type (`OrderServiceClient` /
`NewOrderServiceClient`), a `grpc.ServiceDesc` with per-method handlers,
and `RegisterOrderServiceServer`. `PlaceOrderReq`/`Tick`/etc are ordinary
`@codec` structs. `ServerStream[T]`/`ClientStream[T]`/`BidiStream[Req,
Resp]` (plus the client-side `ClientRecvStream`/`ClientSendStream`/
`ClientBidiStream`) are four **generic** wrapper types in a small runtime
package (`test_rpc/rpc/`), written once, reused for every method and
every message type — codegen never needs a new stream type per method.

Wire format is gRPC's pluggable `encoding.Codec`, backed by `@codec`'s
binary encoder (`codec.Codec`) instead of protobuf — set via
`grpc.ForceServerCodec`/`grpc.ForceCodec`. **Deliberate v1 tradeoff:**
this only interoperates with other gpp-compiled services, not arbitrary
protoc-generated clients in other languages — true protobuf wire compat
would need field-numbered tags in the encoder and was scoped out.

A method whose signature doesn't match one of the four shapes is a real
compile error at that method's position. The generated code assumes the
declaring file already imports `context`, `google.golang.org/grpc` (as
`grpc`), and the stream runtime package (as `rpc`) — required regardless
of which shapes are used, since generated client methods always take a
`context.Context`; re-importing them itself would be a duplicate-import
error, so codegen deliberately doesn't emit an import block.

See `test_rpc/` for a full runnable example (`service/service.go` defines
the service, `main.go` dials a real listener and exercises all four
shapes) — it's its own Go module (`go.mod`) since it pulls in real
`google.golang.org/grpc`.

### Map dot-access sugar (`m.foo`)

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

Unlike the other additions above, this isn't a pre-typecheck source
rewrite -- it can't be, since deciding whether `m.foo` means "map key" or
"undefined field" requires knowing `m`'s type, which isn't available
until the real type-checker runs. So it's implemented in
`types2.(*Checker).selector` (`src/cmd/compile/internal/types2/call.go`):
when normal field/method lookup fails and the base type's underlying
type is a map with a string-kind key, it resolves the selector as the
map's element type instead of erroring, and flags the syntax node
(`SelectorExpr.MapDot`, new field alongside the existing `NilSafe`).
`noder/writer.go` checks that flag and emits a plain index expression
(`exprIndex` + a synthesized string constant for the key) instead of the
usual field-selection bytecode, since there's no real `types2.Selection`
to record for a synthesized key. No parser or gofmt changes needed --
`.` is already valid syntax, only its type-checking meaning changes.

**Known gap:** a typo (`r.nmae`) silently returns the zero value instead
of erroring, same footgun as raw map indexing -- not new, but sugar
makes it easier to typo a field-like name by accident. A linter pass
against known map-literal keys would be the place to catch that, not the
compiler.

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
