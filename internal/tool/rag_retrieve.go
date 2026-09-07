package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"seckill-agent/internal/client/reviewjob"
)

type RAGRetrieveTool struct {
	client *reviewjob.Client
}

type RAGRetrieveInput struct {
	QueryText     string   `json:"query_text"`
	ReviewID      int64    `json:"review_id"`
	StoreID       int64    `json:"store_id"`
	UserID        int64    `json:"user_id"`
	SkuID         int64    `json:"sku_id"`
	SpuID         int64    `json:"spu_id"`
	ActivityID    string   `json:"activity_id"`
	PolicyVersion string   `json:"policy_version"`
	TopK          int32    `json:"top_k"`
	ChunkTypes    []string `json:"chunk_types"`
	Aspects       []string `json:"aspects"`
}

func NewRAGRetrieveTool(client *reviewjob.Client) *RAGRetrieveTool {
	return &RAGRetrieveTool{client: client}
}

func (t *RAGRetrieveTool) Info() Info {
	return Info{
		Name:        "retrieve_rag",
		Description: "召回RAG历史案例、相似评价和历史决策chunk，用于第三步让Agent参考历史经验；query_text可为空，工具会自动构造回退检索词。",
		InputSchema: `{"query_text":"物流太慢，客服态度差","review_id":1001,"store_id":3001,"activity_id":"campaign-2026-07","policy_version":"v2","top_k":10,"chunk_types":["review_summary","review_aspect","coupon_decision"],"aspects":["SUMMARY","DECISION","LOGISTICS","SERVICE"]}`,
	}
}

func (t *RAGRetrieveTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req RAGRetrieveInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid rag retrieve input: %w", err)
	}
	if req.StoreID <= 0 {
		return "", fmt.Errorf("store_id must be greater than 0")
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	if req.TopK < 10 {
		req.TopK = 10
	}
	if req.TopK > 20 {
		req.TopK = 20
	}
	if strings.TrimSpace(req.QueryText) == "" {
		req.QueryText = buildFallbackRAGQuery(req)
	}
	if len(req.ChunkTypes) == 0 {
		req.ChunkTypes = defaultRAGChunkTypes()
	}
	if len(req.Aspects) == 0 {
		req.Aspects = defaultRAGAspects()
	}
	req.ChunkTypes = normalizeRAGChunkTypes(req.ChunkTypes)
	req.Aspects = normalizeRAGAspects(req.Aspects)

	start := time.Now()
	resp, err := t.client.RetrieveRAG(ctx, reviewjob.RAGRetrieveRequest{
		QueryText:      req.QueryText,
		RetrievalMode:  "decision_support",
		RetrievalScope: "store",
		ReviewID:       req.ReviewID,
		StoreID:        req.StoreID,
		UserID:         req.UserID,
		SkuID:          req.SkuID,
		SpuID:          req.SpuID,
		ActivityID:     req.ActivityID,
		PolicyVersion:  req.PolicyVersion,
		TopK:           req.TopK,
		ChunkTypes:     req.ChunkTypes,
		Aspects:        req.Aspects,
	})
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"tool":       t.Info().Name,
		"source":     "review-job",
		"latency_ms": time.Since(start).Milliseconds(),
		"request":    req,
		"rag": map[string]any{
			"chunks": compactRAGChunks(resp.Chunks),
		},
	}

	data, _ := json.Marshal(payload)
	return string(data), nil
}

func buildFallbackRAGQuery(req RAGRetrieveInput) string {
	return fmt.Sprintf(
		"review_id=%d store_id=%d 优惠券策略 历史案例 评价事实 ES统计 RAG召回",
		req.ReviewID,
		req.StoreID,
	)
}

func defaultRAGChunkTypes() []string {
	return []string{"review_summary", "review_aspect", "coupon_decision"}
}

func defaultRAGAspects() []string {
	return []string{"SUMMARY", "DECISION", "LOGISTICS", "SERVICE", "QUALITY", "PRICE", "REPEAT", "RECOMMEND"}
}

func normalizeRAGChunkTypes(in []string) []string {
	return appendMissingUpperish(in, []string{"review_summary", "review_aspect", "coupon_decision"})
}

func normalizeRAGAspects(in []string) []string {
	out := make([]string, 0, len(in)+2)
	seen := map[string]bool{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.EqualFold(item, "REPURCHASE") {
			item = "REPEAT"
		}
		key := strings.ToUpper(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return appendMissingUpperish(out, []string{"SUMMARY", "DECISION"})
}

func appendMissingUpperish(in []string, required []string) []string {
	out := make([]string, 0, len(in)+len(required))
	seen := map[string]bool{}
	for _, item := range in {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToUpper(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trimmed)
	}
	for _, item := range required {
		key := strings.ToUpper(item)
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	return out
}

type compactRAGChunk struct {
	ChunkID   string `json:"chunk_id"`
	ReviewID  int64  `json:"review_id"`
	StoreID   int64  `json:"store_id"`
	ChunkType string `json:"chunk_type"`
	Aspect    string `json:"aspect"`
	Text      string `json:"text"`
}

func compactRAGChunks(chunks []*reviewjob.RAGChunkDTO) []compactRAGChunk {
	out := make([]compactRAGChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		out = append(out, compactRAGChunk{
			ChunkID:   chunk.ChunkID,
			ReviewID:  chunk.ReviewID,
			StoreID:   chunk.StoreID,
			ChunkType: chunk.ChunkType,
			Aspect:    chunk.Aspect,
			Text:      chunk.Text,
		})
	}
	return out
}
