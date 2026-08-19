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
