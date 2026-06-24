package usage

import "time"

// Record represents a granular token billing transaction emitted by the egress data plane.
type Record struct {
	LocalKey         string    `json:"local_key"`
	RoutePath        string    `json:"route_path"`
	ClientIP         string    `json:"client_ip"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CreatedAt        time.Time `json:"created_at"`
}
