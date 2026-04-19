package deepseek

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"seckill-agent/internal/config"
	"seckill-agent/internal/llm"
)

func TestClient_Chat(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotContentType string
	var gotBody struct {
		Model       string  `json:"model"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	// 启动本地HTTP服务，绑定随机端口，用我的handler函数处理请求
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")

		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-1",
			"model": "deepseek-chat",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello from deepseek",
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
		TimeoutSeconds: 5,
		MaxTokens:      128,
		Temperature:    0.2,
	})

	resp, err := client.Chat(t.Context(), llm.ChatRequest{
		SystemPrompt: "you are a test assistant",
		UserPrompt:   "say hello",
	})
	if err != nil {
		t.Fatalf("chat request failed: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("unexpected auth header: %s", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("unexpected content-type: %s", gotContentType)
	}
	if gotBody.Model != "deepseek-chat" {
		t.Fatalf("unexpected model: %s", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(gotBody.Messages))
	}
	if resp.Content != "hello from deepseek" {
		t.Fatalf("unexpected response content: %s", resp.Content)
	}
}
