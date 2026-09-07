package reviewservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type GetReviewEvidenceResponse struct {
	Evidence *ReviewEvidence `json:"evidence"`
}

type ReviewEvidence struct {
	ReviewID         int64   `json:"reviewId"`
	OrderID          int64   `json:"orderId"`
	StoreID          int64   `json:"storeId"`
	UserID           int64   `json:"userId"`
	SkuID            int64   `json:"skuId"`
	SpuID            int64   `json:"spuId"`
	Score            int32   `json:"score"`
	ServiceScore     int32   `json:"serviceScore"`
	ExpressScore     int32   `json:"expressScore"`
	Content          string  `json:"content"`
	PicInfo          string  `json:"picInfo"`
	VideoInfo        string  `json:"videoInfo"`
	HasMedia         bool    `json:"hasMedia"`
	ReviewStatus     int32   `json:"reviewStatus"`
	AuditPassed      bool    `json:"auditPassed"`
	IsHidden         bool    `json:"isHidden"`
	HasReply         bool    `json:"hasReply"`
	ReplyContent     string  `json:"replyContent"`
	HasAppeal        bool    `json:"hasAppeal"`
	AppealReason     string  `json:"appealReason"`
	AppealContent    string  `json:"appealContent"`
	AppealStatus     int32   `json:"appealStatus"`
	IsFirstReview    bool    `json:"isFirstReview"`
	UserTotalReviews int64   `json:"userTotalReviews"`
	UserAvgScore     float64 `json:"userAvgScore"`
	UserLowScoreRate float64 `json:"userLowScoreRate"`
	ReviewCreatedAt  string  `json:"reviewCreatedAt"`
	ReviewUpdatedAt  string  `json:"reviewUpdatedAt"`
	EvidenceVersion  int64   `json:"evidenceVersion"`
}

type reviewEvidenceWireResponse struct {
	Evidence *reviewEvidenceWire `json:"evidence"`
}

type reviewEvidenceWire struct {
	ReviewID         jsonInt64 `json:"reviewId"`
	OrderID          jsonInt64 `json:"orderId"`
	StoreID          jsonInt64 `json:"storeId"`
	UserID           jsonInt64 `json:"userId"`
	SkuID            jsonInt64 `json:"skuId"`
	SpuID            jsonInt64 `json:"spuId"`
	Score            int32     `json:"score"`
	ServiceScore     int32     `json:"serviceScore"`
	ExpressScore     int32     `json:"expressScore"`
	Content          string    `json:"content"`
	PicInfo          string    `json:"picInfo"`
	VideoInfo        string    `json:"videoInfo"`
	HasMedia         bool      `json:"hasMedia"`
	ReviewStatus     int32     `json:"reviewStatus"`
	AuditPassed      bool      `json:"auditPassed"`
	IsHidden         bool      `json:"isHidden"`
	HasReply         bool      `json:"hasReply"`
	ReplyContent     string    `json:"replyContent"`
	HasAppeal        bool      `json:"hasAppeal"`
	AppealReason     string    `json:"appealReason"`
	AppealContent    string    `json:"appealContent"`
	AppealStatus     int32     `json:"appealStatus"`
	IsFirstReview    bool      `json:"isFirstReview"`
	UserTotalReviews jsonInt64 `json:"userTotalReviews"`
	UserAvgScore     float64   `json:"userAvgScore"`
	UserLowScoreRate float64   `json:"userLowScoreRate"`
	ReviewCreatedAt  string    `json:"reviewCreatedAt"`
	ReviewUpdatedAt  string    `json:"reviewUpdatedAt"`
	EvidenceVersion  jsonInt64 `json:"evidenceVersion"`
}

type jsonInt64 int64

func (j *jsonInt64) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*j = 0
		return nil
	}

	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if strings.TrimSpace(s) == "" {
			*j = 0
			return nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parse int64 from string %q: %w", s, err)
		}
		*j = jsonInt64(v)
		return nil
	}

	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	v, err := n.Int64()
	if err != nil {
		return fmt.Errorf("parse int64 from number %q: %w", n.String(), err)
	}
	*j = jsonInt64(v)
	return nil
}

func (w *reviewEvidenceWire) toDomain() *ReviewEvidence {
	if w == nil {
		return nil
	}

	return &ReviewEvidence{
		ReviewID:         int64(w.ReviewID),
		OrderID:          int64(w.OrderID),
		StoreID:          int64(w.StoreID),
		UserID:           int64(w.UserID),
		SkuID:            int64(w.SkuID),
		SpuID:            int64(w.SpuID),
		Score:            w.Score,
		ServiceScore:     w.ServiceScore,
		ExpressScore:     w.ExpressScore,
		Content:          w.Content,
		PicInfo:          w.PicInfo,
		VideoInfo:        w.VideoInfo,
		HasMedia:         w.HasMedia,
		ReviewStatus:     w.ReviewStatus,
		AuditPassed:      w.AuditPassed,
		IsHidden:         w.IsHidden,
		HasReply:         w.HasReply,
		ReplyContent:     w.ReplyContent,
		HasAppeal:        w.HasAppeal,
		AppealReason:     w.AppealReason,
		AppealContent:    w.AppealContent,
		AppealStatus:     w.AppealStatus,
		IsFirstReview:    w.IsFirstReview,
		UserTotalReviews: int64(w.UserTotalReviews),
		UserAvgScore:     w.UserAvgScore,
		UserLowScoreRate: w.UserLowScoreRate,
		ReviewCreatedAt:  w.ReviewCreatedAt,
		ReviewUpdatedAt:  w.ReviewUpdatedAt,
		EvidenceVersion:  int64(w.EvidenceVersion),
	}
}

func (r *GetReviewEvidenceResponse) UnmarshalJSON(data []byte) error {
	var wire reviewEvidenceWireResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.Evidence = wire.Evidence.toDomain()
	return nil
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

func (c *Client) GetReviewEvidence(ctx context.Context, reviewID int64) (*GetReviewEvidenceResponse, error) {
	if reviewID <= 0 {
		return nil, fmt.Errorf("review_id must be greater than 0")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/agent/review/evidence/%d", c.baseURL, reviewID), nil)
	if err != nil {
		return nil, fmt.Errorf("create review evidence request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call review-service: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read review-service response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("review-service returned status %d: %s", resp.StatusCode, string(body))
	}

	var out GetReviewEvidenceResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unmarshal review-service response: %w", err)
	}
	if out.Evidence == nil {
		return nil, fmt.Errorf("review evidence is empty")
	}

	return &out, nil
}
