package contextx

import (
	"strings"
	"testing"

	"seckill-agent/internal/memory"
)

func TestCompose_TrimsByBudget(t *testing.T) {
	t.Parallel()

	view := memory.View{
		SessionID: "session-1",
		Task:      "test task",
		Summary:   strings.Repeat("啊", 100),
		RecentSteps: []memory.StepRecord{
			{Step: 1, Action: "tool_call", ToolName: "t1", Arguments: strings.Repeat("x", 100)},
			{Step: 2, Action: "tool_call", ToolName: "t2", Arguments: strings.Repeat("x", 100)},
			{Step: 3, Action: "tool_call", ToolName: "t3", Arguments: strings.Repeat("x", 100)},
		},
	}

	out := Compose(view, "test task", Budget{
		MaxPromptTokens: 20,
		MaxRecentSteps:  2,
		MaxSummaryChars: 10,
	})

	if !out.Truncated {
		t.Fatal("expected compressed context to be truncated")
	}
	if len(out.RecentSteps) > 2 {
		t.Fatalf("expected at most 2 recent steps, got %d", len(out.RecentSteps))
	}
	if len([]rune(out.Summary)) > 10 {
		t.Fatalf("expected summary to be trimmed, got %d runes", len([]rune(out.Summary)))
	}
}
