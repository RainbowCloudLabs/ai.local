package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/daneshih1125/ai.local/internal/apml"
	"github.com/daneshih1125/ai.local/internal/keystore"
	"github.com/daneshih1125/ai.local/internal/usage"
	pb "github.com/daneshih1125/ai.local/proto"
)

// AdminHandler coordinates control plane service operations by bridging
// static APML rules with the dynamic in-memory credential keystore.
type AdminHandler struct {
	pb.UnimplementedAdminServiceServer
	cfg   *apml.APMLConfig
	store *keystore.Store
	usage *usage.UsageStore
}

// NewAdminHandler instantiates a fresh administrative dispatch handler.
func NewAdminHandler(cfg *apml.APMLConfig, store *keystore.Store, usageStore *usage.UsageStore) *AdminHandler {
	return &AdminHandler{
		cfg:   cfg,
		store: store,
		usage: usageStore,
	}
}

// ListRoutes flushes out all currently active L7 routing definitions.
func (h *AdminHandler) ListRoutes(ctx context.Context, req *pb.ListRoutesRequest) (*pb.ListRoutesResponse, error) {
	var resp pb.ListRoutesResponse
	for uri, route := range h.cfg.Routes {
		quotaName := route.Quota
		if quotaName == "" {
			quotaName = "None"
		}
		resp.Routes = append(resp.Routes, &pb.RouteInfo{
			Uri:      uri,
			Quota:    quotaName,
			Provider: route.Provider,
		})
	}
	return &resp, nil
}

// ListQuotas aggregates active structural metrics and audits their real-time application status.
func (h *AdminHandler) ListQuotas(ctx context.Context, req *pb.ListQuotasRequest) (*pb.ListQuotasResponse, error) {
	var resp pb.ListQuotasResponse

	// Cross-reference current routes to dynamically determine if a quota is APPLIED
	appliedQuotas := make(map[string]bool)
	for _, route := range h.cfg.Routes {
		if route.Quota != "" {
			appliedQuotas[route.Quota] = true
		}
	}

	for name, q := range h.cfg.Quotas {
		status := "IDLE"
		if appliedQuotas[name] {
			status = "APPLIED"
		}

		modeDisplay := q.Mode
		if q.Daily == 0 && q.Monthly == 0 {
			modeDisplay = "unlimited"
		}

		resp.Quotas = append(resp.Quotas, &pb.QuotaInfo{
			Name:    name,
			Daily:   q.Daily,
			Monthly: q.Monthly,
			Mode:    modeDisplay,
			Status:  status,
		})
	}
	return &resp, nil
}

// ListKeys copies internal credential maps safely to expose secure metadata via the administrative console.
func (h *AdminHandler) ListKeys(ctx context.Context, req *pb.ListKeysRequest) (*pb.ListKeysResponse, error) {
	records := h.store.ListKeys()
	var resp pb.ListKeysResponse

	for _, r := range records {
		resp.Keys = append(resp.Keys, &pb.KeyInfo{
			Uuid:        r.UUID,
			Alias:       r.Alias,
			Route:       r.Route,
			RealKey:     keystore.MaskKey(r.RealKey), // Ensure upstream token stays hidden
			InternalKey: r.InternalKey,
		})
	}
	return &resp, nil
}

// AddKey verifies context limits and injects a newly encrypted authorization token into the active pool.
func (h *AdminHandler) AddKey(ctx context.Context, req *pb.AddKeyRequest) (*pb.AddKeyResponse, error) {
	// Strategic defense: Ensure the target configuration route exists before allocating space
	if _, routeExists := h.cfg.Routes[req.Route]; !routeExists {
		return nil, fmt.Errorf("target context route %q does not exist within current configurations", req.Route)
	}

	if strings.TrimSpace(req.RealKey) == "" {
		return nil, fmt.Errorf("upstream authorization token payload cannot be empty")
	}

	record, err := h.store.AddKey(req.Route, req.RealKey, req.Alias)
	if err != nil {
		return nil, fmt.Errorf("failed to append secure telemetry entry: %w", err)
	}

	return &pb.AddKeyResponse{InternalKey: record.InternalKey}, nil
}

// DeleteKey atomically purges an operational credential reference by its secure UUID mapping.
func (h *AdminHandler) DeleteKey(ctx context.Context, req *pb.DeleteKeyRequest) (*pb.DeleteKeyResponse, error) {
	success := h.store.DeleteKey(req.Uuid)
	return &pb.DeleteKeyResponse{Success: success}, nil
}

// GetStats returns telemetry statistics in DAILY, MONTHLY, or VERBOSE mode.
func (h *AdminHandler) GetStats(ctx context.Context, req *pb.GetStatsRequest) (*pb.GetStatsResponse, error) {
	if h.usage == nil {
		return nil, fmt.Errorf("usage store is not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	resp := &pb.GetStatsResponse{}

	switch req.Mode {
	case pb.GetStatsRequest_DAILY:
		startDate := strings.TrimSpace(req.StartDate)
		endDate := strings.TrimSpace(req.EndDate)

		// Default to today's date when caller does not provide range.
		if startDate == "" || endDate == "" {
			today := time.Now().Format("2006-01-02")
			if startDate == "" {
				startDate = today
			}
			if endDate == "" {
				endDate = today
			}
		}

		rows, err := h.usage.GetDailyStats(startDate, endDate)
		if err != nil {
			return nil, fmt.Errorf("get daily stats: %w", err)
		}
		for _, r := range rows {
			resp.DailyRows = append(resp.DailyRows, &pb.DailyStatRow{
				Date:             r.Date,
				RoutePath:        r.RoutePath,
				LocalKey:         r.LocalKey,
				TotalRequests:    r.TotalRequests,
				PromptTokens:     r.PromptTokens,
				CompletionTokens: r.CompletionTokens,
				TotalTokens:      r.TotalTokens,
				LastActivity:     r.LastActivity,
			})
		}
	case pb.GetStatsRequest_MONTHLY:
		year := int(req.Year)
		if year <= 0 {
			year = time.Now().Year()
		}

		rows, err := h.usage.GetMonthlyStats(year)
		if err != nil {
			return nil, fmt.Errorf("get monthly stats: %w", err)
		}
		for _, r := range rows {
			resp.MonthlyRows = append(resp.MonthlyRows, &pb.MonthlyStatRow{
				Month:            r.Month,
				RoutePath:        r.RoutePath,
				LocalKey:         r.LocalKey,
				TotalRequests:    r.TotalRequests,
				PromptTokens:     r.PromptTokens,
				CompletionTokens: r.CompletionTokens,
				TotalTokens:      r.TotalTokens,
			})
		}

	case pb.GetStatsRequest_VERBOSE:
		limit := int(req.MaxCount)
		if limit <= 0 {
			limit = 100
		}

		rows, err := h.usage.GetVerboseLogs(limit)
		if err != nil {
			return nil, fmt.Errorf("get verbose logs: %w", err)
		}
		for _, r := range rows {
			resp.VerboseRows = append(resp.VerboseRows, &pb.VerboseStatRow{
				Id:               r.ID,
				Timestamp:        r.Timestamp,
				LocalKey:         r.LocalKey,
				RoutePath:        r.RoutePath,
				ClientIp:         r.ClientIP,
				PromptTokens:     r.PromptTokens,
				CompletionTokens: r.CompletionTokens,
				TotalTokens:      r.TotalTokens,
			})
		}

	default:
		return nil, fmt.Errorf("unsupported stats mode: %v", req.Mode)
	}

	return resp, nil
}
