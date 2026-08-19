package service

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"rpcdemo/rpc"

	. "rpcdemo/codec"
)

@codec
type PlaceOrderReq struct {
	Symbol string
	Qty    int
}

@codec
type PlaceOrderResp struct {
	OrderID string
	Ok      bool
}

@codec
type Tick struct {
	Symbol string
	Price  float64
}

@codec
type WatchReq struct {
	Symbol string
}

@rpc
type OrderService interface {
	PlaceOrder(ctx context.Context, req *PlaceOrderReq) (*PlaceOrderResp, error)
	WatchTicks(req *WatchReq, stream *rpc.ServerStream[Tick]) error
	UploadFills(stream *rpc.ClientStream[Tick]) (*PlaceOrderResp, error)
	Chat(stream *rpc.BidiStream[Tick, Tick]) error
}

// orderServiceImpl is the server-side implementation a real service would
// provide -- ordinary Go, no codegen involved here.
type orderServiceImpl struct{}

func (orderServiceImpl) PlaceOrder(ctx context.Context, req *PlaceOrderReq) (*PlaceOrderResp, error) {
	return &PlaceOrderResp{OrderID: "ord-" + req.Symbol, Ok: true}, nil
}

func (orderServiceImpl) WatchTicks(req *WatchReq, stream *rpc.ServerStream[Tick]) error {
	prices := []float64{100.0, 100.5, 101.25}
	for _, p := range prices {
		if err := stream.Send(&Tick{Symbol: req.Symbol, Price: p}); err != nil {
			return err
		}
	}
	return nil
}

func (orderServiceImpl) UploadFills(stream *rpc.ClientStream[Tick]) (*PlaceOrderResp, error) {
	n := 0
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
		n++
	}
	return &PlaceOrderResp{OrderID: fmt.Sprintf("fills-%d", n), Ok: true}, nil
}

func (orderServiceImpl) Chat(stream *rpc.BidiStream[Tick, Tick]) error {
	for {
		t, err := stream.Recv()
		if err != nil {
			return nil
		}
		t.Price *= 2
		if err := stream.Send(t); err != nil {
			return err
		}
	}
}

// NewServer builds a *grpc.Server with the @codec binary codec forced
// (no protobuf) and the generated OrderService registered on it.
func NewServer() *grpc.Server {
	s := grpc.NewServer(grpc.ForceServerCodec(Codec{}))
	RegisterOrderServiceServer(s, orderServiceImpl{})
	return s
}

// DialClient connects to addr using the same forced codec.
func DialClient(addr string) (OrderServiceClient, *grpc.ClientConn, error) {
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(Codec{})),
	)
	if err != nil {
		return nil, nil, err
	}
	return NewOrderServiceClient(cc), cc, nil
}
