package contextx

type CompressedContext struct {
	SessionID       string
	Task            string
	Summary         string
	RecentSteps     []StepRecord
	EstimatedTokens int  // 估计使用了多少Token
	Truncated       bool // 是否发生截断 用于调试、日志、监控上下文丢失情况、判断是否需要调整预算
}

func Compose(view View, task string, budget Budget) CompressedContext {
	summary := trimPromptText(view.Summary, budget.MaxSummaryChars)
	recent := compactStepsForPrompt(view.RecentSteps) // 针对不同字段设置最大长度
	truncated := false

	// 限制步骤数
	if budget.MaxRecentSteps > 0 && len(recent) > budget.MaxRecentSteps {
		overflow := len(recent) - budget.MaxRecentSteps
		recent = recent[overflow:] // 从最早的开始删除（这里没有将删除的步骤再次合并到摘要中，因为摘要的合并子啊存储层的normalizeState 中完成）
		truncated = true
	}

	estimated := ApproxTokens(task) + ApproxTokens(summary) // 估算任务+摘要（未估算Prompt固定模板、系统提示词、JSON字段名等额外内容）
	for _, step := range recent {
		estimated += ApproxTokens(step.Action)
		estimated += ApproxTokens(step.ToolName)
		estimated += ApproxTokens(step.Arguments)
		estimated += ApproxTokens(step.Output)
		estimated += ApproxTokens(step.Error)
	}

	// 限制总Token数
	if budget.MaxPromptTokens > 0 && estimated > budget.MaxPromptTokens {
		for len(recent) > 0 && estimated > budget.MaxPromptTokens {
			removed := recent[0]
			recent = recent[1:]

			estimated -= ApproxTokens(removed.Action)
			estimated -= ApproxTokens(removed.ToolName)
			estimated -= ApproxTokens(removed.Arguments)
			estimated -= ApproxTokens(removed.Output)
			estimated -= ApproxTokens(removed.Error)
			truncated = true
		}
	}

	return CompressedContext{
		SessionID:       view.SessionID,
		Task:            task,
		Summary:         summary,
		RecentSteps:     recent,
		EstimatedTokens: estimated,
		Truncated:       truncated,
	}
}

// 专门压缩步骤中的长字段
func compactStepsForPrompt(steps []StepRecord) []StepRecord {
	out := make([]StepRecord, 0, len(steps))
	for _, step := range steps {
		item := step
		item.Arguments = trimPromptText(item.Arguments, 800)
		item.Error = trimPromptText(item.Error, 800)
		switch item.ToolName {
		// 控制输出字段长度
		case "retrieve_rag":
			item.Output = trimPromptText(item.Output, 2600) // rag 2600
		case "get_review_evidence":
			item.Output = trimPromptText(item.Output, 1800) // evidence 1800
		case "get_review_stats":
			item.Output = trimPromptText(item.Output, 1600) // stats 1600
		case "store_decision", "llm":
			item.Output = trimPromptText(item.Output, 1200) // decision 1200
		default:
			item.Output = trimPromptText(item.Output, 1000) // llm 1000
		}
		out = append(out, item)
	}
	return out
}

func trimPromptText(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	const marker = "...[truncated]"
	markerRunes := []rune(marker)
	if limit <= len(markerRunes) {
		return string(runes[:limit])
	}
	return string(runes[:limit-len(markerRunes)]) + marker
}
