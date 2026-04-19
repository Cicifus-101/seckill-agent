package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type MockCouponPlanTool struct{}

type CouponPlanInput struct {
	UserCount int     `json:"user_count"`
	Discount  float64 `json:"discount"`
}

func (t *MockCouponPlanTool) Info() Info {
	return Info{
		Name:        "plan_coupon_quota",
		Description: "根据候选用户数量和折扣，计算秒杀券配额方案。",
		InputSchema: `{"user_count":100,"discount":0.8}`,
	}
}

func (t *MockCouponPlanTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req CouponPlanInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid coupon plan input: %w", err)
	}
	if req.UserCount <= 0 {
		return "", fmt.Errorf("user_count must be greater than 0")
	}
	if req.Discount <= 0 || req.Discount >= 1 {
		req.Discount = 0.8
	}

	resp := map[string]any{
		"target_user_count": req.UserCount,
		"discount":          req.Discount,
		"coupon_count":      req.UserCount,
		"strategy":          "one coupon per target user",
	}
	data, _ := json.Marshal(resp)
	return string(data), nil
}
