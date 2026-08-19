---
title: Format-agnostic codec
---

[← back to index](index.html)

## Format-agnostic serialization (`@codec`) {#codec-decorator}

**Landed:** `dd1e76a7bb` — 2026-08-19, as `@serde`
**Renamed:** `0875646a24` — 2026-08-19, `@serde` → `@codec` throughout
(matches Go's own naming convention, `encoding.Codec`, instead of
borrowing Rust's `serde` name)

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

### Implementation

Rewritten pre-typecheck in `noder.rewriteCodecDecorators`
(`src/cmd/compile/internal/noder/noder.go`; `@` also becomes legal
before a type decl here, not just func decls, extending the parser
support added for [decorators](01-decorators.html)). Unlike decorators or
the try-operator, the generated method bodies are assembled as Go
**source text** and reparsed with `syntax.Parse` into a synthetic file
whose declarations get spliced into the real one — far less error-prone
than hand-building dozens of statement/expression AST node types for
every field-type shape.

A field type this pass can't handle (channels, funcs, `interface{}`,
non-string map keys) is a real compile error at that field's position; a
field naming another type is assumed to be another `@codec` struct, and
if it isn't, the generated `(v.Field).CodecEncode(e)` call simply fails
to compile with an ordinary "undefined method" error (position points at
the generated code, not the field — a known rough edge of the reparse
approach) rather than silently dropping the field.

### Known limitations (v1)

Struct types only, no generics, no embedded/anonymous fields, `map`
keys must be `string`.

### See also

`test_codec/` in the `gpp` wrapper repo (`codec.go` for the interfaces,
`codec_json.go` / `codec_binary.go` for the two backends, `main.go` for a
full round-trip demo including nested structs, slices, maps, and
pointers). `@codec` structs are also the message type for
[`@rpc`](05-rpc-decorator.html) services.
