package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"seckill-agent/internal/client/reviewjob"
)

type ReviewStatsTool struct {
	client *reviewjob.Client
}

type ReviewStatsInput struct {
	ReviewID   int64 `json:"review_id"`
	StoreID    int64 `json:"store_id"`
	UserID     int64 `json:"user_id"`
	SkuID      int64 `json:"sku_id"`
	SpuID      int64 `json:"spu_id"`
	WindowDays int   `json:"window_days"`
}

func NewReviewStatsTool(client *reviewjob.Client) *ReviewStatsTool {
	return &ReviewStatsTool{client: client}
}

func (t *ReviewStatsTool) Info() Info {
	return Info{
		Name:        "get_review_stats",
		Description: "获取ES结构化风险统计，包括差评窗口、未回复差评、物流/服务问题占比和policy tags，用于第二步决策。",
		InputSchema: `{"review_id":1001,"store_id":3001,"user_id":2001,"sku_id":5001,"spu_id":6001,"window_days":7}`,
	}
}

func (t *ReviewStatsTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req ReviewStatsInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid review stats input: %w", err)
	}
	if req.StoreID <= 0 {
		return "", fmt.Errorf("store_id must be greater than 0")
	}
	if req.WindowDays <= 0 {
		req.WindowDays = 7
	}

	start := time.Now()
	resp, err := t.client.GetReviewStats(ctx, reviewjob.ReviewStatsRequest{
		ReviewID:   req.ReviewID,
		StoreID:    req.StoreID,
		UserID:     req.UserID,
		SkuID:      req.SkuID,
		SpuID:      req.SpuID,
		WindowDays: req.WindowDays,
	})
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"tool":       t.Info().Name,
		"source":     "review-job",
		"latency_ms": time.Since(start).Milliseconds(),
		"request":    req,
		"stats":      resp,
	}

	data, _ := json.Marshal(payload)
	return string(data), nil
}
