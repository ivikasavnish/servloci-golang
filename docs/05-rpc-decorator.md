---
title: Native gRPC services
---

[← back to index](index.html)

## Native gRPC services (`@rpc`) {#rpc-decorator}

**Landed:** `c9932ad0f8` — 2026-08-19

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

### Implementation

`noder.rewriteRPCDecorators` (also reparse-generated, like
[`@codec`](04-codec-decorator.html)) pattern-matches each method against
the four gRPC shapes — unary, server-streaming, client-streaming, bidi —
purely from the syntax tree, and generates a client type
(`OrderServiceClient` / `NewOrderServiceClient`), a `grpc.ServiceDesc`
with per-method handlers, and `RegisterOrderServiceServer`.
`PlaceOrderReq`/`Tick`/etc are ordinary `@codec` structs.
`ServerStream[T]`/`ClientStream[T]`/`BidiStream[Req, Resp]` (plus the
client-side `ClientRecvStream`/`ClientSendStream`/`ClientBidiStream`)
are four **generic** wrapper types in a small runtime package
(`test_rpc/rpc/`), written once, reused for every method and every
message type — codegen never needs a new stream type per method.

Wire format is gRPC's pluggable `encoding.Codec`, backed by `@codec`'s
binary encoder (`codec.Codec`) instead of protobuf — set via
`grpc.ForceServerCodec`/`grpc.ForceCodec`.

### Deliberate v1 tradeoff

This only interoperates with other gpp-compiled services, not arbitrary
protoc-generated clients in other languages — true protobuf wire
compatibility would need field-numbered tags in the encoder and was
scoped out.

A method whose signature doesn't match one of the four shapes is a real
compile error at that method's position. The generated code assumes the
declaring file already imports `context`, `google.golang.org/grpc` (as
`grpc`), and the stream runtime package (as `rpc`) — required regardless
of which shapes are used, since generated client methods always take a
`context.Context`; re-importing them itself would be a duplicate-import
error, so codegen deliberately doesn't emit an import block.

### See also

`test_rpc/` in the `gpp` wrapper repo for a full runnable example
(`service/service.go` defines the service, `main.go` dials a real
listener and exercises all four shapes) — it's its own Go module
(`go.mod`) since it pulls in real `google.golang.org/grpc`.
