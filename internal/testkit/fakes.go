package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"seckill-agent/internal/llm"
	"seckill-agent/internal/tool"
	"sync"
)

type SequenceLLM struct {
	mu        sync.Mutex
	Responses []llm.ChatResponse
	Errors    []error
	Calls     []llm.ChatRequest
	index     int
}

func (s *SequenceLLM) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Calls = append(s.Calls, req)

	current := s.index
	s.index++

	if current < len(s.Errors) && s.Errors[current] != nil {
		return llm.ChatResponse{}, s.Errors[current]
	}

	if current < len(s.Responses) {
		return s.Responses[current], nil
	}

	return llm.ChatResponse{}, errors.New("sequence llm exhausted")
}

type StaticTool struct {
	InfoValue   tool.Info
	ExecuteFunc func(ctx context.Context, input json.RawMessage) (string, error)
}

func (t *StaticTool) Info() tool.Info {
	return t.InfoValue
}

func (t *StaticTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if t.ExecuteFunc != nil {
		return t.ExecuteFunc(ctx, input)
	}
	return "", nil
}
