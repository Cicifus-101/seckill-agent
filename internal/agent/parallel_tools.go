package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"seckill-agent/internal/contextx"
)

type reviewEvidenceForParallel struct {
	ReviewID int64
	StoreID  int64
	UserID   int64
	SkuID    int64
	SpuID    int64
	Content  string
}

func (a *Agent) runStatsRAGAndCampaignAfterEvidence(ctx context.Context, sessionID string, evidenceObservation string, input ExecutionInput) []Step {
	evidence := parseEvidenceForParallel(evidenceObservation)
	if evidence.ReviewID <= 0 || evidence.StoreID <= 0 {
		return nil
	}

	type result struct {
		toolName    string
		args        map[string]any
		observation string
		err         error
		durationMS  int64
	}

	results := make([]result, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		args := buildStatsArgs(evidence) // 用户、店铺近期差评，商品风险，近7天统计，政策标签
		observation, err, ms := a.runAutoToolObservation(ctx, "get_review_stats", args, fallbackStatsObservation(evidence))
		results[0] = result{toolName: "get_review_stats", args: args, observation: observation, err: err, durationMS: ms}
	}()
	go func() {
		defer wg.Done()
		args := buildRAGArgs(evidence, input) // 已评价为检索文本，在当前店铺和活动内查找相似评价和历史发券决策
		observation, err, ms := a.runAutoToolObservation(ctx, "retrieve_rag", args, fallbackRAGObservation(evidence, input))
		results[1] = result{toolName: "retrieve_rag", args: args, observation: observation, err: err, durationMS: ms}
	}()
	go func() {
		defer wg.Done()
		args := buildCampaignArgs(evidence, input)
		observation, err, ms := a.runAutoToolObservation(ctx, "get_campaign_context", args, fallbackCampaignObservation(evidence, input))
		results[2] = result{toolName: "get_campaign_context", args: args, observation: observation, err: err, durationMS: ms}
	}()
	wg.Wait()

	steps := make([]Step, 0, 2)
	for _, item := range results {
		step := a.appendAutoToolResult(ctx, sessionID, item.toolName, item.args, item.observation, item.durationMS)
		// 处理降级错误
		if item.err != nil {
			step.Error = "degraded: " + item.err.Error()
		} // degraded: connection timeout，将该Step返回，不让整个Agent失败
		steps = append(steps, step)
	}
	return steps
}

func buildCampaignArgs(e reviewEvidenceForParallel, input ExecutionInput) map[string]any {
	return map[string]any{"store_id": e.StoreID, "activity_id": input.ActivityID, "user_id": e.UserID, "sku_id": e.SkuID, "spu_id": e.SpuID}
}

// 活动不可用时，构造降级结果（避免外部活动服务故障时误发券）
func fallbackCampaignObservation(e reviewEvidenceForParallel, input ExecutionInput) map[string]any {
	return map[string]any{"source": "fallback", "tool": "get_campaign_context",
		"request": buildCampaignArgs(e, input),
		"campaign": map[string]any{"activity_id": input.ActivityID,
			"status": "UNAVAILABLE", "product_eligible": false, "user_eligible": false}}
}

func (a *Agent) runAutoToolObservation(ctx context.Context, toolName string, args map[string]any, fallback map[string]any) (string, error, int64) {
	start := time.Now()
	argsBytes, _ := json.Marshal(args)
	t, ok := a.registry.Get(toolName)
	// 降级，防止Agent因为RAG或ES临时不可用崩溃
	if !ok {
		fallback["degraded"] = true
		fallback["error"] = "tool not found: " + toolName
		fallbackBytes, _ := json.Marshal(fallback)
		return string(fallbackBytes), context.Canceled, time.Since(start).Milliseconds()
	}
	toolCtx, cancel := withTimeoutIfShorter(ctx, a.toolTimeout) // 取 父context的剩余时间 和 工具超时时间更短的那个
	observation, err := t.Execute(toolCtx, argsBytes)
	cancel()
	if err != nil {
		fallback["degraded"] = true
		fallback["error"] = err.Error()
		fallbackBytes, _ := json.Marshal(fallback)
		return string(fallbackBytes), err, time.Since(start).Milliseconds()
	}
	return observation, nil, time.Since(start).Milliseconds()
}

