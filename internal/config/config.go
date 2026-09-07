package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultConfigPath = "configs/config.json"

type Config struct {
	App            AppConfig             `json:"app"`
	HTTP           HTTPConfig            `json:"http"`
	Log            LogConfig             `json:"log"`
	LLM            LLMConfig             `json:"llm"`
	Agent          AgentConfig           `json:"agent"`
	AgentJob       AgentJobConfig        `json:"agent_job"`
	Memory         MemoryConfig          `json:"memory"`
	PromptBudget   PromptBudgetConfig    `json:"prompt_budget"`
	CouponPolicy   CouponPolicyConfig    `json:"coupon_policy"`
	Redis          RedisConfig           `json:"redis"`
	Resilience     ResilienceConfig      `json:"resilience"`
	Observability  ObservabilityConfig   `json:"observability"`
	ReviewService  ServiceEndpointConfig `json:"review_service"`
	ReviewJob      ServiceEndpointConfig `json:"review_job"`
	SeckillService SeckillServiceConfig  `json:"seckill_service"`
}

type AppConfig struct {
	Name    string `json:"name"`
	Env     string `json:"env"`
	Version string `json:"version"`
}

type HTTPConfig struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	ReadTimeoutSeconds int    `json:"read_timeout_seconds"`
	//HTTP服务器向客户端写响应的超时时间
	WriteTimeoutSeconds    int `json:"write_timeout_seconds"`
	IdleTimeoutSeconds     int `json:"idle_timeout_seconds"`
	ShutdownTimeoutSeconds int `json:"shutdown_timeout_seconds"`
}

type LogConfig struct {
	Level     string `json:"level"`
	Format    string `json:"format"`
	AddSource bool   `json:"add_source"`
}

