package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_UsesFileAndEnvOverrides(t *testing.T) {

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	file := []byte(`{
		"app": {
			"name": "seckill-agent",
			"env": "local",
			"version": "0.1.0"
		},
		"http": {
			"host": "0.0.0.0",
			"port": 8070,
			"read_timeout_seconds": 5,
			"write_timeout_seconds": 10,
			"idle_timeout_seconds": 30,
			"shutdown_timeout_seconds": 10
		},
		"log": {
			"level": "info",
			"format": "text",
			"add_source": true
		},
		"llm": {
			"provider": "deepseek",
			"base_url": "https://api.deepseek.com",
			"api_key": "file-api-key",
			"model": "deepseek-chat",
			"timeout_seconds": 20,
			"max_tokens": 1024,
			"temperature": 0.2
		},
		"agent": {
			"max_steps": 5,
			"tool_call_timeout_seconds": 10
		}
	}`)

	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	t.Setenv("SECKILL_AGENT_CONFIG", path)
	t.Setenv("SECKILL_AGENT_HTTP_PORT", "9090")
	t.Setenv("SECKILL_AGENT_LLM_API_KEY", "env-api-key")
	t.Setenv("SECKILL_AGENT_AGENT_MAX_STEPS", "8")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.HTTP.Port != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.HTTP.Port)
	}
	if cfg.LLM.APIKey != "env-api-key" {
		t.Fatalf("expected api key from env, got %s", cfg.LLM.APIKey)
	}
	if cfg.Agent.MaxSteps != 8 {
		t.Fatalf("expected max steps 8, got %d", cfg.Agent.MaxSteps)
	}
	if cfg.App.Name != "seckill-agent" {
		t.Fatalf("unexpected app name: %s", cfg.App.Name)
	}
}
