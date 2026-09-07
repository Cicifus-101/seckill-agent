package agent

import (
	"encoding/json"
	"seckill-agent/internal/contextx"
	"strings"
)

// Guard 使用的最小事实集合（evidence和rag）
type decisionFacts struct {
	Score      int32 //评价分数
	HasAppeal  bool
	Content    string // 评价正文
	RAGAspects []string
}

func applyDecisionGuard(decision Decision, history []contextx.StepRecord, policy *CouponPolicyProvider) Decision {
	facts := extractDecisionFacts(history)
	changed := false
	reasons := make([]string, 0, 2)

	if strings.EqualFold(decision.Action, "SUGGEST_COUPON") {
		decision.Action = "GRANT_COUPON"
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "SUGGEST_COUPON_NORMALIZED")
	}
	// 有申诉/争议 人工
	if facts.HasAppeal {
		decision.Action = "MANUAL_REVIEW"
		decision.Scene = "DISPUTE_REVIEW"
		decision.NeedHumanReview = true
		decision.CouponSuggestion = ""
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "CURRENT_EVIDENCE_PRIORITY")
		reasons = append(reasons, "当前评价存在申诉/争议，历史相似案例仅作参考，不得自动发券")
		changed = true
	}

	// 低分质量转人工
	if facts.Score <= 2 && hasQualitySignal(facts) {
		decision.Action = "MANUAL_REVIEW"
		decision.Scene = "HIGH_RISK_REVIEW"
		decision.NeedHumanReview = true
		decision.CouponSuggestion = ""
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "CURRENT_EVIDENCE_PRIORITY")
		reasons = append(reasons, "当前评价为低分质量问题，历史补偿案例不能直接复用，需人工复核")
		changed = true
	}
	if strings.EqualFold(decision.Action, "MANUAL_REVIEW") {
		decision.NeedHumanReview = true
	}
	if !strings.EqualFold(decision.Action, "GRANT_COUPON") {
		decision.CouponSuggestion = ""
	} else {
		// 券金额读取配置生成建议
		decision.CouponSuggestion = couponSuggestionForDecisionWithPolicy(decision, policy)
	}
	if changed {
		guardReason := "DecisionGuard调整：" + strings.Join(reasons, "；")
		//PolicyTags 结构化标签，用于筛选/统计/审计
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "DECISION_GUARD")
		if strings.TrimSpace(decision.Reasoning) == "" { //详细推理说明
			decision.Reasoning = guardReason
		} else {
			decision.Reasoning += " " + guardReason //在LLM给的reasoning后面追加Guard的解释
		}
		if strings.TrimSpace(decision.Reason) == "" { // 最终决策的简短原因
			decision.Reason = guardReason //Reason 只在原本为空时补充
		}
	}
	return decision
}

// extractDecisionFacts 从历史数据提取事实
func extractDecisionFacts(history []contextx.StepRecord) decisionFacts {
	var facts decisionFacts
	for _, item := range history {
		if strings.TrimSpace(item.Output) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(item.Output), &payload); err != nil {
			continue
		}
		switch item.ToolName {
		case "get_review_evidence":
			if evidence, ok := payload["evidence"].(map[string]any); ok {
				if facts.Score == 0 { // 评分
					facts.Score = int32(anyInt64(evidence["score"]))
				}
				// 申诉状态
				facts.HasAppeal = facts.HasAppeal || anyBool(evidence["hasAppeal"]) || anyBool(evidence["has_appeal"])
				if facts.Content == "" {
					facts.Content = anyString(evidence["content"]) // 申诉内容
				}
			}
		case "retrieve_rag":
			facts.RAGAspects = append(facts.RAGAspects, collectRAGAspects(payload)...) // rag Aspects
		}
	}
	return facts
}

// 整理 rag Aspects
func collectRAGAspects(payload map[string]any) []string {
	rag, ok := payload["rag"].(map[string]any)
	if !ok {
		return nil
	}
	chunks, ok := rag["chunks"].([]any)
	if !ok {
		return nil
	}
	aspects := make([]string, 0, len(chunks))
	for _, item := range chunks {
		chunk, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if aspect := anyString(chunk["aspect"]); aspect != "" {
			aspects = append(aspects, aspect)
		}
	}
	return aspects
}

// hasQualitySignal 是否涉及质量问题
func hasQualitySignal(facts decisionFacts) bool {
	content := strings.ToLower(facts.Content)
	for _, keyword := range []string{"质量", "破损", "掉色", "异味", "材质", "做工", "瑕疵", "缩水", "quality"} {
		if strings.Contains(content, strings.ToLower(keyword)) {
			return true
		}
	}
	for _, aspect := range facts.RAGAspects {
		if strings.EqualFold(aspect, "QUALITY") {
			return true
		}
	}
	return false
}

// appendMissingTag 追加标签
func appendMissingTag(tags []string, tag string) []string {
	for _, existing := range tags {
		if strings.EqualFold(existing, tag) {
			return tags
		}
	}
	return append(tags, tag)
}
