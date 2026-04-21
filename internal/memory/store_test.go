package memory

import (
	"context"
	"fmt"
	"seckill-agent/internal/contextx"
	"testing"
)

func TestInMemoryStore_CompressesOldSteps(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore(Config{
		MaxSessions:       10,
		MaxRecentSteps:    2,
		MaxSummaryChars:   200,
		SessionTTLSeconds: 60,
	})

	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		step := contextx.StepRecord{
			Step:      i,
			ToolName:  "analyze_comments",
			Action:    "tool_call",
			Arguments: fmt.Sprintf(`{"days":%d}`, i),
			Output:    fmt.Sprintf(`{"ok":%d}`, i),
		}
		store.AppendStep(ctx, "session-1", step)

		t.Logf("append step: step=%d tool=%s action=%s args=%s output=%s",
			step.Step, step.ToolName, step.Action, step.Arguments, step.Output)
	}

	view := store.Snapshot(ctx, "session-1")
	if len(view.RecentSteps) != 2 {
		t.Fatalf("expected 2 recent steps, got %d", len(view.RecentSteps))
	}
	if view.Summary == "" {
		t.Fatal("expected summary to be generated after overflow")
	}

	t.Logf("snapshot summary=%q", view.Summary)
	t.Logf("snapshot recent_steps=%d", len(view.RecentSteps))
	for i, step := range view.RecentSteps {
		t.Logf("recent[%d]=step=%d tool=%s action=%s args=%s output=%s error=%s",
			i, step.Step, step.ToolName, step.Action, step.Arguments, step.Output, step.Error)
	}
}
