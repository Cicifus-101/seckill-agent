package memory

import (
	"fmt"
	"strings"
)

func summarizeSteps(steps []StepRecord) string {
	if len(steps) == 0 {
		return ""
	}

	var b strings.Builder
	for _, step := range steps {
		b.WriteString(fmt.Sprintf("[step %d] tool=%s action=%s", step.Step, step.ToolName, step.Action))
		if step.Arguments != "" {
			b.WriteString(" args=")
			b.WriteString(truncate(step.Arguments, 120))
		}
		if step.Output != "" {
			b.WriteString(" output=")
			b.WriteString(truncate(step.Output, 180))
		}
		if step.Error != "" {
			b.WriteString(" error=")
			b.WriteString(truncate(step.Error, 120))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()) // 移除首尾空白字符
}

func trimText(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func truncate(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
