package contextx

import "seckill-agent/internal/memory"

type CompressedContext struct {
	SessionID       string
	Task            string
	Summary         string
	RecentSteps     []memory.StepRecord
	EstimatedTokens int
	Truncated       bool
}

func Compose(view memory.View, task string, budget Budget) CompressedContext {
	summary := trimText(view.Summary, budget.MaxSummaryChars)
	recent := append([]memory.StepRecord(nil), view.RecentSteps...)
	truncated := false

	if budget.MaxRecentSteps > 0 && len(recent) > budget.MaxRecentSteps {
		overflow := len(recent) - budget.MaxRecentSteps
		recent = recent[overflow:]
		truncated = true
	}

	estimated := ApproxTokens(task) + ApproxTokens(summary)
	for _, step := range recent {
		estimated += ApproxTokens(step.Action)
		estimated += ApproxTokens(step.ToolName)
		estimated += ApproxTokens(step.Arguments)
		estimated += ApproxTokens(step.Output)
		estimated += ApproxTokens(step.Error)
	}

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
