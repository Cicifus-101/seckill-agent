package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"seckill-agent/internal/contextx"
)

func TestInMemoryStore_ExpiresSession(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore(Config{
		MaxSessions:       10,
		MaxRecentSteps:    2,
		MaxSummaryChars:   200,
		SessionTTLSeconds: 1,
	})

	ctx := context.Background()
	store.Save(ctx, SessionState{
		SessionID: "session-ttl",
		Task:      "ttl test",
		UpdatedAt: time.Now().Add(-2 * time.Second),
	})

	_, ok := store.Load(ctx, "session-ttl")
	if ok {
		t.Fatal("expected expired session to be evicted")
	}
}

func TestInMemoryStore_EvictsOldestSession(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore(Config{
		MaxSessions:       2,
		MaxRecentSteps:    2,
		MaxSummaryChars:   200,
		SessionTTLSeconds: 60,
	})

	ctx := context.Background()

	store.Save(ctx, SessionState{SessionID: "s1", UpdatedAt: time.Now().Add(-3 * time.Second)})
	time.Sleep(10 * time.Millisecond)
	store.Save(ctx, SessionState{SessionID: "s2", UpdatedAt: time.Now().Add(-2 * time.Second)})
	time.Sleep(10 * time.Millisecond)
	store.Save(ctx, SessionState{SessionID: "s3", UpdatedAt: time.Now()})

	if _, ok := store.Load(ctx, "s1"); ok {
		t.Fatal("expected oldest session s1 to be evicted")
	}
	if _, ok := store.Load(ctx, "s2"); !ok {
		t.Fatal("expected session s2 to remain")
	}
	if _, ok := store.Load(ctx, "s3"); !ok {
		t.Fatal("expected session s3 to remain")
	}
}

func TestInMemoryStore_ConcurrentAppendStep(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore(Config{
		MaxSessions:       10,
		MaxRecentSteps:    50,
		MaxSummaryChars:   500,
		SessionTTLSeconds: 60,
	})

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.AppendStep(ctx, "session-concurrent", contextx.StepRecord{
				Step:      i + 1,
				ToolName:  "analyze_comments",
				Action:    "tool_call",
				Arguments: fmt.Sprintf(`{"days":%d}`, i+1),
				Output:    fmt.Sprintf(`{"ok":%d}`, i+1),
			})
		}(i)
	}

	wg.Wait()

	view := store.Snapshot(ctx, "session-concurrent")
	if len(view.RecentSteps) == 0 {
		t.Fatal("expected recent steps after concurrent append")
	}
	if len(view.RecentSteps) > 50 {
		t.Fatalf("unexpected recent steps count: %d", len(view.RecentSteps))
	}
}

func BenchmarkInMemoryStore_AppendStep(b *testing.B) {
	store := NewInMemoryStore(Config{
		MaxSessions:       1000,
		MaxRecentSteps:    6,
		MaxSummaryChars:   1200,
		SessionTTLSeconds: 60,
	})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		store.AppendStep(ctx, "bench-session", contextx.StepRecord{
			Step:      i,
			ToolName:  "analyze_comments",
			Action:    "tool_call",
			Arguments: `{"days":7}`,
			Output:    `{"ok":true}`,
		})
	}
}

func BenchmarkInMemoryStore_Snapshot(b *testing.B) {
	store := NewInMemoryStore(Config{
		MaxSessions:       1000,
		MaxRecentSteps:    6,
		MaxSummaryChars:   1200,
		SessionTTLSeconds: 60,
	})
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		store.AppendStep(ctx, "bench-session", contextx.StepRecord{
			Step:      i,
			ToolName:  "analyze_comments",
			Action:    "tool_call",
			Arguments: `{"days":7}`,
			Output:    `{"ok":true}`,
		})
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = store.Snapshot(ctx, "bench-session")
	}
}
