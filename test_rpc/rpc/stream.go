// Package rpc provides the generic streaming plumbing @rpc-generated
// service/client code targets, so gpp's noder codegen never has to emit a
// new stream type per method -- these four generic wrappers (parameterized
// per message type) cover every gRPC streaming shape.
package rpc

import (
	"context"

	"google.golang.org/grpc"
)

// ServerStream is the send side a server-streaming method implementation
// pushes responses through.
type ServerStream[T any] struct {
	grpc.ServerStream
}

func (s *ServerStream[T]) Send(m *T) error          { return s.ServerStream.SendMsg(m) }
func (s *ServerStream[T]) Context() context.Context { return s.ServerStream.Context() }

// ClientStream is the receive side a client-streaming method
// implementation reads incoming requests from.
type ClientStream[T any] struct {
	grpc.ServerStream
}

func (s *ClientStream[T]) Recv() (*T, error) {
	m := new(T)
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
func (s *ClientStream[T]) Context() context.Context { return s.ServerStream.Context() }

// BidiStream is both sides, for a full-duplex method implementation.
type BidiStream[Req, Resp any] struct {
	grpc.ServerStream
}

func (s *BidiStream[Req, Resp]) Send(m *Resp) error { return s.ServerStream.SendMsg(m) }
func (s *BidiStream[Req, Resp]) Recv() (*Req, error) {
	m := new(Req)
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
func (s *BidiStream[Req, Resp]) Context() context.Context { return s.ServerStream.Context() }

// ClientRecvStream is what a client-side call to a server-streaming method
// returns: a handle the caller reads responses from.
type ClientRecvStream[T any] struct {
	grpc.ClientStream
}

func (s *ClientRecvStream[T]) Recv() (*T, error) {
	m := new(T)
	if err := s.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ClientSendStream is what a client-side call to a client-streaming method
// returns: a handle the caller sends requests into, then closes to get the
// single final response.
type ClientSendStream[Req, Resp any] struct {
	grpc.ClientStream
}

func (s *ClientSendStream[Req, Resp]) Send(m *Req) error { return s.ClientStream.SendMsg(m) }
func (s *ClientSendStream[Req, Resp]) CloseAndRecv() (*Resp, error) {
	if err := s.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(Resp)
	if err := s.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ClientBidiStream is what a client-side call to a bidi method returns.
type ClientBidiStream[Req, Resp any] struct {
	grpc.ClientStream
}

func (s *ClientBidiStream[Req, Resp]) Send(m *Req) error { return s.ClientStream.SendMsg(m) }
func (s *ClientBidiStream[Req, Resp]) Recv() (*Resp, error) {
	m := new(Resp)
	if err := s.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
func (s *ClientBidiStream[Req, Resp]) CloseSend() error { return s.ClientStream.CloseSend() }
