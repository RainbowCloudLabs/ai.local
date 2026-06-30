package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/RainbowCloudLabs/ai.local/grpc"
	"github.com/RainbowCloudLabs/ai.local/internal/apml"
	"github.com/RainbowCloudLabs/ai.local/internal/keystore"
	"github.com/RainbowCloudLabs/ai.local/internal/logx"
	"github.com/RainbowCloudLabs/ai.local/internal/proxy"
	internaltls "github.com/RainbowCloudLabs/ai.local/internal/tls"
	"github.com/RainbowCloudLabs/ai.local/internal/usage"
)

type flags struct {
	dataDir     string
	configFile  string
	grpcAddr    string
	proxyAddr   string
	genCertOnly bool
}

type services struct {
	grpcServer   *grpc.Server
	proxyServer  *proxy.ProxyServer
	usageBackend *usage.UsageBackend
}

func main() {
	f := parseFlags()

	if err := os.MkdirAll(f.dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create data directory %s: %v", f.dataDir, err)
		os.Exit(1)
	}

	configPath := resolveConfigPath(f)
	cfg := loadConfig(configPath)

	if err := logx.Init(f.dataDir, buildPlanName(cfg)+".log", false); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v", err)
		os.Exit(1)
	}

	if f.genCertOnly {
		generateCert(cfg.BaseURI, f.dataDir)
		return
	}

	logx.AppInfof("ai.local engine running: version %s — gateway base URI: %s", cfg.Version, cfg.BaseURI)

	certPath, keyPath := resolveCertPaths(f.dataDir)
	if !internaltls.CertExists(certPath, keyPath) {
		logx.AppErrorf("TLS credentials missing in data directory: %s", f.dataDir)
		fmt.Fprintln(os.Stderr, "error: TLS credentials missing in data directory.")
		fmt.Fprintln(os.Stderr, "To provision self-signed certs, run:")
		fmt.Fprintf(os.Stderr, "  ai.local -d %s -gen-cert\n", f.dataDir)
		fmt.Fprintln(os.Stderr, "Alternatively, place your custom OpenSSL / ACME certificates into the folder manually.")
		os.Exit(1)
	}
	logx.AppDebugf("TLS certificate alignment verified: crt=%s, key=%s", certPath, keyPath)

	logx.AppInfof("initializing usage backend store (SQLite)...")
	db := initDatabase(f.dataDir, cfg)
	defer db.Close()

	svc := initServices(cfg, db)
	defer svc.usageBackend.Stop()

	go backupAPMLConfiguration(f.dataDir, configPath, buildPlanName(cfg))

	startServices(svc, f, certPath, keyPath)

	logx.AppInfof("core services fully energized. control-plane: %s, data-plane: %s", f.grpcAddr, f.proxyAddr)
	fmt.Println("\nai.local core services fully energized. Press Ctrl+C to terminate.")
	waitForShutdown(svc)
}

// ---- flag parsing ----

func parseFlags() flags {
	f := flags{}
	flag.StringVar(&f.dataDir, "d", "/etc/ai.local", "Base directory for all runtime data (DB, certs, config)")
	flag.StringVar(&f.configFile, "f", "", "Path to APML config file (Optional. Defaults to <dataDir>/ai.local.apml)")
	flag.StringVar(&f.grpcAddr, "grpc-addr", ":50051", "gRPC control plane listen address")
	flag.StringVar(&f.proxyAddr, "proxy-addr", ":8443", "Proxy data plane listen address")
	flag.BoolVar(&f.genCertOnly, "gen-cert", false, "Generate a self-signed TLS certificate into the data directory and exit")
	flag.Parse()
	return f
}

// ---- config ----

func resolveConfigPath(f flags) string {
	if f.configFile != "" {
		return f.configFile
	}
	return filepath.Join(f.dataDir, "ai.local.apml")
}

func loadConfig(configPath string) *apml.APMLConfig {
	cfg, err := apml.Parse(configPath)
	if err != nil {
		fatalf("failed to load config: %v", err)
	}
	fmt.Printf("ai.local engine initialized: version %s — gateway base URI: %s\n", cfg.Version, cfg.BaseURI)
	return cfg
}

// ---- cert ----

func generateCert(baseURI, dataDir string) {
	certPath := filepath.Join(dataDir, "ai.local.crt")
	keyPath := filepath.Join(dataDir, "ai.local.key")

	fmt.Printf("Generating self-signed certificate for %s into %s...\n", baseURI, dataDir)
	logx.AppInfof("generating self-signed certificate for %s into %s...", baseURI, dataDir)

	if err := internaltls.GenerateSelfSignedCert(baseURI, certPath, keyPath); err != nil {
		fatalf("failed to generate certificate: %v", err)
	}
	fmt.Println("TLS credentials successfully synchronized onto disk.")
	logx.AppInfof("TLS credentials successfully synchronized onto disk.")
}

