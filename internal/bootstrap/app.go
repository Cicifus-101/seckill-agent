package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"seckill-agent/internal/agent"
	"seckill-agent/internal/config"
	"seckill-agent/internal/contextx"
	"seckill-agent/internal/llm/deepseek"
	"seckill-agent/internal/memory"
	"seckill-agent/internal/prompt"
	httpserver "seckill-agent/internal/server/http"
	"seckill-agent/internal/tool"
	"seckill-agent/pkg/logx"
	"time"
)

type App struct {
	cfg        config.Config
	logger     *slog.Logger
	httpServer *http.Server
}

func NewApp() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger, err := logx.New(cfg.Log)
	if err != nil {
		return nil, err
	}

	llmClient := deepseek.NewClient(cfg.LLM)

	memoryStore := memory.NewInMemoryStore(memory.Config{
		MaxSessions:       cfg.Memory.MaxSessions,
		MaxRecentSteps:    cfg.Memory.MaxRecentSteps,
		MaxSummaryChars:   cfg.Memory.MaxSummaryChars,
		SessionTTLSeconds: cfg.Memory.SessionTTLSeconds,
	})

	budget := contextx.Budget{
		MaxPromptTokens: cfg.Memory.MaxPromptTokens,
		MaxRecentSteps:  cfg.Memory.MaxRecentSteps,
		MaxSummaryChars: cfg.Memory.MaxSummaryChars,
	}

	registry := tool.NewRegistry()
	registry.Register(&tool.MockCommentAnalysisTool{})
	registry.Register(&tool.MockCouponPlanTool{})
	registry.Register(&tool.MockWarmupTool{})

	prompts := prompt.NewManager()
	ag := agent.New(llmClient, prompts, registry, cfg.Agent.MaxSteps, agent.WithMemoryStore(memoryStore, budget))
	handler := httpserver.NewHandler(cfg, logger, llmClient, ag)
	server := httpserver.NewServer(cfg.HTTP, handler)

	logger.Info("application bootstrapped",
		"app", cfg.App.Name,
		"env", cfg.App.Env,
		"version", cfg.App.Version,
		"http_addr", server.Addr,
		"llm_provider", cfg.LLM.Provider,
		"llm_model", cfg.LLM.Model,
		"memory_max_sessions", cfg.Memory.MaxSessions,
		"memory_max_recent_steps", cfg.Memory.MaxRecentSteps,
		"memory_max_prompt_tokens", cfg.Memory.MaxPromptTokens,
	)

	return &App{
		cfg:        cfg,
		logger:     logger,
		httpServer: server,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		a.logger.Info("http server listening", "addr", a.httpServer.Addr)
		errCh <- a.httpServer.ListenAndServe()
	}()

	select {
	// 返回一个只读通道，用于监听上下文的取消信号
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(a.cfg.HTTP.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()

		a.logger.Info("shutdown signal received")
		return a.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (a *App) Logger() *slog.Logger {
	return a.logger
}
