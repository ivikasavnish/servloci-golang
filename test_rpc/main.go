package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"rpcdemo/service"
)

func main() {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	srv := service.NewServer()
	go func() {
		if err := srv.Serve(lis); err != nil {
			fmt.Println("serve error:", err)
		}
	}()
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	client, cc, err := service.DialClient(lis.Addr().String())
	if err != nil {
		panic(err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// unary
	resp, err := client.PlaceOrder(ctx, &service.PlaceOrderReq{Symbol: "AAPL", Qty: 10})
	if err != nil {
		panic(err)
	}
	fmt.Printf("PlaceOrder: %+v\n", resp)

	// server-streaming
	stream, err := client.WatchTicks(ctx, &service.WatchReq{Symbol: "AAPL"})
	if err != nil {
		panic(err)
	}
	for {
		tick, err := stream.Recv()
		if err != nil {
			break // io.EOF at end of stream
		}
		fmt.Printf("Tick: %+v\n", *tick)
	}

	// client-streaming
	up, err := client.UploadFills(ctx)
	if err != nil {
		panic(err)
	}
	for i := 0; i < 3; i++ {
		if err := up.Send(&service.Tick{Symbol: "AAPL", Price: float64(i)}); err != nil {
			panic(err)
		}
	}
	upResp, err := up.CloseAndRecv()
	if err != nil {
		panic(err)
	}
	fmt.Printf("UploadFills: %+v\n", upResp)

	// bidi streaming
	chat, err := client.Chat(ctx)
	if err != nil {
		panic(err)
	}
	go func() {
		for i := 0; i < 3; i++ {
			chat.Send(&service.Tick{Symbol: "MSFT", Price: float64(i + 1)})
		}
		chat.CloseSend()
	}()
	for i := 0; i < 3; i++ {
		t, err := chat.Recv()
		if err != nil {
			break
		}
		fmt.Printf("Chat echo: %+v\n", *t)
	}

	fmt.Println("done")
}