func resolveCertPaths(dataDir string) (certPath, keyPath string) {
	return filepath.Join(dataDir, "ai.local.crt"),
		filepath.Join(dataDir, "ai.local.key")
}

// ---- database ----

func buildDBName(cfg *apml.APMLConfig) string {
	cleaner := strings.NewReplacer(
		"https://", "", "http://", "",
		".", "-", ":", "-", "/", "-",
	)
	return fmt.Sprintf("%s-%s-usage.db", cleaner.Replace(cfg.BaseURI), cfg.PlanVersion)
}

func buildPlanName(cfg *apml.APMLConfig) string {
	cleaner := strings.NewReplacer(
		"https://", "", "http://", "",
		".", "-", ":", "-", "/", "-",
	)
	return fmt.Sprintf("%s-%s", cleaner.Replace(cfg.BaseURI), cfg.PlanVersion)
}

func initDatabase(dataDir string, cfg *apml.APMLConfig) *sql.DB {
	dbPath := filepath.Join(dataDir, buildDBName(cfg))
	logx.AppDebugf("opening sqlite database connection at: %s", dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fatalf("failed to open usage db: %v", err)
	}
	if err := usage.InitSchema(db); err != nil {
		fatalf("failed to init usage schema: %v", err)
	}
	logx.AppInfof("sqlite storage engine schema initialized successfully.")
	return db
}

// ---- services ----

func initServices(cfg *apml.APMLConfig, db *sql.DB) *services {
	logx.AppDebugf("assembling runtime internal subsystems...")
	keyStore := keystore.NewStore()
	usageStore := usage.NewUsageStore(db)

	usageBackend := usage.NewUsageBackend(db)
	usageBackend.StartWorker()
	logx.AppDebugf("asynchronous token ledger background worker spawned.")

	grpcServer, err := grpc.NewServer(cfg, keyStore, usageStore)
	if err != nil {
		fatalf("failed to initialize gRPC server: %v", err)
	}

	proxyServer, err := proxy.NewProxyServer(cfg, keyStore, usageBackend, usageStore)
	if err != nil {
		fatalf("failed to initialize proxy engine: %v", err)
	}

	return &services{
		grpcServer:   grpcServer,
		proxyServer:  proxyServer,
		usageBackend: usageBackend,
	}
}

func startServices(svc *services, f flags, certPath, keyPath string) {
	go func() {
		logx.AppDebugf("binding gRPC control plane listener onto %s", f.grpcAddr)
		if err := svc.grpcServer.Start(f.grpcAddr); err != nil {
			fatalf("gRPC control plane crashed: %v", err)
		}
	}()

	go func() {
		logx.AppDebugf("binding reverse proxy data plane listener onto %s (TLS)", f.proxyAddr)
		if err := svc.proxyServer.Start(f.proxyAddr, certPath, keyPath); err != nil {
			fatalf("data plane proxy engine crashed: %v", err)
		}
	}()
}

func waitForShutdown(svc *services) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	logx.AppWarnf("received terminal signal (%v). initiating graceful shutdown protocol...", sig)
	fmt.Println("\n[ai.local] Initiating graceful shutdown protocol...")

	svc.grpcServer.Stop()
	logx.AppInfof("core gateway services safely deactivated. engine shutdown complete.")
	fmt.Println("[ai.local] Core services safely deactivated. Goodbye.")
}

// ---- backup ----

func backupAPMLConfiguration(dataDir, configPath, planName string) {
	timestamp := time.Now().Format("20060102-150405")

	latestPath := filepath.Join(dataDir, fmt.Sprintf(".apml-%s", planName))
	historyPath := filepath.Join(dataDir, fmt.Sprintf(".apml-%s-%s", planName, timestamp))

	content, err := os.ReadFile(configPath)
	if err != nil {
		logx.AppWarnf("[Backup Warning] Failed to read source APML: %v", err)
		return
	}

	if err := os.WriteFile(latestPath, content, 0644); err != nil {
		logx.AppWarnf("[Backup Warning] Failed to sync latest APML snapshot: %v", err)
	}
	if err := os.WriteFile(historyPath, content, 0644); err != nil {
		logx.AppWarnf("[Backup Warning] Failed to sync historical APML: %v", err)
		return
	}

	logx.AppInfof("APML routing specifications successfully backed up. snapshot=%s, archive=%s", latestPath, historyPath)
	fmt.Println("[Backup] Configuration synchronized.")
	fmt.Printf("  → Snapshot:   %s\n", latestPath)
	fmt.Printf("  → Trajectory: %s\n", historyPath)
}

// ---- helpers ----

func fatalf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logx.AppErrorf("FATAL PANIC: %s", msg)
	os.Exit(1)
}
