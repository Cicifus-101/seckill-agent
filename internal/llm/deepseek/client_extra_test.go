package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"seckill-agent/internal/config"
	"seckill-agent/internal/llm"
)

func TestClient_Chat_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-1",
			"model": "deepseek-chat",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "slow response",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:        server.URL,
		APIKey:         "test-api-key",
		Model:          "deepseek-chat",
		TimeoutSeconds: 1,
		MaxTokens:      128,
		Temperature:    0.2,
	})

	ctx, cancel := t.Context(), func() {}
	defer cancel()

	shortCtx, shortCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer shortCancel()

	_, err := client.Chat(shortCtx, llm.ChatRequest{
		SystemPrompt: "test",
		UserPrompt:   "test",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "context deadline") &&
		!strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_Chat_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"1","model":"x","choices":[`))
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:        server.URL,
		APIKey:         "test-api-key",
		Model:          "deepseek-chat",
		TimeoutSeconds: 5,
		MaxTokens:      128,
		Temperature:    0.2,
	})

	_, err := client.Chat(t.Context(), llm.ChatRequest{
		SystemPrompt: "test",
		UserPrompt:   "test",
	})
	if err == nil {
		t.Fatal("expected json unmarshal error")
	}
}
