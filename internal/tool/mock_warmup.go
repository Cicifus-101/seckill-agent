package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type MockWarmupTool struct{}

type WarmupInput struct {
	CampaignID string `json:"campaign_id"`
}

func (t *MockWarmupTool) Info() Info {
	return Info{
		Name:        "trigger_seckill_warmup",
		Description: "触发秒杀预热流程，通知秒杀服务进入预热状态。",
		InputSchema: `{"campaign_id":"campaign_001"}`,
	}
}

func (t *MockWarmupTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req WarmupInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid warmup input: %w", err)
	}
	if req.CampaignID == "" {
		req.CampaignID = "campaign_demo"
	}

	resp := map[string]any{
		"campaign_id": req.CampaignID,
		"status":      "warmup_triggered",
	}
	data, _ := json.Marshal(resp)
	return string(data), nil
}
