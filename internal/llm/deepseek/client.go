package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"seckill-agent/internal/config"
	"seckill-agent/internal/llm"
	"strings"
	"time"
)

type Client struct {
	baseURL     string
	apiKey      string
	model       string
	maxTokens   int
	temperature float64
	httpClient  *http.Client
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"` // assistant 输出回答
	} `json:"choices"`
	// 指针区分是没有错误还是空错误对象
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func NewClient(cfg config.LLMConfig) *Client {
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}
}

func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	payload := chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("marshal deepseek request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("create deepseek request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("call deepseek api: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("read deepseek response: %w", err)
	}

	if httpResp.StatusCode >= http.StatusBadRequest {
		return llm.ChatResponse{}, fmt.Errorf("deepseek api returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var res chatCompletionResponse
	if err = json.Unmarshal(respBody, &res); err != nil {
		return llm.ChatResponse{}, fmt.Errorf("unmarshal deepseek response: %w", err)
	}

	// 检查是否由候选回复
	if res.Error != nil {
		return llm.ChatResponse{}, fmt.Errorf("deepseek api error: %s", res.Error.Message)
	}
	if len(res.Choices) == 0 {
		return llm.ChatResponse{}, fmt.Errorf("deepseek response missing choices")
	}

	return llm.ChatResponse{
		ID:      res.ID,
		Model:   res.Model,
		Content: res.Choices[0].Message.Content,
	}, nil
}
