package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/daneshih1125/ai.local/grpc"
	"github.com/daneshih1125/ai.local/internal/apml"
	"github.com/daneshih1125/ai.local/internal/keystore"
	"github.com/daneshih1125/ai.local/internal/proxy"
	internaltls "github.com/daneshih1125/ai.local/internal/tls"
	"github.com/daneshih1125/ai.local/internal/usage"
)

const (
	defaultCertPath = "/etc/ai.local/ai.local.crt"
	defaultKeyPath  = "/etc/ai.local/ai.local.key"
	defaultDBPath   = "/etc/ai.local/usage.db"
)

func main() {
	configFile := flag.String("f", "", "Path to APML config file (required)")
	certFile := flag.String("cert", "", "Path to TLS certificate file")
	keyFile := flag.String("key", "", "Path to TLS private key file")
	grpcAddr := flag.String("grpc-addr", ":50051", "gRPC control plane listen address")
	proxyAddr := flag.String("proxy-addr", ":8443", "Proxy data plane listen address")
	flag.Parse()

	// Validate required CLI arguments
	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "error: -f <config.apml> is required")
		flag.Usage()
		os.Exit(1)
	}

	// Parse and validate APML configuration via ironclad defense layer
	cfg, err := apml.Parse(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ai.local engine initialized: version %s — gateway base URI: %s\n", cfg.Version, cfg.BaseURI)

	// Initialize SQLite DB
	db, err := sql.Open("sqlite", defaultDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open usage db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := usage.InitSchema(db); err != nil {
		fmt.Fprintf(os.Stderr, "failed to init usage schema: %v", err)
		os.Exit(1)
	}
	usageBackend := usage.NewUsageBackend(db)
	usageBackend.StartWorker()
	defer usageBackend.Stop()

	// Resolve data-plane TLS credentials for Gin reverse proxy
	certPath, keyPath, err := resolveTLS(*certFile, *keyFile, cfg.BaseURI)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: TLS resolution failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Data-plane TLS cert: %s\n", certPath)
	fmt.Printf("Data-plane TLS key:  %s\n", keyPath)

	// ==========================================================================
	// Initialize State Repository (In-Memory Secure Token Store)
	// ==========================================================================
	keyStore := keystore.NewStore()

	// ==========================================================================
	// Initialize UsageStore
	// ==========================================================================
	usageStore := usage.NewUsageStore(db)

	// ==========================================================================
	//  Initialize Control Plane: TLS-enforced gRPC Server
	// ==========================================================================
	// Uses GenerateEphemeralGRPCTLSConfig internally to enforce 100% SSL channels
	grpcServer, err := grpc.NewServer(*grpcAddr, cfg, keyStore, usageStore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialize gRPC server: %v\n", err)
		os.Exit(1)
	}

	// Spawn gRPC server onto an isolated thread to prevent blocking the application
	go func() {
		if err := grpcServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "critical: gRPC control plane crashed: %v\n", err)
			os.Exit(1)
		}
	}()

	// ==========================================================================
	// Initialize Data Plane: Gin-backed L7 Reverse Proxy Engine
	// ==========================================================================
	proxyServer, err := proxy.NewProxyServer(cfg, keyStore, usageBackend, usageStore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialize proxy engine: %v\n", err)
		os.Exit(1)
	}

	// Spawn reverse proxy listener asynchronously
	go func() {
		if err := proxyServer.Start(*proxyAddr, certPath, keyPath); err != nil {
			fmt.Fprintf(os.Stderr, "critical: data plane proxy engine crashed: %v\n", err)
			os.Exit(1)
		}
	}()

	// ==========================================================================
	// Coordinate Graceful Shutdown Protocol via OS Signal Interception
	// ==========================================================================
	fmt.Println("\nai.local core services fully energized. Press Ctrl+C to terminate.")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan // Blocks execution loop here until termination signal is trapped

	fmt.Println("\n[ai.local] Initiating graceful shutdown protocol...")
	grpcServer.Stop() // Securely unbind transport sockets and connections
	fmt.Println("[ai.local] Core services safely deactivated. Goodbye.")
}

// resolveTLS determines the cert and key paths to use.
// If not specified via flags, checks default paths or prompts the user to generate.
func resolveTLS(certFlag, keyFlag, baseURI string) (string, string, error) {
	// both explicitly specified
	if certFlag != "" && keyFlag != "" {
		return certFlag, keyFlag, nil
	}

	// check default paths
	if internaltls.CertExists(defaultCertPath, defaultKeyPath) {
		return defaultCertPath, defaultKeyPath, nil
	}

	// prompt user
	fmt.Println()
	fmt.Println("No TLS certificate specified.")
	fmt.Printf("Generate a self-signed certificate for %s? [y/N]: ", baseURI)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))

	if input != "y" && input != "yes" {
		return "", "", fmt.Errorf("cannot start without TLS certificate")
	}

	// ensure output directory exists
	if err := os.MkdirAll("/etc/ai.local", 0755); err != nil {
		return "", "", fmt.Errorf("failed to create /etc/ai.local: %w", err)
	}

	fmt.Printf("Generating self-signed certificate for %s...\n", baseURI)
	if err := internaltls.GenerateSelfSignedCert(baseURI, defaultCertPath, defaultKeyPath); err != nil {
		return "", "", fmt.Errorf("failed to generate certificate: %w", err)
	}

	fmt.Printf("  → %s\n", defaultCertPath)
	fmt.Printf("  → %s\n", defaultKeyPath)
	fmt.Println()
	fmt.Println("To trust this certificate, run:")
	fmt.Println(
		"  Linux:   sudo cp /etc/ai.local/ai.local.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates",
	)
	fmt.Println(
		"  Mac:     sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain /etc/ai.local/ai.local.crt",
	)
	fmt.Println("  Windows: certutil -addstore \"Root\" /etc/ai.local/ai.local.crt")
	fmt.Println()

	return defaultCertPath, defaultKeyPath, nil
}
