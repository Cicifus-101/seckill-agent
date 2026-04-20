package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultConfigPath = "config/config.json"

type Config struct {
	App    AppConfig    `json:"app"`
	HTTP   HTTPConfig   `json:"http"`
	Log    LogConfig    `json:"log"`
	LLM    LLMConfig    `json:"llm"`
	Agent  AgentConfig  `json:"agent"`
	Memory MemoryConfig `json:"memory"`
}

type AppConfig struct {
	Name    string `json:"name"`
	Env     string `json:"env"`
	Version string `json:"version"`
}

type HTTPConfig struct {
	Host                   string `json:"host"`
	Port                   int    `json:"port"`
	ReadTimeoutSeconds     int    `json:"read_timeout_seconds"`
	WriteTimeoutSeconds    int    `json:"write_timeout_seconds"`
	IdleTimeoutSeconds     int    `json:"idle_timeout_seconds"`
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds"`
}

type LogConfig struct {
	Level     string `json:"level"`
	Format    string `json:"format"`
	AddSource bool   `json:"addSource"`
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
	MaxSteps              int `json:"max_steps"`
	ToolCallTimeoutSecond int `json:"tool_call_timeout_seconds"`
}

type MemoryConfig struct {
	MaxSessions       int `json:"max_sessions"`
	MaxRecentSteps    int `json:"max_recent_steps"`
	MaxSummaryChars   int `json:"max_summary_chars"`
	MaxPromptTokens   int `json:"max_prompt_tokens"`
	SessionTTLSeconds int `json:"session_ttl_seconds"`
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
	if c.Agent.ToolCallTimeoutSecond <= 0 {
		return errors.New("agent.tool_call_timeout_seconds must be greater than 0")
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
	if c.Memory.MaxPromptTokens <= 0 {
		return errors.New("memory.max_prompt_tokens must be greater than 0")
	}
	if c.Memory.SessionTTLSeconds <= 0 {
		return errors.New("memory.session_ttl_seconds must be greater than 0")
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
			Port:                   8080,
			ReadTimeoutSeconds:     5,
			WriteTimeoutSeconds:    10,
			IdleTimeoutSeconds:     30,
			ShutdownTimeoutSeconds: 10,
		},
		Log: LogConfig{
			Level:     "info",
			Format:    "text",
			AddSource: true,
		},
		LLM: LLMConfig{
			Provider:       "deepseek",
			BaseURL:        "https://api.deepseek.com",
			Model:          "deepseek-chat",
			TimeoutSeconds: 20,
			MaxTokens:      1024,
			Temperature:    0.2,
		},
		Agent: AgentConfig{
			MaxSteps:              5,
			ToolCallTimeoutSecond: 10,
		},
		Memory: MemoryConfig{
			MaxSessions:       1000,
			MaxRecentSteps:    6,
			MaxSummaryChars:   1200,
			MaxPromptTokens:   2000,
			SessionTTLSeconds: 1800,
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
	setInt(&cfg.Agent.ToolCallTimeoutSecond, os.Getenv("SECKILL_AGENT_AGENT_TOOL_CALL_TIMEOUT_SECONDS"))

	setInt(&cfg.Memory.MaxSessions, os.Getenv("SECKILL_AGENT_MEMORY_MAX_SESSIONS"))
	setInt(&cfg.Memory.MaxRecentSteps, os.Getenv("SECKILL_AGENT_MEMORY_MAX_RECENT_STEPS"))
	setInt(&cfg.Memory.MaxSummaryChars, os.Getenv("SECKILL_AGENT_MEMORY_MAX_SUMMARY_CHARS"))
	setInt(&cfg.Memory.MaxPromptTokens, os.Getenv("SECKILL_AGENT_MEMORY_MAX_PROMPT_TOKENS"))
	setInt(&cfg.Memory.SessionTTLSeconds, os.Getenv("SECKILL_AGENT_MEMORY_SESSION_TTL_SECONDS"))

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
