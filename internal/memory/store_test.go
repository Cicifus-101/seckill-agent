package memory

import (
	"context"
	"fmt"
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
		store.AppendStep(ctx, "session-1", StepRecord{
			Step:      i,
			ToolName:  "analyze_comments",
			Action:    "tool_call",
			Arguments: fmt.Sprintf(`{"days":%d}`, i),
			Output:    fmt.Sprintf(`{"ok":%d}`, i),
		})
	}

	view := store.Snapshot(ctx, "session-1")
	if len(view.RecentSteps) != 2 {
		t.Fatalf("expected 2 recent steps, got %d", len(view.RecentSteps))
	}
	if view.Summary == "" {
		t.Fatal("expected summary to be generated after overflow")
	}
}
