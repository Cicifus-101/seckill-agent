package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"seckill-agent/internal/agent"
	"seckill-agent/internal/client/reviewjob"
	"seckill-agent/internal/client/reviewservice"
	"seckill-agent/internal/client/seckill"
	"seckill-agent/internal/config"
	"seckill-agent/internal/contextx"
	"seckill-agent/internal/job"
	"seckill-agent/internal/llm"
	"time"

	"seckill-agent/internal/llm/deepseek"
	"seckill-agent/internal/memory"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/redisx"
	"seckill-agent/internal/resilience"
	httpserver "seckill-agent/internal/server/http"
	"seckill-agent/internal/tool"
	"seckill-agent/pkg/logx"
)

type App struct {
	cfg        config.Config
	logger     *slog.Logger
	httpServer *http.Server // 监听地址，路由Handler
	jobService *job.Service // 异步任务队列
	// 当前主要加入的是redis client ，这个设计的好处是，将来增加数据库、消息队列等只需注册到closers
	closers []io.Closer // 需要关闭的服务器资源(实现了CLose() error的组件就可以放入这个切片)
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

	var llmClient llm.Client = deepseek.NewClient(cfg.LLM)
	llmClient = resilience.NewGuardedLLMClient(llmClient, resilience.LLMGuardConfig{
		RatePerSecond:       cfg.Resilience.LLMRateLimit.RatePerSecond,
		Burst:               cfg.Resilience.LLMRateLimit.Burst,
		WaitTimeout:         time.Duration(cfg.Resilience.LLMRateLimit.WaitTimeoutMS) * time.Millisecond,
		CircuitMaxRequests:  uint32(cfg.Resilience.LLMCircuitBreaker.MaxRequests),
		CircuitInterval:     time.Duration(cfg.Resilience.LLMCircuitBreaker.IntervalSeconds) * time.Second,
		CircuitTimeout:      time.Duration(cfg.Resilience.LLMCircuitBreaker.TimeoutSeconds) * time.Second,
		FailureThreshold:    uint32(cfg.Resilience.LLMCircuitBreaker.FailureThreshold),
		ConsecutiveSuccess:  uint32(cfg.Resilience.LLMCircuitBreaker.ConsecutiveSuccesses),
		DisableRateLimiter:  !cfg.Resilience.LLMRateLimit.Enabled,
		DisableCircuitBreak: !cfg.Resilience.LLMCircuitBreaker.Enabled,
	})

	reviewServiceClient := reviewservice.NewClient(
		cfg.ReviewService.BaseURL,
		time.Duration(cfg.ReviewService.TimeoutSeconds)*time.Second,
	)
	reviewJobClient := reviewjob.NewClient(
		cfg.ReviewJob.BaseURL,
		time.Duration(cfg.ReviewJob.TimeoutSeconds)*time.Second,
	)
	var couponGrantClient agent.CouponGrantService
	var seckillClient *seckill.Client
	if cfg.SeckillService.Enabled {
		seckillClient = seckill.NewClient(
			cfg.SeckillService.BaseURL,
			cfg.SeckillService.GrantPath,
			time.Duration(cfg.SeckillService.TimeoutSeconds)*time.Second,
		)
		couponGrantClient = seckillClient
	}

	memoryCfg := memory.Config{
		MaxSessions:       cfg.Memory.MaxSessions,
		MaxRecentSteps:    cfg.Memory.MaxRecentSteps,
		MaxSummaryChars:   cfg.Memory.MaxSummaryChars,
		SessionTTLSeconds: cfg.Memory.SessionTTLSeconds,
	}
	// Store 是接口，Agent依赖接口不管底层是redis还是内存，（Go进程内的map），降级策略
	var memoryStore memory.Store = memory.NewInMemoryStore(memoryCfg) // 内存存储
	//DecisionLock是接口，未赋值，当前变量是nil表示没有分布式锁
	var decisionLock agent.DecisionLock
	var redisClient *redisx.Client
	// Closer是接口，保存应用退出时需要关闭的资源
	var closers []io.Closer

	if cfg.Redis.Enabled {
		redisClient = redisx.New(redisx.Config{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		// cancel 是主动释放该上下文资源的函数
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := redisClient.Ping(pingCtx); err != nil {
			cancel()
			return nil, err
		}
		cancel()
		closers = append(closers, redisClient) // 注册清理资源
		memoryStore = memory.NewRedisStore(redisClient, memoryCfg, cfg.Redis.KeyPrefix+"session:")
		if cfg.Resilience.DecisionLock.Enabled {
			decisionLock = resilience.NewDistributedLock(
				redisClient, // set nx lua脚本
				cfg.Resilience.DecisionLock.KeyPrefix,
				time.Duration(cfg.Resilience.DecisionLock.TTLSeconds)*time.Second,
			)
		}
	}

	budget := contextx.Budget{
		MaxPromptTokens: cfg.PromptBudget.MaxPromptTokens,
		MaxRecentSteps:  cfg.PromptBudget.MaxRecentSteps,
		MaxSummaryChars: cfg.PromptBudget.MaxSummaryChars,
	}

	registry := tool.NewRegistry()
	// agent根据LLM返回的工具名查找对应工具，调用工具的Execute()方法
	registry.Register(tool.NewReviewEvidenceTool(reviewServiceClient))
	registry.Register(tool.NewReviewStatsTool(reviewJobClient))
	registry.Register(tool.NewRAGRetrieveTool(reviewJobClient))
	if seckillClient != nil {
		// 秒杀客户端创建了，注册get_campaign_context
		registry.Register(tool.NewCampaignContextTool(seckillClient))
	}

	// 提示词
	prompts := prompt.NewManager()
	couponPolicy := agent.NewCouponPolicyProvider(
		cfg.CouponPolicy.Path,
		cfg.CouponPolicy.HotReload,
		time.Duration(cfg.CouponPolicy.ReloadSeconds)*time.Second,
	)
	finalizerOpts := []agent.ReviewCouponFinalizerOption{
		agent.WithReviewCouponDecisionStore(reviewJobClient),
		agent.WithReviewCouponPolicy(couponPolicy), // 最终决策进行业务规则校验和校正
	}
	if couponGrantClient != nil {
		finalizerOpts = append(finalizerOpts, agent.WithReviewCouponGrantService(couponGrantClient))
	}
	if decisionLock != nil {
		// 保存决策前或发券前锁定业务对象（跨实例并发保护）
		finalizerOpts = append(finalizerOpts, agent.WithReviewCouponDecisionLock(decisionLock))
	}
	// 创建最终决策处理器
	reviewCouponFinalizer := agent.NewReviewCouponFinalizer(finalizerOpts...)

	// 准备Agent选项
	opts := []agent.Option{
		agent.WithMemoryStore(memoryStore, budget), //memory和预算
		agent.WithTimeouts(
			time.Duration(cfg.LLM.TimeoutSeconds)*time.Second,
			time.Duration(cfg.Agent.ToolCallTimeoutSeconds)*time.Second,
		),
		agent.WithDecisionFinalizer(reviewCouponFinalizer), // 业务闭环处理器
	}
	ag := agent.New(llmClient, prompts, registry, cfg.Agent.MaxSteps, opts...)
	var jobService *job.Service //异步job service初始化
	if cfg.AgentJob.Enabled && redisClient != nil {
		jobService = job.NewService(job.Config{
			Enabled: cfg.AgentJob.Enabled,
			// 任务key： seckill-agent:agent:jobs:{job_id}   队列key：seckill-agent:agent:jobs:queue
			KeyPrefix:         cfg.Redis.KeyPrefix,
			WorkerCount:       cfg.AgentJob.WorkerCount,
			GlobalConcurrency: cfg.AgentJob.GlobalConcurrency,
			ScopeConcurrency:  cfg.AgentJob.ScopeConcurrency,
			MaxRetries:        cfg.AgentJob.MaxRetries,
			JobTTL:            time.Duration(cfg.AgentJob.JobTTLSeconds) * time.Second,
			QueueWait:         time.Duration(cfg.AgentJob.QueueWaitSeconds) * time.Second,
			RunTimeout:        time.Duration(cfg.AgentJob.RunTimeoutSeconds) * time.Second,
			RetryBackoff:      time.Duration(cfg.AgentJob.RetryBackoffSeconds) * time.Second,
			MaxBatchSize:      cfg.AgentJob.MaxBatchSize,
		}, redisClient, ag, logger)
	}
	handler := httpserver.NewHandler(cfg, logger, llmClient, ag, jobService)
	server := httpserver.NewServer(cfg.HTTP, handler) //监听地址、路由、Http的4个Timeout

	logger.Info("application bootstrapped",
		"app", cfg.App.Name,
		"env", cfg.App.Env,
		"version", cfg.App.Version,
		"http_addr", server.Addr,
		"llm_provider", cfg.LLM.Provider,
		"llm_model", cfg.LLM.Model,
		"memory_max_sessions", cfg.Memory.MaxSessions,
		"memory_max_recent_steps", cfg.Memory.MaxRecentSteps,
		"memory_max_summary_chars", cfg.Memory.MaxSummaryChars,
		"prompt_budget_max_recent_steps", cfg.PromptBudget.MaxRecentSteps,
		"prompt_budget_max_summary_chars", cfg.PromptBudget.MaxSummaryChars,
		"prompt_budget_max_tokens", cfg.PromptBudget.MaxPromptTokens,
		"redis_enabled", cfg.Redis.Enabled,
		"redis_addr", cfg.Redis.Addr,
		"llm_rate_limit_enabled", cfg.Resilience.LLMRateLimit.Enabled,
		"llm_rate_limit_rate_per_second", cfg.Resilience.LLMRateLimit.RatePerSecond,
		"llm_circuit_breaker_enabled", cfg.Resilience.LLMCircuitBreaker.Enabled,
		"decision_lock_enabled", cfg.Resilience.DecisionLock.Enabled,
		"coupon_policy_path", cfg.CouponPolicy.Path,
		"coupon_policy_hot_reload", cfg.CouponPolicy.HotReload,
		"otel_endpoint", cfg.Observability.OTELEndpoint,
		"review_service_base_url", cfg.ReviewService.BaseURL,
		"review_job_base_url", cfg.ReviewJob.BaseURL,
		"seckill_service_enabled", cfg.SeckillService.Enabled,
		"seckill_service_base_url", cfg.SeckillService.BaseURL,
		"agent_job_enabled", cfg.AgentJob.Enabled,
		"agent_job_worker_count", cfg.AgentJob.WorkerCount,
		"agent_job_max_retries", cfg.AgentJob.MaxRetries,
	)

	return &App{
		cfg:        cfg,
		logger:     logger,
		httpServer: server,
		jobService: jobService,
		closers:    closers,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	if a.jobService != nil {
		a.jobService.Start(ctx)
	}

	go func() {
		a.logger.Info("http server listening", "addr", a.httpServer.Addr)
		errCh <- a.httpServer.ListenAndServe() // 阻塞方法，如果直接调用后面的select就永远无法执行，无法监听关闭信号
	}()

	select {
	// 收到退出信号，能进入case分支，说明原来的ctx已经被取消了（ctx.Done()通道关闭 ctx.Err()变成 context.Cancelled）
	case <-ctx.Done():
		//原来的 ctx 已经被取消，继续使用会立即返回，无法等待正在执行的HTTP请求完成。新的 shutdownCtx 给HTTP服务一个独立的关闭时间窗口
		// 原来的ctx是Run方法里面的
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(a.cfg.HTTP.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()

		a.logger.Info("shutdown signal received")
		// 优雅关闭，停止接收新连接，关闭空闲连接，等待正在处理的请求结束，超时后返回错误
		err := a.httpServer.Shutdown(shutdownCtx)
		a.closeResources()
		return err
	case err := <-errCh:
		a.closeResources() // 服务启动失败或崩溃
		return err
	}
}

func (a *App) Logger() *slog.Logger {
	return a.logger
}

func (a *App) closeResources() {
	for _, closer := range a.closers {
		if err := closer.Close(); err != nil {
			a.logger.Warn("close resource failed", "error", err)
		}
	}
}
