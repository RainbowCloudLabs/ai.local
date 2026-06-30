package grpc

import (
	"fmt"
	"net"

	"github.com/RainbowCloudLabs/ai.local/internal/apml"
	"github.com/RainbowCloudLabs/ai.local/internal/keystore"
	"github.com/RainbowCloudLabs/ai.local/internal/logx"
	"github.com/RainbowCloudLabs/ai.local/internal/usage"
	pb "github.com/RainbowCloudLabs/ai.local/proto"

	g "google.golang.org/grpc"
)

// Server encapsulates execution life-cycles for the administrative gRPC pipeline.
type Server struct {
	grpcServer *g.Server
}

// NewServer initializes the gRPC server with TLS and registers handlers.
// Binding to a network address is deferred to Start().
func NewServer(cfg *apml.APMLConfig, store *keystore.Store, usageStore *usage.UsageStore) (*Server, error) {
	tlsCreds, err := GenerateEphemeralGRPCTLSConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load administrative tls layer: %w", err)
	}

	s := g.NewServer(g.Creds(tlsCreds))

	handler := NewAdminHandler(cfg, store, usageStore)
	pb.RegisterAdminServiceServer(s, handler)

	return &Server{grpcServer: s}, nil
}

// Start binds to the given address and begins serving.
func (s *Server) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to open gRPC transport address %s: %w", addr, err)
	}
	logx.AppInfof("ai.local-control admin control-plane listening on gRPC: %s", addr)
	if err := s.grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("gRPC control execution crash: %w", err)
	}
	return nil
}

// Stop gracefully unbinds transport connections and releases sockets.
func (s *Server) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}
