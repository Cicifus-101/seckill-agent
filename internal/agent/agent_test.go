package agent

import (
	"context"
	"encoding/json"
	"testing"

	"seckill-agent/internal/llm"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/testkit"
	"seckill-agent/internal/tool"
)

func TestAgent_Run_Success(t *testing.T) {
	t.Parallel()

	llmClient := &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{
				Content: `{"type":"tool_call","tool_name":"analyze_comments","arguments":{"days":7,"sentiment":"positive"}}`,
			},
			{
				Content: `{"type":"tool_call","tool_name":"plan_coupon_quota","arguments":{"user_count":2,"discount":0.8}}`,
			},
			{
				Content: `{"type":"final","final_answer":"已完成优惠券分发方案生成"}`,
			},
		},
	}

	registry := buildTestRegistry()
	ag := New(llmClient, prompt.NewManager(), registry, 5)

	result, err := ag.Run(context.Background(), "分析最近一周评论区反馈积极的用户，给他们推送 8 折秒杀券")
	if err != nil {
		t.Fatalf("agent run failed: %v", err)
	}

	if result.FinalAnswer != "已完成优惠券分发方案生成" {
		t.Fatalf("unexpected final answer: %s", result.FinalAnswer)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 tool steps, got %d", len(result.Steps))
	}
	if result.Steps[0].ToolName != "analyze_comments" {
		t.Fatalf("unexpected first tool: %s", result.Steps[0].ToolName)
	}
	if result.Steps[1].ToolName != "plan_coupon_quota" {
		t.Fatalf("unexpected second tool: %s", result.Steps[1].ToolName)
	}
}

func TestAgent_Run_UnknownTool(t *testing.T) {
	t.Parallel()

	llmClient := &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{
				Content: `{"type":"tool_call","tool_name":"not_exists","arguments":{}}`,
			},
		},
	}

	ag := New(llmClient, prompt.NewManager(), tool.NewRegistry(), 3)

	_, err := ag.Run(context.Background(), "test unknown tool")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestAgent_Run_MaxStepsReached(t *testing.T) {
	t.Parallel()

	llmClient := &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{Content: `{"type":"tool_call","tool_name":"analyze_comments","arguments":{"days":7}}`},
			{Content: `{"type":"tool_call","tool_name":"analyze_comments","arguments":{"days":7}}`},
		},
	}

	registry := buildTestRegistry()
	ag := New(llmClient, prompt.NewManager(), registry, 1)

	result, err := ag.Run(context.Background(), "force max steps")
	if err != nil {
		t.Fatalf("agent run should not error on max steps path: %v", err)
	}
	if result.FinalAnswer != "达到最大迭代次数，未收敛到最终答案" {
		t.Fatalf("unexpected final answer: %s", result.FinalAnswer)
	}
}

func BenchmarkAgent_Run(b *testing.B) {
	registry := buildTestRegistry() // 注册测试工具

	// 开启内存分配统计
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		llmClient := &testkit.SequenceLLM{
			Responses: []llm.ChatResponse{
				{Content: `{"type":"tool_call","tool_name":"analyze_comments","arguments":{"days":7,"sentiment":"positive"}}`},
				{Content: `{"type":"tool_call","tool_name":"plan_coupon_quota","arguments":{"user_count":2,"discount":0.8}}`},
				{Content: `{"type":"final","final_answer":"ok"}`},
			},
		}

		ag := New(llmClient, prompt.NewManager(), registry, 5)
		_, _ = ag.Run(context.Background(), "benchmark task")
	}
}

func buildTestRegistry() *tool.Registry {
	registry := tool.NewRegistry()

	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "analyze_comments",
			Description: "mock comment analysis",
			InputSchema: `{"days":7,"sentiment":"positive"}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			return `{"users":[{"user_id":1001,"score":0.96}]}`, nil
		},
	})

	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "plan_coupon_quota",
			Description: "mock coupon planning",
			InputSchema: `{"user_count":2,"discount":0.8}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			return `{"coupon_count":2,"strategy":"one coupon per user"}`, nil
		},
	})

	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "trigger_seckill_warmup",
			Description: "mock warmup",
			InputSchema: `{"campaign_id":"campaign_001"}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			return `{"status":"warmup_triggered"}`, nil
		},
	})

	return registry
}
