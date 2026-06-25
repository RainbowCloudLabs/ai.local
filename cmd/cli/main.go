package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	pb "github.com/daneshih1125/ai.local/proto"
	"github.com/manifoldco/promptui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// CLIConfig holds global CLI runtime options.
type CLIConfig struct {
	Addr string
}

// parseCLIConfig parses global flags and returns CLI configuration.
func parseCLIConfig() CLIConfig {
	grpcAddr := flag.String("addr", "127.0.0.1:50051", "gRPC control plane server address")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ai.local.cli [options] <resource> <action> [args]\n\n")
		fmt.Fprintf(os.Stderr, "Resources & Actions:\n")
		fmt.Fprintf(os.Stderr, "  route list\n")
		fmt.Fprintf(os.Stderr, "  quota list\n")
		fmt.Fprintf(os.Stderr, "  key list\n")
		fmt.Fprintf(os.Stderr, "  key add <route_path>\n")
		fmt.Fprintf(os.Stderr, "  key del <key_uuid>\n")
		fmt.Fprintf(os.Stderr, "  stats [--start-date YYYY-MM-DD --end-date YYYY-MM-DD]\n")
		fmt.Fprintf(os.Stderr, "  stats --monthly [--year YYYY]\n")
		fmt.Fprintf(os.Stderr, "  stats --verbose [--count N]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	return CLIConfig{
		Addr: *grpcAddr,
	}
}

// newAdminClient creates a TLS gRPC connection and returns an AdminService client.
func newAdminClient(addr string) (pb.AdminServiceClient, *grpc.ClientConn, error) {
	creds := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to control plane at %s: %w", addr, err)
	}

	return pb.NewAdminServiceClient(conn), conn, nil
}

func main() {
	cfg := parseCLIConfig()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	resource := args[0]
	action := ""
	if len(args) >= 2 {
		action = args[1]
	}

	client, conn, err := newAdminClient(cfg.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	cmdCtx := context.Background()

	switch resource {
	case "route":
		if action == "list" {
			handleRouteList(cmdCtx, client)
			return
		}
		flag.Usage()
		os.Exit(1)

	case "quota":
		if action == "list" {
			handleQuotaList(cmdCtx, client)
			return
		}
		flag.Usage()
		os.Exit(1)

	case "key":
		switch action {
		case "list":
			handleKeyList(cmdCtx, client)
		case "add":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "error: missing target route, e.g., 'key add /claude'")
				os.Exit(1)
			}
			handleKeyAdd(cmdCtx, client, args[2])
		case "del":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "error: missing tracking key uuid, e.g., 'key del <uuid>'")
				os.Exit(1)
			}
			handleKeyDel(cmdCtx, client, args[2])
		default:
			flag.Usage()
			os.Exit(1)
		}
		return

	case "stats":
		if err := handleStatsCommand(cmdCtx, client, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return

	default:
		flag.Usage()
		os.Exit(1)
	}
}

// handleRouteList fetches and prints all route definitions.
func handleRouteList(ctx context.Context, client pb.AdminServiceClient) {
	resp, err := client.ListRoutes(ctx, &pb.ListRoutesRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc error: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "URI\tQUOTA\tPROVIDER")
	for _, r := range resp.Routes {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Uri, r.Quota, r.Provider)
	}
	w.Flush()
}

// handleQuotaList fetches and prints quota definitions.
func handleQuotaList(ctx context.Context, client pb.AdminServiceClient) {
	resp, err := client.ListQuotas(ctx, &pb.ListQuotasRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc error: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "NAME\tDAILY\tMONTHLY\tMODE\tSTATUS")
	for _, q := range resp.Quotas {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", q.Name, q.Daily, q.Monthly, q.Mode, q.Status)
	}
	w.Flush()
}

// handleKeyList fetches and prints key mappings.
func handleKeyList(ctx context.Context, client pb.AdminServiceClient) {
	resp, err := client.ListKeys(ctx, &pb.ListKeysRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc error: %v\n", err)
		os.Exit(1)
	}

	if len(resp.Keys) == 0 {
		fmt.Println("No active routing keys found inside keystore.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "UUID\tALIAS\tROUTE\tREAL KEY\tINTERNAL KEY")
	for _, k := range resp.Keys {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", k.Uuid, k.Alias, k.Route, k.RealKey, k.InternalKey)
	}
	w.Flush()
}

// handleKeyAdd prompts for credentials and submits AddKey via RPC.
func handleKeyAdd(ctx context.Context, client pb.AdminServiceClient, route string) {

	keyPrompt := promptui.Prompt{
		Label: "Please input key",
		Mask:  '*',
	}
	realKey, err := keyPrompt.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "input error: %v\n", err)
		os.Exit(1)
	}

	aliasPrompt := promptui.Prompt{
		Label: "Alias (allow empty)",
	}
	alias, err := aliasPrompt.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "input error: %v\n", err)
		os.Exit(1)
	}

	netCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := client.AddKey(netCtx, &pb.AddKeyRequest{
		Route:   route,
		RealKey: realKey,
		Alias:   alias,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nGenerated internal key successfully:\n%s\n", resp.InternalKey)
}

// handleKeyDel deletes a key record by UUID.
func handleKeyDel(ctx context.Context, client pb.AdminServiceClient, uuid string) {
	resp, err := client.DeleteKey(ctx, &pb.DeleteKeyRequest{Uuid: uuid})
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc error: %v\n", err)
		os.Exit(1)
	}

	if resp.Success {
		fmt.Printf("Successfully revoked key tracking target [%s]\n", uuid)
	} else {
		fmt.Fprintf(os.Stderr, "error: tracking entity target [%s] not found inside keystore\n", uuid)
		os.Exit(1)
	}
}
