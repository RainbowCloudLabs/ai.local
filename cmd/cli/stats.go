package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"text/tabwriter"
	"time"

	pb "github.com/daneshih1125/ai.local/proto"
)

// statsCLIOptions stores parsed options for the "stats" command.
type statsCLIOptions struct {
	StartDate string
	EndDate   string
	Monthly   bool
	Year      int
	Verbose   bool
	Count     int
}

// datePattern validates YYYY-MM-DD format.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseStatsOptions parses stats subcommand flags from remaining CLI args.
func parseStatsOptions(args []string) (statsCLIOptions, error) {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts statsCLIOptions
	fs.StringVar(&opts.StartDate, "start-date", "", "start date in YYYY-MM-DD (default: today)")
	fs.StringVar(&opts.EndDate, "end-date", "", "end date in YYYY-MM-DD (default: today)")
	fs.BoolVar(&opts.Monthly, "monthly", false, "show monthly stats (default year: current year)")
	fs.IntVar(&opts.Year, "year", 0, "year for monthly stats, e.g., 2026")
	fs.BoolVar(&opts.Verbose, "verbose", false, "show verbose logs (default count: 100)")
	fs.IntVar(&opts.Count, "count", 100, "row count for verbose mode (1..1000)")

	if err := fs.Parse(args); err != nil {
		return statsCLIOptions{}, err
	}

	// Validate mode exclusivity.
	if opts.Monthly && opts.Verbose {
		return statsCLIOptions{}, errors.New("flags --monthly and --verbose cannot be used together")
	}

	// Validate date flags only when daily mode is used.
	if !opts.Monthly && !opts.Verbose {
		if opts.StartDate != "" && !datePattern.MatchString(opts.StartDate) {
			return statsCLIOptions{}, fmt.Errorf("invalid --start-date format %q, expected YYYY-MM-DD", opts.StartDate)
		}
		if opts.EndDate != "" && !datePattern.MatchString(opts.EndDate) {
			return statsCLIOptions{}, fmt.Errorf("invalid --end-date format %q, expected YYYY-MM-DD", opts.EndDate)
		}
	}

	// Clamp verbose count to 1..1000.
	if opts.Count < 1 {
		opts.Count = 1
	}
	if opts.Count > 1000 {
		opts.Count = 1000
	}

	return opts, nil
}

// handleStatsCommand routes stats CLI options to GetStats RPC and prints corresponding table output.
func handleStatsCommand(ctx context.Context, client pb.AdminServiceClient, args []string) error {
	opts, err := parseStatsOptions(args)
	if err != nil {
		return err
	}

	req := &pb.GetStatsRequest{}

	switch {
	case opts.Verbose:
		req.Mode = pb.GetStatsRequest_VERBOSE
		req.MaxCount = int32(opts.Count)

	case opts.Monthly:
		req.Mode = pb.GetStatsRequest_MONTHLY
		year := opts.Year
		if year == 0 {
			year = time.Now().Year()
		}
		req.Year = int32(year)

	default:
		req.Mode = pb.GetStatsRequest_DAILY
		today := time.Now().Format("2006-01-02")

		start := opts.StartDate
		end := opts.EndDate
		if start == "" {
			start = today
		}
		if end == "" {
			end = today
		}

		req.StartDate = start
		req.EndDate = end
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.GetStats(rpcCtx, req)
	if err != nil {
		return fmt.Errorf("rpc GetStats failed: %w", err)
	}

	switch req.Mode {
	case pb.GetStatsRequest_DAILY:
		printDailyStatsTable(resp.GetDailyRows())
	case pb.GetStatsRequest_MONTHLY:
		printMonthlyStatsTable(resp.GetMonthlyRows())
	case pb.GetStatsRequest_VERBOSE:
		printVerboseStatsTable(resp.GetVerboseRows())
	default:
		return fmt.Errorf("unsupported response mode: %v", req.Mode)
	}

	return nil
}

// printDailyStatsTable prints Table A for daily stats.
func printDailyStatsTable(rows []*pb.DailyStatRow) {
	if len(rows) == 0 {
		fmt.Println("No daily stats found for the given date range.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DATE\tROUTE PATH\tLOCAL KEY\tREQUESTS\tPROMPT TOKENS\tCOMPLETION TOKENS\tTOTAL TOKENS\tLAST ACTIVITY")
	for _, r := range rows {
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
			r.GetDate(),
			r.GetRoutePath(),
			r.GetLocalKey(),
			r.GetTotalRequests(),
			r.GetPromptTokens(),
			r.GetCompletionTokens(),
			r.GetTotalTokens(),
			r.GetLastActivity(),
		)
	}
	w.Flush()
}

// printMonthlyStatsTable prints Table C for monthly stats.
func printMonthlyStatsTable(rows []*pb.MonthlyStatRow) {
	if len(rows) == 0 {
		fmt.Println("No monthly stats found for the specified year.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "MONTH\tROUTE PATH\tLOCAL KEY\tREQUESTS\tPROMPT TOKENS\tCOMPLETION TOKENS\tTOTAL TOKENS")
	for _, r := range rows {
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%d\t%d\t%d\t%d\n",
			r.GetMonth(),
			r.GetRoutePath(),
			r.GetLocalKey(),
			r.GetTotalRequests(),
			r.GetPromptTokens(),
			r.GetCompletionTokens(),
			r.GetTotalTokens(),
		)
	}
	w.Flush()
}

// printVerboseStatsTable prints verbose usage logs.
func printVerboseStatsTable(rows []*pb.VerboseStatRow) {
	if len(rows) == 0 {
		fmt.Println("No verbose stats found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tROUTE PATH\tLOCAL KEY\tCLIENT IP\tPROMPT TKNS\tCOMPLETION TKNS\tTOTAL TKNS\tTIMESTAMP")
	for _, r := range rows {
		fmt.Fprintf(
			w,
			"%d\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			r.GetId(),
			r.GetRoutePath(),
			r.GetLocalKey(),
			r.GetClientIp(),
			r.GetPromptTokens(),
			r.GetCompletionTokens(),
			r.GetTotalTokens(),
			r.GetTimestamp(),
		)
	}
	w.Flush()
}
