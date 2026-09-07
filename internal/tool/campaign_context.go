package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"seckill-agent/internal/client/seckill"
)

type CampaignContextTool struct{ client *seckill.Client }

type CampaignContextInput struct {
	StoreID    int64  `json:"store_id"`
	ActivityID string `json:"activity_id,omitempty"`
	UserID     int64  `json:"user_id"`
	SkuID      int64  `json:"sku_id,omitempty"`
	SpuID      int64  `json:"spu_id,omitempty"`
}

func NewCampaignContextTool(client *seckill.Client) *CampaignContextTool {
	return &CampaignContextTool{client: client}
}

func (t *CampaignContextTool) Info() Info {
	return Info{Name: "get_campaign_context", Description: "查询店铺活动、商品资格、用户领取上限、剩余库存和预算；这是发券的硬约束。", InputSchema: `{"store_id":1001,"activity_id":"A20260714","user_id":2001,"sku_id":3001,"spu_id":4001}`}
}

func (t *CampaignContextTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req CampaignContextInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid campaign context input: %w", err)
	}
	if req.StoreID <= 0 || req.UserID <= 0 {
		return "", fmt.Errorf("campaign context requires store_id and user_id")
	}
	if t.client == nil {
		return "", fmt.Errorf("campaign context client is not configured")
	}
	start := time.Now()
	contextData, err := t.client.GetCampaignContext(ctx, seckill.CampaignContextRequest{StoreID: req.StoreID, ActivityID: req.ActivityID, UserID: req.UserID, SkuID: req.SkuID, SpuID: req.SpuID})
	if err != nil {
		return "", err
	}
	payload := map[string]any{"tool": t.Info().Name, "source": "seckill-service", "latency_ms": time.Since(start).Milliseconds(), "request": req, "campaign": contextData}
	raw, _ := json.Marshal(payload)
	return string(raw), nil
}
