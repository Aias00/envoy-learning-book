package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	server "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		if len(args) != 3 || args[0] != "backend" || (args[1] != "blue" && args[1] != "green") {
			return fmt.Errorf("usage: control-plane [backend blue|green :port]")
		}
		return serveHTTP(ctx, &http.Server{Addr: args[2], Handler: backendHandler(args[1]), ReadHeaderTimeout: 5 * time.Second})
	}
	cp, err := newControlPlane(ctx)
	if err != nil {
		return err
	}
	lis, err := net.Listen("tcp", ":18000")
	if err != nil {
		return err
	}
	defer lis.Close()
	grpcServer := grpc.NewServer()
	defer grpcServer.Stop()
	cp.registerADS(ctx, grpcServer, server.CallbackFuncs{
		StreamRequestFunc: func(streamID int64, req *discovery.DiscoveryRequest) error {
			if req.GetResponseNonce() == "" {
				return nil
			}
			if req.GetErrorDetail() != nil {
				log.Printf("NACK stream=%d type=%s version=%s nonce=%s error=%v", streamID, req.GetTypeUrl(), req.GetVersionInfo(), req.GetResponseNonce(), req.GetErrorDetail())
			} else {
				log.Printf("ACK stream=%d type=%s version=%s nonce=%s", streamID, req.GetTypeUrl(), req.GetVersionInfo(), req.GetResponseNonce())
			}
			return nil
		},
		StreamResponseFunc: func(_ context.Context, streamID int64, _ *discovery.DiscoveryRequest, resp *discovery.DiscoveryResponse) {
			log.Printf("send stream=%d type=%s version=%s nonce=%s", streamID, resp.GetTypeUrl(), resp.GetVersionInfo(), resp.GetNonce())
		},
	})
	// Stop both listeners if either fails, or when the process receives SIGTERM.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	grpcError := make(chan error, 1)
	go func() { grpcError <- grpcServer.Serve(lis); cancel() }()
	log.Printf("node=%s initial_version=v1 backend=blue ADS=:18000 management=:19000", nodeID)
	httpError := serveHTTP(ctx, &http.Server{Addr: ":19000", Handler: cp.handler(), ReadHeaderTimeout: 5 * time.Second})
	if httpError != nil {
		return httpError
	}
	select {
	case err := <-grpcError:
		if !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
	default:
	}
	return nil
}

func serveHTTP(ctx context.Context, srv *http.Server) error {
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		case <-stopped:
		}
	}()
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
