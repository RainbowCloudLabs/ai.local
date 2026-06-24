package grpc

import (
	"fmt"
	"net"

	// Adjust these import paths according to your real go.mod module name
	"github.com/daneshih1125/ai.local/internal/apml"
	"github.com/daneshih1125/ai.local/internal/keystore"
	"github.com/daneshih1125/ai.local/internal/usage"
	pb "github.com/daneshih1125/ai.local/proto"

	// Alias the official Google gRPC package as "g" to completely avoid naming collisions
	g "google.golang.org/grpc"
)

// Server encapsulates execution life-cycles for the administrative gRPC pipeline.
type Server struct {
	grpcServer *g.Server
	listener   net.Listener
	addr       string
}

// NewServer registers handlers, generates in-memory TLS certs, and binds to network sockets.
func NewServer(addr string, cfg *apml.APMLConfig, store *keystore.Store, usageStore *usage.UsageStore) (*Server, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to open gRPC transport address loop: %w", err)
	}

	// Call our refactored in-memory self-signed TLS compiler from tls.go
	tlsCreds, err := GenerateEphemeralGRPCTLSConfig()
	if err != nil {
		lis.Close() // Avoid resource leaks if crypto setup fails
		return nil, fmt.Errorf("failed to load administrative tls layer: %w", err)
	}

	// Lock down the server channels with 100% SSL using the official g.Creds option
	s := g.NewServer(g.Creds(tlsCreds))

	// Bind administrative business logic handlers
	handler := NewAdminHandler(cfg, store, usageStore)
	pb.RegisterAdminServiceServer(s, handler)

	return &Server{
		grpcServer: s,
		listener:   lis,
		addr:       addr,
	}, nil
}

// Start spawns the listener interface onto an isolated operational plane thread.
func (s *Server) Start() error {
	fmt.Printf("ai.local-control admin control-plane listening on gRPC: %s\n", s.addr)
	if err := s.grpcServer.Serve(s.listener); err != nil {
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
