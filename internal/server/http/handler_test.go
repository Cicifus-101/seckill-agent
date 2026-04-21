package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"seckill-agent/internal/agent"
	"seckill-agent/internal/config"
	"seckill-agent/internal/llm"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/testkit"
	"seckill-agent/internal/tool"
	"testing"
)

func TestHandler_Healthz(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{Content: `{"type":"final","final_answer":"ok"}`},
		},
	}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected status field: %v", body["status"])
	}
}

func TestHandler_ChatDebug(t *testing.T) {
	t.Parallel()

	llmClient := &testkit.SequenceLLM{
		Responses: []llm.ChatResponse{
			{
				ID:      "chatcmpl-1",
				Model:   "deepseek-chat",
				Content: "hello debug",
			},
		},
	}

	h := newTestHandler(t, llmClient, nil)

	payload := []byte(`{
		"system_prompt":"you are a test assistant",
		"user_prompt":"say hello"
	}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["output"] != "hello debug" {
		t.Fatalf("unexpected output: %v", body["output"])
	}
	if body["model"] != "deepseek-chat" {
		t.Fatalf("unexpected model: %v", body["model"])
	}
}

func TestHandler_AgentRun(t *testing.T) {
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
				Content: `{"type":"final","final_answer":"已完成"}`,
			},
		},
	}

	registry := tool.NewRegistry()
	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "analyze_comments",
			Description: "mock",
			InputSchema: `{"days":7}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			return `{"users":[{"user_id":1001}]}`, nil
		},
	})
	registry.Register(&testkit.StaticTool{
		InfoValue: tool.Info{
			Name:        "plan_coupon_quota",
			Description: "mock",
			InputSchema: `{"user_count":2}`,
		},
		ExecuteFunc: func(ctx context.Context, input json.RawMessage) (string, error) {
			return `{"coupon_count":2}`, nil
		},
	})

	ag := agent.New(llmClient, prompt.NewManager(), registry, 5)
	h := newTestHandler(t, llmClient, ag)

	payload := []byte(`{
		"task":"分析最近一周评论区反馈积极的用户，给他们推送 8 折秒杀券"
	}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/run", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["final_answer"] != "已完成" {
		t.Fatalf("unexpected final answer: %v", body["final_answer"])
	}
}

// 性能瓶颈、内存分配、执行速度
func BenchmarkHealthz(b *testing.B) {
	h := newTestHandler(b, &testkit.SequenceLLM{}, nil)
	handler := h.Routes()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	b.ReportAllocs()

	for i := 0; i < b.N; i++ { // 用来计算每次耗时
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func newTestHandler(t testing.TB, llmClient llm.Client, ag *agent.Agent) *Handler {
	t.Helper()

	cfg := config.Config{
		App: config.AppConfig{
			Name:    "seckill-agent",
			Env:     "test",
			Version: "0.1.0",
		},
		HTTP: config.HTTPConfig{
			Host:                   "127.0.0.1",
			Port:                   8080,
			ReadTimeoutSeconds:     5,
			WriteTimeoutSeconds:    5,
			IdleTimeoutSeconds:     5,
			ShutdownTimeoutSeconds: 5,
		},
		Log: config.LogConfig{
			Level:     "error",
			Format:    "text",
			AddSource: false,
		},
		LLM: config.LLMConfig{
			TimeoutSeconds: 5,
		},
		Agent: config.AgentConfig{
			MaxSteps:               5,
			ToolCallTimeoutSeconds: 10,
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewHandler(cfg, logger, llmClient, ag)
}