// 将自动工具结果转换成标准的step，写入memory
func (a *Agent) appendAutoToolResult(ctx context.Context, sessionID string, toolName string, args map[string]any, observation string, durationMS int64) Step {
	argsBytes, _ := json.Marshal(args)
	step := Step{
		Step:        len(a.workflowHistory(ctx, sessionID)) + 1,
		ToolName:    toolName,
		Arguments:   string(argsBytes),
		Observation: observation,
		DurationMS:  durationMS,
	}
	a.appendStep(ctx, sessionID, contextx.StepRecord{
		Step:      step.Step,
		ToolName:  toolName,
		Action:    "tool_call",
		Arguments: step.Arguments,
		Output:    observation,
	})
	return step
}

// 将评价证据工具返回的JSON解析成并行查询需要的最小结构
func parseEvidenceForParallel(raw string) reviewEvidenceForParallel {
	var payload map[string]any
	_ = json.Unmarshal([]byte(raw), &payload)
	evidence, _ := payload["evidence"].(map[string]any)
	return reviewEvidenceForParallel{
		ReviewID: firstInt64(0, anyInt64(evidence["reviewId"]), anyInt64(evidence["review_id"])),
		StoreID:  firstInt64(0, anyInt64(evidence["storeId"]), anyInt64(evidence["store_id"])),
		UserID:   firstInt64(0, anyInt64(evidence["userId"]), anyInt64(evidence["user_id"])),
		SkuID:    firstInt64(0, anyInt64(evidence["skuId"]), anyInt64(evidence["sku_id"])),
		SpuID:    firstInt64(0, anyInt64(evidence["spuId"]), anyInt64(evidence["spu_id"])),
		Content:  anyString(evidence["content"]),
	}
}

// 构造统计参数
func buildStatsArgs(e reviewEvidenceForParallel) map[string]any {
	return map[string]any{"review_id": e.ReviewID, "store_id": e.StoreID, "user_id": e.UserID, "sku_id": e.SkuID, "spu_id": e.SpuID, "window_days": 7}
}

func buildRAGArgs(e reviewEvidenceForParallel, input ExecutionInput) map[string]any {
	return map[string]any{
		"query_text":     strings.TrimSpace(e.Content), // 评价内容作为检索文本，找相似文本和历史决策
		"review_id":      e.ReviewID,
		"store_id":       e.StoreID,
		"user_id":        e.UserID,
		"sku_id":         e.SkuID,
		"spu_id":         e.SpuID,
		"activity_id":    input.ActivityID,
		"policy_version": input.PolicyVersion,
		"top_k":          10,
		"chunk_types":    []string{"review_summary", "review_aspect", "coupon_decision"}, // 评价维度分析
		"aspects":        inferRAGAspects(e.Content),                                     // 根据评价内容推断检索维度
	}
}

func inferRAGAspects(content string) []string {
	aspects := []string{"SUMMARY", "DECISION"}
	add := func(v string) {
		for _, existing := range aspects {
			if existing == v {
				return
			}
		}
		aspects = append(aspects, v)
	}
	for keyword, aspect := range map[string]string{"物流": "LOGISTICS", "快递": "LOGISTICS", "客服": "SERVICE", "服务": "SERVICE", "质量": "QUALITY", "破损": "QUALITY", "价格": "PRICE", "优惠": "PRICE", "复购": "REPEAT", "再买": "REPEAT", "推荐": "RECOMMEND"} {
		if strings.Contains(content, keyword) {
			add(aspect)
		}
	}
	return aspects
}

func fallbackStatsObservation(e reviewEvidenceForParallel) map[string]any {
	return map[string]any{"source": "fallback", "tool": "get_review_stats", "request": buildStatsArgs(e), "stats": map[string]any{"review_id": e.ReviewID, "risk_level": "unknown", "policy_tags": []string{"stats_unavailable"}}}
}

func fallbackRAGObservation(e reviewEvidenceForParallel, input ExecutionInput) map[string]any {
	return map[string]any{"source": "fallback", "tool": "retrieve_rag", "request": buildRAGArgs(e, input), "rag": map[string]any{"chunks": []any{}}}
}
