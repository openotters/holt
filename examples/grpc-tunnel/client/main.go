// Command client is a holt peer that serves a real gRPC service
// over the tunnel — with server reflection, so grpcurl can call it
// through the hub without any .proto files. The peer listens on
// nothing; the hub reaches its gRPC service by dialing back through
// the tunnel.
//
//	go run ./examples/grpc-tunnel/client --id client-1
//	go run ./examples/grpc-tunnel/client --id client-2
//
// The peer registers the standard gRPC health service (reporting
// SERVING) plus reflection. Call it via the hub — see ../server.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"

	"github.com/openotters/holt/dial"
)

func main() {
	hubAddr := flag.String("hub", "127.0.0.1:7500", "hub tunnel (gRPC) address")
	id := flag.String("id", "client-1", "this peer's id (how the hub addresses it)")
	flag.Parse()

	if err := run(*hubAddr, *id); err != nil {
		log.Fatal(err)
	}
}

func run(hubAddr, id string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// A real gRPC server — health + reflection — served over the
	// tunnel. *grpc.Server is an http.Handler (ServeHTTP), so it
	// plugs straight into dial.Options.Handler; the hub speaks h2 over
	// the tunnel, so gRPC works end to end.
	grpcSrv := grpc.NewServer()

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("demo.Echo", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcSrv, hs)

	reflection.Register(grpcSrv) // lets grpcurl discover services with no .proto

	// The peer announces its id to the hub via a metadata header on
	// the Attach call; the hub uses it as the tunnel key.
	cc, err := grpc.NewClient(hubAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(peerIDUnary(id)),
		grpc.WithStreamInterceptor(peerIDStream(id)))
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("serving gRPC (health + reflection) over the tunnel",
		zap.String("hub", hubAddr), zap.String("id", id))

	// grpcSrv implements http.Handler via ServeHTTP.
	var handler http.Handler = grpcSrv

	if err := dial.Run(ctx, dial.Options{
		Conn:    cc,
		Handler: handler,
		Version: "grpc-tunnel-demo",
		Logger:  logger,
	}); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}

const peerIDHeader = "x-peer-id"

func peerIDUnary(id string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, peerIDHeader, id)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func peerIDStream(id string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, peerIDHeader, id)

		return streamer(ctx, desc, cc, method, opts...)
	}
}