type LLMConfig struct {
	Provider       string  `json:"provider"`
	BaseURL        string  `json:"base_url"`
	APIKey         string  `json:"api_key"`
	Model          string  `json:"model"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	MaxTokens      int     `json:"max_tokens"`
	Temperature    float64 `json:"temperature"`
}

type AgentConfig struct {
	MaxSteps int `json:"max_steps"`
	// 从发起工具调用到收到工具返回结果的截止时间
	ToolCallTimeoutSeconds int `json:"tool_call_timeout_seconds"`
}

// 异步任务队列
type AgentJobConfig struct {
	Enabled           bool `json:"enabled"`
	WorkerCount       int  `json:"worker_count"`
	GlobalConcurrency int  `json:"global_concurrency"`
	ScopeConcurrency  int  `json:"scope_concurrency"`
	MaxRetries        int  `json:"max_retries"`
	// 任务结果在队列系统中的存活时间（Redis存储的任务详情）
	JobTTLSeconds       int `json:"job_ttl_seconds"` // 7天
	QueueWaitSeconds    int `json:"queue_wait_seconds"`
	RunTimeoutSeconds   int `json:"run_timeout_seconds"`
	RetryBackoffSeconds int `json:"retry_backoff_seconds"`
	MaxBatchSize        int `json:"max_batch_size"`
}

type MemoryConfig struct {
	MaxSessions       int `json:"max_sessions"`
	MaxRecentSteps    int `json:"max_recent_steps"`
	MaxSummaryChars   int `json:"max_summary_chars"`
	SessionTTLSeconds int `json:"session_ttl_seconds"`
}

// PromptBudgetConfig limits the memory view sent to the LLM. It is separate
// from MemoryConfig, which controls how much execution history is retained.
type PromptBudgetConfig struct {
	MaxPromptTokens int `json:"max_prompt_tokens"`
	MaxRecentSteps  int `json:"max_recent_steps"`
	MaxSummaryChars int `json:"max_summary_chars"`
}

type CouponPolicyConfig struct {
	Path            string `json:"path"`
	HotReload       bool   `json:"hot_reload"`
	ReloadSeconds   int    `json:"reload_seconds"`
	DefaultValidity int    `json:"default_validity_days"`
}

type ServiceEndpointConfig struct {
	BaseURL        string `json:"base_url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type SeckillServiceConfig struct {
	Enabled        bool   `json:"enabled"`
	BaseURL        string `json:"base_url"`
	GrantPath      string `json:"grant_path"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// 用于session memory、异步队列任务、分布式锁
type RedisConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Password  string `json:"password"`
	DB        int    `json:"db"`
	KeyPrefix string `json:"key_prefix"` // 用于隔离项目
}

type ResilienceConfig struct {
	LLMRateLimit      RateLimitConfig      `json:"llm_rate_limit"`
	LLMCircuitBreaker CircuitBreakerConfig `json:"llm_circuit_breaker"`
	DecisionLock      DecisionLockConfig   `json:"decision_lock"`
}

type RateLimitConfig struct {
	Enabled       bool    `json:"enabled"`
	RatePerSecond float64 `json:"rate_per_second"`
	Burst         int     `json:"burst"`
	WaitTimeoutMS int     `json:"wait_timeout_ms"` //等待令牌时长（ms）
}

type CircuitBreakerConfig struct {
	Enabled              bool `json:"enabled"`
	MaxRequests          int  `json:"max_requests"`
	IntervalSeconds      int  `json:"interval_seconds"`
	TimeoutSeconds       int  `json:"timeout_seconds"`       // 打开多久后进入半开
	FailureThreshold     int  `json:"failure_threshold"`     //失败次数到多少后触发熔断
	ConsecutiveSuccesses int  `json:"consecutive_successes"` //成功多少次后恢复正常
}

type DecisionLockConfig struct {
	Enabled    bool   `json:"enabled"`
	TTLSeconds int    `json:"ttl_seconds"`
	KeyPrefix  string `json:"key_prefix"`
}

type ObservabilityConfig struct {
	Enabled       bool    `json:"enabled"`
	ServiceName   string  `json:"service_name"`
	Env           string  `json:"env"`
	OTELEndpoint  string  `json:"otel_endpoint"`
	SampleRatio   float64 `json:"sample_ratio"` // 采样率
	EnableMetrics bool    `json:"enable_metrics"`
	MetricsAddr   string  `json:"metrics_addr"` //是否开启 metrics
}

func Load() (Config, error) {
	cfg := defaultConfig()

	path := strings.TrimSpace(os.Getenv("SECKILL_AGENT_CONFIG"))
	if path == "" {
		path = defaultConfigPath
	}

	if err := loadFromFile(path, &cfg); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("read config file %s: %w", path, err)
		}
	}

	overrideFromEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.App.Name == "" {
		return errors.New("app.name is required")
	}
	if c.HTTP.Host == "" {
		return errors.New("http.host is required")
	}
	if c.HTTP.Port <= 0 {
		return errors.New("http.port must be greater than 0")
	}
	if c.Log.Level == "" {
		return errors.New("log.level is required")
	}
	if c.LLM.Provider == "" {
		return errors.New("llm.provider is required")
	}
	if c.LLM.BaseURL == "" {
		return errors.New("llm.base_url is required")
	}
	if c.LLM.APIKey == "" {
		return errors.New("llm.api_key is required")
	}
	if c.LLM.Model == "" {
		return errors.New("llm.model is required")
	}
	if c.Agent.MaxSteps <= 0 {
		return errors.New("agent.max_steps must be greater than 0")
	}
	if c.Agent.ToolCallTimeoutSeconds <= 0 {
		return errors.New("agent.tool_call_timeout_seconds must be greater than 0")
	}
	if c.AgentJob.Enabled && !c.Redis.Enabled {
		return errors.New("redis.enabled must be true when agent_job.enabled=true")
	}
	if c.AgentJob.Enabled {
		if c.AgentJob.WorkerCount <= 0 {
			return errors.New("agent_job.worker_count must be greater than 0")
		}
		if c.AgentJob.MaxRetries < 0 {
			return errors.New("agent_job.max_retries must not be negative")
		}
		if c.AgentJob.MaxBatchSize <= 0 {
			return errors.New("agent_job.max_batch_size must be greater than 0")
		}
		if c.AgentJob.GlobalConcurrency <= 0 {
			return errors.New("agent_job.global_concurrency must be greater than 0")
		}
		if c.AgentJob.ScopeConcurrency <= 0 {
			return errors.New("agent_job.scope_concurrency must be greater than 0")
		}
	}
	if c.Memory.MaxSessions <= 0 {
		return errors.New("memory.max_sessions must be greater than 0")
	}
	if c.Memory.MaxRecentSteps <= 0 {
		return errors.New("memory.max_recent_steps must be greater than 0")
	}
	if c.Memory.MaxSummaryChars <= 0 {
		return errors.New("memory.max_summary_chars must be greater than 0")
	}
	if c.Memory.SessionTTLSeconds <= 0 {
		return errors.New("memory.session_ttl_seconds must be greater than 0")
	}
	if c.PromptBudget.MaxPromptTokens <= 0 {
		return errors.New("prompt_budget.max_prompt_tokens must be greater than 0")
	}
	if c.PromptBudget.MaxRecentSteps <= 0 {
		return errors.New("prompt_budget.max_recent_steps must be greater than 0")
	}
	if c.PromptBudget.MaxSummaryChars <= 0 {
		return errors.New("prompt_budget.max_summary_chars must be greater than 0")
	}
	if c.Redis.Enabled && c.Redis.Addr == "" {
		return errors.New("redis.addr is required when redis.enabled=true")
	}
	if c.Resilience.LLMRateLimit.Enabled {
		if c.Resilience.LLMRateLimit.RatePerSecond <= 0 {
			return errors.New("resilience.llm_rate_limit.rate_per_second must be greater than 0")
		}
		if c.Resilience.LLMRateLimit.Burst <= 0 {
			return errors.New("resilience.llm_rate_limit.burst must be greater than 0")
		}
	}
	if c.Resilience.DecisionLock.Enabled && !c.Redis.Enabled {
		return errors.New("redis.enabled must be true when decision_lock.enabled=true")
	}
	if c.ReviewService.BaseURL == "" {
		return errors.New("review_service.base_url is required")
	}
	if c.ReviewService.TimeoutSeconds <= 0 {
		return errors.New("review_service.timeout_seconds must be greater than 0")
	}
	if c.ReviewJob.BaseURL == "" {
		return errors.New("review_job.base_url is required")
	}
	if c.ReviewJob.TimeoutSeconds <= 0 {
		return errors.New("review_job.timeout_seconds must be greater than 0")
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		App: AppConfig{
			Name:    "seckill-agent",
			Env:     "local",
			Version: "0.1.0",
		},
		HTTP: HTTPConfig{
			Host:                   "0.0.0.0",
			Port:                   8080, // 8070
			ReadTimeoutSeconds:     5,
			WriteTimeoutSeconds:    180, // 240s
			IdleTimeoutSeconds:     30,  // 空闲连接最多保留多久 30s
			ShutdownTimeoutSeconds: 10,  // 优雅退出信号后，优雅关闭 HTTP服务器 最多等待 60s
		},
		Log: LogConfig{
			Level:     "info",
			Format:    "text", // json
			AddSource: true,   // false, 附带源代码文件和行号
		},
		LLM: LLMConfig{
			Provider:       "deepseek",
			BaseURL:        "https://api.deepseek.com",
			Model:          "deepseek-chat",
			TimeoutSeconds: 20,   // 单次LLM请求最多等待 30
			MaxTokens:      1024, // 单次生成的token数 1200
			Temperature:    0.2,  // 输出随机度
		},
		Agent: AgentConfig{
			MaxSteps:               5,  // 6
			ToolCallTimeoutSeconds: 10, // 单个工具最多执行秒数 12
		},
		AgentJob: AgentJobConfig{ //异步任务队列
			Enabled:             false, // 开启redis异步任务队列
			WorkerCount:         2,     // 启动后台 6 个后台 Worker并发处理任务
			GlobalConcurrency:   6,
			ScopeConcurrency:    2,
			MaxRetries:          2,
			JobTTLSeconds:       86400, // 任务状态和结果在redis保存 7 天
			QueueWaitSeconds:    2,     // redis空队列的单次阻塞轮询时间 1s
			RunTimeoutSeconds:   180,   // 单次任务执行时长最多 4分钟
			RetryBackoffSeconds: 2,     // 失败后等待3秒再重新入队
			MaxBatchSize:        50,    // 批量提交任务数 100
		},
		Memory: MemoryConfig{
			MaxSessions:       1000,
			MaxRecentSteps:    24,
			MaxSummaryChars:   4800,
			SessionTTLSeconds: 1800, // 会话记忆保存2个小时
		},
		PromptBudget: PromptBudgetConfig{
			MaxPromptTokens: 2000,
			MaxRecentSteps:  6,
			MaxSummaryChars: 1200,
		},
		CouponPolicy: CouponPolicyConfig{
			Path:            "configs/coupon_policy.json", //优惠券决策策略文件
			HotReload:       true,                         //运行期间允许重新加载策略文件
			ReloadSeconds:   30,                           //最多每30秒检查一次策略变化
			DefaultValidity: 14,                           //优惠券默认有效期14天
		},
		// redis开启，会话管理用redis,关闭用内存管理
		Redis: RedisConfig{
			Enabled:   false,
			Addr:      "127.0.0.1:6380",
			Password:  "change-me-local",
			DB:        0,
			KeyPrefix: "seckill-agent:", //统一前缀
		},
		Resilience: ResilienceConfig{
			LLMRateLimit: RateLimitConfig{ // 限流
				Enabled:       true,
				RatePerSecond: 2,    // 每秒允许4次
				Burst:         4,    // 短时间内允许突发8次
				WaitTimeoutMS: 1500, // 没拿到令牌最多等3秒
			},
			LLMCircuitBreaker: CircuitBreakerConfig{ //熔断
				Enabled:              true,
				MaxRequests:          2,  // 半开4个探测请求
				IntervalSeconds:      30, //每60s重置一次失败统计
				TimeoutSeconds:       20, // 熔断打开30s之后进入半开状态
				FailureThreshold:     5,  //统计周期内累计失败8次之后打开熔断
				ConsecutiveSuccesses: 2,  // 半开状态连续成功3次后恢复正常
			},
			DecisionLock: DecisionLockConfig{ // 分布式锁（一个评价被发放重复决策、Job重试造成重复业务写入、多个实例同时发放同一业务优惠券）
				Enabled:    true,                  // Redis 启用时默认保护最终决策；单实例开发可显式关闭
				TTLSeconds: 120,                   // 锁最长持有2分钟
				KeyPrefix:  "seckill-agent:lock:", // 锁key的统一前缀
			},
		},
		Observability: ObservabilityConfig{
			Enabled:       true,
			ServiceName:   "seckill-agent",
			Env:           "dev",
			OTELEndpoint:  "127.0.0.1:4317", // Otel的grpc地址
			SampleRatio:   1.0,              // 采样20%的trace
			EnableMetrics: true,             //开启 Metrics
			MetricsAddr:   "0.0.0.0:2113",
		},
		ReviewService: ServiceEndpointConfig{
			BaseURL:        "http://127.0.0.1:8000",
			TimeoutSeconds: 5,
		},
		ReviewJob: ServiceEndpointConfig{
			BaseURL:        "http://127.0.0.1:8082",
			TimeoutSeconds: 5, //单次请求最多等待8秒
		},
		// 注册get_campaign_context工具，给finalizer注入发券服务
		SeckillService: SeckillServiceConfig{
			Enabled:        false,
			BaseURL:        "http://127.0.0.1:8001",
			GrantPath:      "/api/v1/seckill/coupon/review/grant",
			TimeoutSeconds: 3, //单次请求最多等待5秒
		},
	}
}

func loadFromFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, cfg)
}

func overrideFromEnv(cfg *Config) {
	setString(&cfg.App.Name, os.Getenv("SECKILL_AGENT_APP_NAME"))
	setString(&cfg.App.Env, os.Getenv("SECKILL_AGENT_APP_ENV"))
	setString(&cfg.App.Version, os.Getenv("SECKILL_AGENT_APP_VERSION"))

	setString(&cfg.HTTP.Host, os.Getenv("SECKILL_AGENT_HTTP_HOST"))
	setInt(&cfg.HTTP.Port, os.Getenv("SECKILL_AGENT_HTTP_PORT"))
	setInt(&cfg.HTTP.ReadTimeoutSeconds, os.Getenv("SECKILL_AGENT_HTTP_READ_TIMEOUT_SECONDS"))
	setInt(&cfg.HTTP.WriteTimeoutSeconds, os.Getenv("SECKILL_AGENT_HTTP_WRITE_TIMEOUT_SECONDS"))
	setInt(&cfg.HTTP.IdleTimeoutSeconds, os.Getenv("SECKILL_AGENT_HTTP_IDLE_TIMEOUT_SECONDS"))
	setInt(&cfg.HTTP.ShutdownTimeoutSeconds, os.Getenv("SECKILL_AGENT_HTTP_SHUTDOWN_TIMEOUT_SECONDS"))

	setString(&cfg.Log.Level, os.Getenv("SECKILL_AGENT_LOG_LEVEL"))
	setString(&cfg.Log.Format, os.Getenv("SECKILL_AGENT_LOG_FORMAT"))
	setBool(&cfg.Log.AddSource, os.Getenv("SECKILL_AGENT_LOG_ADD_SOURCE"))

	setString(&cfg.LLM.Provider, os.Getenv("SECKILL_AGENT_LLM_PROVIDER"))
	setString(&cfg.LLM.BaseURL, os.Getenv("SECKILL_AGENT_LLM_BASE_URL"))
	setString(&cfg.LLM.APIKey, os.Getenv("SECKILL_AGENT_LLM_API_KEY"))
	setString(&cfg.LLM.Model, os.Getenv("SECKILL_AGENT_LLM_MODEL"))
	setInt(&cfg.LLM.TimeoutSeconds, os.Getenv("SECKILL_AGENT_LLM_TIMEOUT_SECONDS"))
	setInt(&cfg.LLM.MaxTokens, os.Getenv("SECKILL_AGENT_LLM_MAX_TOKENS"))
	setFloat(&cfg.LLM.Temperature, os.Getenv("SECKILL_AGENT_LLM_TEMPERATURE"))

	setInt(&cfg.Agent.MaxSteps, os.Getenv("SECKILL_AGENT_AGENT_MAX_STEPS"))
	setInt(&cfg.Agent.ToolCallTimeoutSeconds, os.Getenv("SECKILL_AGENT_AGENT_TOOL_CALL_TIMEOUT_SECONDS"))
	setBool(&cfg.AgentJob.Enabled, os.Getenv("SECKILL_AGENT_JOB_ENABLED"))
	setInt(&cfg.AgentJob.WorkerCount, os.Getenv("SECKILL_AGENT_JOB_WORKER_COUNT"))
	setInt(&cfg.AgentJob.GlobalConcurrency, os.Getenv("SECKILL_AGENT_JOB_GLOBAL_CONCURRENCY"))
	setInt(&cfg.AgentJob.ScopeConcurrency, os.Getenv("SECKILL_AGENT_JOB_SCOPE_CONCURRENCY"))
	setInt(&cfg.AgentJob.MaxRetries, os.Getenv("SECKILL_AGENT_JOB_MAX_RETRIES"))
	setInt(&cfg.AgentJob.JobTTLSeconds, os.Getenv("SECKILL_AGENT_JOB_TTL_SECONDS"))
	setInt(&cfg.AgentJob.QueueWaitSeconds, os.Getenv("SECKILL_AGENT_JOB_QUEUE_WAIT_SECONDS"))
	setInt(&cfg.AgentJob.RunTimeoutSeconds, os.Getenv("SECKILL_AGENT_JOB_RUN_TIMEOUT_SECONDS"))
	setInt(&cfg.AgentJob.RetryBackoffSeconds, os.Getenv("SECKILL_AGENT_JOB_RETRY_BACKOFF_SECONDS"))
	setInt(&cfg.AgentJob.MaxBatchSize, os.Getenv("SECKILL_AGENT_JOB_MAX_BATCH_SIZE"))

	setInt(&cfg.Memory.MaxSessions, os.Getenv("SECKILL_AGENT_MEMORY_MAX_SESSIONS"))
	setInt(&cfg.Memory.MaxRecentSteps, os.Getenv("SECKILL_AGENT_MEMORY_MAX_RECENT_STEPS"))
	setInt(&cfg.Memory.MaxSummaryChars, os.Getenv("SECKILL_AGENT_MEMORY_MAX_SUMMARY_CHARS"))
	setInt(&cfg.Memory.SessionTTLSeconds, os.Getenv("SECKILL_AGENT_MEMORY_SESSION_TTL_SECONDS"))
	setInt(&cfg.PromptBudget.MaxPromptTokens, os.Getenv("SECKILL_AGENT_PROMPT_BUDGET_MAX_PROMPT_TOKENS"))
	setInt(&cfg.PromptBudget.MaxRecentSteps, os.Getenv("SECKILL_AGENT_PROMPT_BUDGET_MAX_RECENT_STEPS"))
	setInt(&cfg.PromptBudget.MaxSummaryChars, os.Getenv("SECKILL_AGENT_PROMPT_BUDGET_MAX_SUMMARY_CHARS"))

	setString(&cfg.CouponPolicy.Path, os.Getenv("SECKILL_AGENT_COUPON_POLICY_PATH"))
	setBool(&cfg.CouponPolicy.HotReload, os.Getenv("SECKILL_AGENT_COUPON_POLICY_HOT_RELOAD"))
	setInt(&cfg.CouponPolicy.ReloadSeconds, os.Getenv("SECKILL_AGENT_COUPON_POLICY_RELOAD_SECONDS"))
	setInt(&cfg.CouponPolicy.DefaultValidity, os.Getenv("SECKILL_AGENT_COUPON_POLICY_DEFAULT_VALIDITY_DAYS"))

	setBool(&cfg.Redis.Enabled, os.Getenv("SECKILL_AGENT_REDIS_ENABLED"))
	setString(&cfg.Redis.Addr, os.Getenv("SECKILL_AGENT_REDIS_ADDR"))
	setString(&cfg.Redis.Password, os.Getenv("SECKILL_AGENT_REDIS_PASSWORD"))
	setInt(&cfg.Redis.DB, os.Getenv("SECKILL_AGENT_REDIS_DB"))
	setString(&cfg.Redis.KeyPrefix, os.Getenv("SECKILL_AGENT_REDIS_KEY_PREFIX"))

	setBool(&cfg.Resilience.LLMRateLimit.Enabled, os.Getenv("SECKILL_AGENT_LLM_RATE_LIMIT_ENABLED"))
	setFloat(&cfg.Resilience.LLMRateLimit.RatePerSecond, os.Getenv("SECKILL_AGENT_LLM_RATE_LIMIT_RATE_PER_SECOND"))
	setInt(&cfg.Resilience.LLMRateLimit.Burst, os.Getenv("SECKILL_AGENT_LLM_RATE_LIMIT_BURST"))
	setInt(&cfg.Resilience.LLMRateLimit.WaitTimeoutMS, os.Getenv("SECKILL_AGENT_LLM_RATE_LIMIT_WAIT_TIMEOUT_MS"))
	setBool(&cfg.Resilience.LLMCircuitBreaker.Enabled, os.Getenv("SECKILL_AGENT_LLM_CIRCUIT_BREAKER_ENABLED"))
	setInt(&cfg.Resilience.LLMCircuitBreaker.MaxRequests, os.Getenv("SECKILL_AGENT_LLM_CIRCUIT_BREAKER_MAX_REQUESTS"))
	setInt(&cfg.Resilience.LLMCircuitBreaker.IntervalSeconds, os.Getenv("SECKILL_AGENT_LLM_CIRCUIT_BREAKER_INTERVAL_SECONDS"))
	setInt(&cfg.Resilience.LLMCircuitBreaker.TimeoutSeconds, os.Getenv("SECKILL_AGENT_LLM_CIRCUIT_BREAKER_TIMEOUT_SECONDS"))
	setInt(&cfg.Resilience.LLMCircuitBreaker.FailureThreshold, os.Getenv("SECKILL_AGENT_LLM_CIRCUIT_BREAKER_FAILURE_THRESHOLD"))
	setInt(&cfg.Resilience.LLMCircuitBreaker.ConsecutiveSuccesses, os.Getenv("SECKILL_AGENT_LLM_CIRCUIT_BREAKER_CONSECUTIVE_SUCCESSES"))
	setBool(&cfg.Resilience.DecisionLock.Enabled, os.Getenv("SECKILL_AGENT_DECISION_LOCK_ENABLED"))
	setInt(&cfg.Resilience.DecisionLock.TTLSeconds, os.Getenv("SECKILL_AGENT_DECISION_LOCK_TTL_SECONDS"))
	setString(&cfg.Resilience.DecisionLock.KeyPrefix, os.Getenv("SECKILL_AGENT_DECISION_LOCK_KEY_PREFIX"))

	setBool(&cfg.Observability.Enabled, os.Getenv("SECKILL_AGENT_OBSERVABILITY_ENABLED"))
	setString(&cfg.Observability.ServiceName, os.Getenv("SECKILL_AGENT_OBSERVABILITY_SERVICE_NAME"))
	setString(&cfg.Observability.Env, os.Getenv("SECKILL_AGENT_OBSERVABILITY_ENV"))
	setString(&cfg.Observability.OTELEndpoint, os.Getenv("SECKILL_AGENT_OBSERVABILITY_OTEL_ENDPOINT"))
	setFloat(&cfg.Observability.SampleRatio, os.Getenv("SECKILL_AGENT_OBSERVABILITY_SAMPLE_RATIO"))
	setBool(&cfg.Observability.EnableMetrics, os.Getenv("SECKILL_AGENT_OBSERVABILITY_ENABLE_METRICS"))
	setString(&cfg.Observability.MetricsAddr, os.Getenv("SECKILL_AGENT_OBSERVABILITY_METRICS_ADDR"))

	setString(&cfg.ReviewService.BaseURL, os.Getenv("SECKILL_AGENT_REVIEW_SERVICE_BASE_URL"))
	setInt(&cfg.ReviewService.TimeoutSeconds, os.Getenv("SECKILL_AGENT_REVIEW_SERVICE_TIMEOUT_SECONDS"))
	setString(&cfg.ReviewJob.BaseURL, os.Getenv("SECKILL_AGENT_REVIEW_JOB_BASE_URL"))
	setInt(&cfg.ReviewJob.TimeoutSeconds, os.Getenv("SECKILL_AGENT_REVIEW_JOB_TIMEOUT_SECONDS"))

	setBool(&cfg.SeckillService.Enabled, os.Getenv("SECKILL_AGENT_SECKILL_SERVICE_ENABLED"))
	setString(&cfg.SeckillService.BaseURL, os.Getenv("SECKILL_AGENT_SECKILL_SERVICE_BASE_URL"))
	setString(&cfg.SeckillService.GrantPath, os.Getenv("SECKILL_AGENT_SECKILL_SERVICE_GRANT_PATH"))
	setInt(&cfg.SeckillService.TimeoutSeconds, os.Getenv("SECKILL_AGENT_SECKILL_SERVICE_TIMEOUT_SECONDS"))
}

func setString(target *string, value string) {
	if strings.TrimSpace(value) != "" {
		*target = value
	}
}

func setInt(target *int, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = parsed
	}
}

func setBool(target *bool, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if parsed, err := strconv.ParseBool(value); err == nil {
		*target = parsed
	}
}

func setFloat(target *float64, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		*target = parsed
	}
}
