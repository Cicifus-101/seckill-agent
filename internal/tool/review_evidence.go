package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"seckill-agent/internal/client/reviewservice"
)

type ReviewEvidenceTool struct {
	client *reviewservice.Client
}

type ReviewEvidenceInput struct {
	ReviewID int64 `json:"review_id"`
}

func NewReviewEvidenceTool(client *reviewservice.Client) *ReviewEvidenceTool {
	return &ReviewEvidenceTool{client: client}
}

func (t *ReviewEvidenceTool) Info() Info {
	return Info{
		Name:        "get_review_evidence",
		Description: "获取评价事实证据，包含评分、内容、回复、申诉与用户评价画像，用于Agent第一步理解事实。",
		InputSchema: `{"review_id":1001}`,
	}
}

func (t *ReviewEvidenceTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req ReviewEvidenceInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid review evidence input: %w", err)
	}
	if req.ReviewID <= 0 {
		return "", fmt.Errorf("review_id must be greater than 0")
	}

	start := time.Now()
	resp, err := t.client.GetReviewEvidence(ctx, req.ReviewID)
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"tool":       t.Info().Name,
		"source":     "review-service",
		"latency_ms": time.Since(start).Milliseconds(),
		"review_id":  req.ReviewID,
		"evidence":   resp.Evidence,
	}

	data, _ := json.Marshal(payload)
	return string(data), nil
}
