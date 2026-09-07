package resilience

import (
	"context"
	"fmt"
	"time"

	"github.com/sony/gobreaker/v2"

	"seckill-agent/internal/llm"
)

type LLMGuardConfig struct {
	RatePerSecond       float64
	Burst               int
	WaitTimeout         time.Duration
	CircuitMaxRequests  uint32
	CircuitInterval     time.Duration
	CircuitTimeout      time.Duration
	FailureThreshold    uint32
	ConsecutiveSuccess  uint32
	DisableRateLimiter  bool
	DisableCircuitBreak bool
}

type GuardedLLMClient struct {
	next    llm.Client
	limiter *TokenBucket
	breaker *gobreaker.CircuitBreaker[llm.ChatResponse]
	cfg     LLMGuardConfig
}

func NewGuardedLLMClient(next llm.Client, cfg LLMGuardConfig) *GuardedLLMClient {
	if cfg.RatePerSecond <= 0 {
		cfg.RatePerSecond = 2
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 4
	}
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = 2 * time.Second
	}
	if cfg.CircuitMaxRequests <= 0 {
		cfg.CircuitMaxRequests = 2
	}
	if cfg.CircuitInterval <= 0 {
		cfg.CircuitInterval = 30 * time.Second
	}
	if cfg.CircuitTimeout <= 0 {
		cfg.CircuitTimeout = 20 * time.Second
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.ConsecutiveSuccess <= 0 {
		cfg.ConsecutiveSuccess = 2
	}

	guarded := &GuardedLLMClient{
		next: next,
		cfg:  cfg,
	}
	if !cfg.DisableRateLimiter {
		guarded.limiter = NewTokenBucket(cfg.RatePerSecond, cfg.Burst)
	}
	if !cfg.DisableCircuitBreak {
		settings := gobreaker.Settings{
			Name:        "llm",
			MaxRequests: cfg.CircuitMaxRequests,
			Interval:    cfg.CircuitInterval,
			Timeout:     cfg.CircuitTimeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= cfg.FailureThreshold
			},
			IsSuccessful: func(err error) bool {
				return err == nil
			},
		}
		guarded.breaker = gobreaker.NewCircuitBreaker[llm.ChatResponse](settings)
	}
	return guarded
}

func (c *GuardedLLMClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if c.limiter != nil {
		waitCtx, cancel := context.WithTimeout(ctx, c.cfg.WaitTimeout)
		defer cancel()
		if err := c.limiter.Wait(waitCtx); err != nil {
			return llm.ChatResponse{}, ErrRateLimited("llm")
		}
	}

	call := func() (llm.ChatResponse, error) {
		return c.next.Chat(ctx, req)
	}
	if c.breaker == nil {
		return call()
	}
	resp, err := c.breaker.Execute(call)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("llm unavailable or circuit open: %w", err)
	}
	return resp, nil
}
