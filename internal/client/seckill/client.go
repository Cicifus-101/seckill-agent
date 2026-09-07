package seckill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	path       string
	httpClient *http.Client
}

type GrantCouponByReviewRequest struct {
	StoreID          int64  `json:"store_id"`
	UserID           int64  `json:"user_id"`
	ReviewID         int64  `json:"review_id"`
	OrderNo          string `json:"order_no,omitempty"`
	ProductID        int64  `json:"product_id,omitempty"`
	Rating           int32  `json:"rating"`
	HasImage         bool   `json:"has_image"`
	IsFirstReview    bool   `json:"is_first_review"`
	Scene            string `json:"scene"`
	ActivityID       string `json:"activity_id"`
	PolicyVersion    string `json:"policy_version"`
	EvidenceVersion  int64  `json:"evidence_version,omitempty"`
	CouponTemplateID string `json:"coupon_template_id"`
	IdempotencyKey   string `json:"idempotency_key"`
}

type CampaignContextRequest struct {
	StoreID    int64  `json:"store_id"`
	ActivityID string `json:"activity_id,omitempty"`
	UserID     int64  `json:"user_id"`
	SkuID      int64  `json:"sku_id,omitempty"`
	SpuID      int64  `json:"spu_id,omitempty"`
}

type CampaignContext struct {
	ActivityID          string `json:"activity_id"`
	StoreID             int64  `json:"store_id"`
	Status              string `json:"status"`
	PolicyVersion       string `json:"policy_version"`
	CouponTemplateID    string `json:"coupon_template_id"`
	RemainingStock      int64  `json:"remaining_stock"`
	RemainingBudgetCent int64  `json:"remaining_budget_cent"`
	UserGrantLimit      int32  `json:"user_grant_limit"`
	UserGrantedCount    int32  `json:"user_granted_count"`
	ProductEligible     bool   `json:"product_eligible"`
	UserEligible        bool   `json:"user_eligible"`
}

type GrantCouponByReviewReply struct {
	Success    bool           `json:"success"`
	Message    string         `json:"message"`
	Coupon     map[string]any `json:"coupon,omitempty"`
	Duplicated bool           `json:"duplicated"`
}

func NewClient(baseURL string, path string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if strings.TrimSpace(path) == "" {
		path = "/api/v1/seckill/coupon/review/grant"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		path:    path,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) GrantCouponByReview(ctx context.Context, req GrantCouponByReviewRequest) (*GrantCouponByReviewReply, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("seckill base_url is empty")
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal grant coupon request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.path, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("create grant coupon request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call seckill grant coupon: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read seckill grant coupon response: %w", err)
	}
	if httpResp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("seckill grant coupon returned status %d: %s", httpResp.StatusCode, string(body))
	}

	var direct GrantCouponByReviewReply
	if err := json.Unmarshal(body, &direct); err == nil && (direct.Success || direct.Message != "" || direct.Coupon != nil) {
		return &direct, nil
	}

	var enveloped struct {
		Code    int                      `json:"code"`
		Message string                   `json:"message"`
		Data    GrantCouponByReviewReply `json:"data"`
	}
	if err := json.Unmarshal(body, &enveloped); err != nil {
		return nil, fmt.Errorf("unmarshal seckill grant coupon response: %w", err)
	}
	if enveloped.Code != 0 {
		return nil, fmt.Errorf("seckill grant coupon api error: code=%d message=%s", enveloped.Code, enveloped.Message)
	}
	return &enveloped.Data, nil
}

func (c *Client) GetCampaignContext(ctx context.Context, req CampaignContextRequest) (*CampaignContext, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("seckill base_url is empty")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/seckill/coupon/activity/context", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call campaign context: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("campaign context returned status %d: %s", resp.StatusCode, string(raw))
	}
	var direct CampaignContext
	if err := json.Unmarshal(raw, &direct); err == nil && direct.ActivityID != "" {
		return &direct, nil
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    CampaignContext `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("campaign context api error: %s", envelope.Message)
	}
	return &envelope.Data, nil
}
