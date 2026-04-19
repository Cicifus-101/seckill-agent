package llm

import "context"

type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
}

type ChatResponse struct {
	ID      string // 定位某次具体的请求，（缓存）用ID做请求去重；按请求统计API调用量
	Model   string
	Content string
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
