package reviewjob

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
	httpClient *http.Client
}

type ReviewStatsRequest struct {
	ReviewID   int64 `json:"review_id"`
	StoreID    int64 `json:"store_id"`
	UserID     int64 `json:"user_id"`
	SkuID      int64 `json:"sku_id"`
	SpuID      int64 `json:"spu_id"`
	WindowDays int   `json:"window_days"`
}

type ReviewStatsResponse struct {
	ReviewID                 int64    `json:"review_id"`
	StoreNegativeCount       int64    `json:"store_negative_count"`
	StoreNegativeCountWindow int64    `json:"store_negative_count_window"`
	SkuNegativeCountWindow   int64    `json:"sku_negative_count_window"`
	UserLowScoreCount30d     int64    `json:"user_low_score_count_30d"`
	UnrepliedNegativeCount   int64    `json:"unreplied_negative_count"`
	LogisticsIssueCount      int64    `json:"logistics_issue_count"`
	ServiceIssueCount        int64    `json:"service_issue_count"`
	LogisticsIssueRatio      float64  `json:"logistics_issue_ratio"`
	ServiceIssueRatio        float64  `json:"service_issue_ratio"`
	RiskLevel                string   `json:"risk_level"`
	SignalTags               []string `json:"signal_tags"`
}

type RAGRetrieveRequest struct {
	QueryText      string   `json:"query_text"`
	RetrievalMode  string   `json:"retrieval_mode"`
	RetrievalScope string   `json:"retrieval_scope"`
	ReviewID       int64    `json:"review_id"`
	StoreID        int64    `json:"store_id"`
	UserID         int64    `json:"user_id"`
	SkuID          int64    `json:"sku_id"`
	SpuID          int64    `json:"spu_id"`
	ActivityID     string   `json:"activity_id"`
	PolicyVersion  string   `json:"policy_version"`
	TopK           int32    `json:"top_k"`
	ChunkTypes     []string `json:"chunk_types"`
	Aspects        []string `json:"aspects"`
}

type RAGRetrieveResponse struct {
	Chunks []*RAGChunkDTO `json:"chunks"`
}

type StoreDecisionRequest struct {
	DecisionID       string         `json:"decision_id"`
	ReviewID         int64          `json:"review_id"`
	StoreID          int64          `json:"store_id"`
	SkuID            int64          `json:"sku_id,omitempty"`
	SpuID            int64          `json:"spu_id,omitempty"`
	EvidenceVersion  int64          `json:"evidence_version,omitempty"`
	DecisionRevision int64          `json:"decision_revision,omitempty"`
	UserID           int64          `json:"user_id"`
	Score            int32          `json:"score"`
	Content          string         `json:"content"`
	Scene            string         `json:"scene"`
	Action           string         `json:"action"`
	RiskLevel        string         `json:"risk_level"`
	CouponGranted    bool           `json:"coupon_granted"`
	CouponID         string         `json:"coupon_id"`
	ActivityID       string         `json:"activity_id"`
	PolicyVersion    string         `json:"policy_version"`
	ExecutionStatus  string         `json:"execution_status"`
	ReasonCodes      []string       `json:"reason_codes"`
	Reason           string         `json:"reason"`
	TraceID          string         `json:"trace_id"`
	EvidenceIDs      []string       `json:"evidence_ids"`
	LLMOutput        map[string]any `json:"llm_output"`
}

type StoreDecisionResponse struct {
	DecisionID string `json:"decision_id"`
	Stored     bool   `json:"stored"`
}

type RAGChunkDTO struct {
	ChunkID         string         `json:"chunk_id"`
	ReviewID        int64          `json:"review_id"`
	StoreID         int64          `json:"store_id"`
	UserID          int64          `json:"user_id"`
	SkuID           int64          `json:"sku_id"`
	SpuID           int64          `json:"spu_id"`
	ChunkType       string         `json:"chunk_type"`
	Aspect          string         `json:"aspect"`
	Text            string         `json:"text"`
	Metadata        map[string]any `json:"metadata"`
	EvidenceVersion int64          `json:"evidence_version"`
	CreatedAt       time.Time      `json:"created_at"`
}

type envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    T      `json:"data"`
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) GetReviewStats(ctx context.Context, req ReviewStatsRequest) (*ReviewStatsResponse, error) {
	if req.WindowDays <= 0 {
		req.WindowDays = 7
	}

	var out envelope[ReviewStatsResponse]
	if err := c.postJSON(ctx, "/v1/agent/review/search/stats", req, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("review-job stats api error: code=%d message=%s", out.Code, out.Message)
	}
	return &out.Data, nil
}

func (c *Client) RetrieveRAG(ctx context.Context, req RAGRetrieveRequest) (*RAGRetrieveResponse, error) {
	if req.TopK <= 0 {
		req.TopK = 5
	}

	var out envelope[RAGRetrieveResponse]
	if err := c.postJSON(ctx, "/v1/agent/rag/retrieve", req, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("review-job rag api error: code=%d message=%s", out.Code, out.Message)
	}
	return &out.Data, nil
}

func (c *Client) StoreDecision(ctx context.Context, req StoreDecisionRequest) (*StoreDecisionResponse, error) {
	var out envelope[StoreDecisionResponse]
	if err := c.postJSON(ctx, "/v1/agent/review/decision/store", req, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("review-job store decision api error: code=%d message=%s", out.Code, out.Message)
	}
	return &out.Data, nil
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any, out any) error {
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(reqBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call review-job: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read review-job response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("review-job returned status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unmarshal review-job response: %w", err)
	}

	return nil
}
