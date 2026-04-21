package contextx

import (
	"strings"
	"testing"
)

func TestCompose_NoTruncationWhenWithinBudget(t *testing.T) {
	t.Parallel()

	view := View{
		SessionID: "session-1",
		Task:      "small task",
		Summary:   "short summary",
		RecentSteps: []StepRecord{
			{Step: 1, Action: "tool_call", ToolName: "t1", Arguments: `{"a":1}`},
		},
	}

	out := Compose(view, "small task", Budget{
		MaxPromptTokens: 1000,
		MaxRecentSteps:  10,
		MaxSummaryChars: 100,
	})

	if out.Truncated {
		t.Fatal("expected no truncation")
	}
	if len(out.RecentSteps) != 1 {
		t.Fatalf("expected 1 recent step, got %d", len(out.RecentSteps))
	}
}

func TestCompose_EmptySummary(t *testing.T) {
	t.Parallel()

	view := View{
		SessionID: "session-1",
		Task:      "task",
		RecentSteps: []StepRecord{
			{Step: 1, Action: "tool_call", ToolName: "t1", Arguments: `{"a":1}`},
		},
	}

	out := Compose(view, "task", Budget{
		MaxPromptTokens: 100,
		MaxRecentSteps:  10,
		MaxSummaryChars: 100,
	})

	if out.Summary != "" {
		t.Fatalf("expected empty summary, got %q", out.Summary)
	}
}

func TestCompose_TrimsRecentStepsFirst(t *testing.T) {
	t.Parallel()

	view := View{
		SessionID: "session-1",
		Task:      "task",
		Summary:   strings.Repeat("a", 20),
		RecentSteps: []StepRecord{
			{Step: 1, Action: "tool_call", ToolName: "t1", Arguments: strings.Repeat("x", 100)},
			{Step: 2, Action: "tool_call", ToolName: "t2", Arguments: strings.Repeat("x", 100)},
			{Step: 3, Action: "tool_call", ToolName: "t3", Arguments: strings.Repeat("x", 100)},
		},
	}

	out := Compose(view, "task", Budget{
		MaxPromptTokens: 10,
		MaxRecentSteps:  3,
		MaxSummaryChars: 10,
	})

	if !out.Truncated {
		t.Fatal("expected truncation")
	}
	if len([]rune(out.Summary)) > 10 {
		t.Fatalf("summary not trimmed: %d", len([]rune(out.Summary)))
	}
}
