package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"seckill-agent/internal/llm"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/testkit"
	"seckill-agent/internal/tool"
)

func TestAgent_Run_ToolTimeout(t *testing.T) {
	t.Parallel()

	llmClient := &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{Content: `{"type":"tool_call","tool_name":"slow_tool","arguments":{}}`},
		},
	}

	registry := tool.NewRegistry()
	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "slow_tool",
			Description: "slow mock",
			InputSchema: `{}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			select {
			case <-time.After(300 * time.Millisecond):
				return `{"ok":true}`, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	})

	ag := New(
		llmClient,
		prompt.NewManager(),
		registry,
		1,
		WithTimeouts(500*time.Millisecond, 50*time.Millisecond),
	)

	_, err := ag.Run(context.Background(), "timeout test")
	if err == nil {
		t.Fatal("expected tool timeout error")
	}
}

func TestAgent_Run_ToolErrorRecorded(t *testing.T) {
	t.Parallel()

	llmClient := &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{Content: `{"type":"tool_call","tool_name":"err_tool","arguments":{}}`},
			{Content: `{"type":"final","final_answer":"done"}`},
		},
	}

	registry := tool.NewRegistry()
	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "err_tool",
			Description: "error mock",
			InputSchema: `{}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "", errors.New("tool failed")
		},
	})

	ag := New(
		llmClient,
		prompt.NewManager(),
		registry,
		2,
		WithTimeouts(500*time.Millisecond, 100*time.Millisecond),
	)

	res, err := ag.Run(context.Background(), "tool error test")
	if err != nil {
		t.Fatalf("agent run should not fail: %v", err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("expected steps recorded")
	}
	if res.Steps[0].Error == "" {
		t.Fatal("expected tool error recorded in step")
	}
}

func TestAgent_Run_ConcurrentSessions(t *testing.T) {
	t.Parallel()

	registry := buildTestRegistry()
	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			llmClient := &testkit.SequenceLLM{
				Responses: []llm.ChatResponse{
					{Content: `{"type":"tool_call","tool_name":"analyze_comments","arguments":{"days":7}}`},
					{Content: `{"type":"final","final_answer":"ok"}`},
				},
			}

			ag := New(llmClient, prompt.NewManager(), registry, 2)
			_, err := ag.RunWithSession(context.Background(), "session-"+string(rune('a'+i)), "test task")
			errCh <- err
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
