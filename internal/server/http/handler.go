package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"seckill-agent/internal/agent"
	"seckill-agent/internal/config"
	"seckill-agent/internal/llm"
	"time"
)

type Handler struct {
	cfg    config.Config
	logger *slog.Logger
	llm    llm.Client
	agent  *agent.Agent
}

type healthResponse struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	Env       string `json:"env"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
}

type charDebugRequest struct {
	SystemPrompt string `json:"system_prompt"`
	UserPrompt   string `json:"user_prompt"`
}

type chatDebugResponse struct {
	TraceID string `json:"trace_id"`
	Model   string `json:"model"`
	Output  string `json:"output"`
}

type agentRunRequest struct {
	Task string `json:"task"`
}

type agentRunResponse struct {
	Task        string       `json:"task"`
	FinalAnswer string       `json:"final_answer"`
	Steps       []agent.Step `json:"steps"`
}

func NewHandler(cfg config.Config, logger *slog.Logger, llmClient llm.Client, ag *agent.Agent) *Handler {
	return &Handler{
		cfg:    cfg,
		logger: logger,
		llm:    llmClient,
		agent:  ag,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("POST /api/v1/llm/chat", h.handleChatDebug)
	mux.HandleFunc("POST /api/v1/agent/run", h.handleAgentRun)
	return withLogging(h.logger, mux)
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:    "ok",
		Service:   h.cfg.App.Name,
		Env:       h.cfg.App.Env,
		Version:   h.cfg.App.Version,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (h *Handler) handleChatDebug(w http.ResponseWriter, r *http.Request) {
	var req charDebugRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if req.UserPrompt == "" {
		writeError(w, http.StatusBadRequest, "user_prompt is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.LLM.TimeoutSeconds)*time.Second)
	defer cancel()

	resp, err := h.llm.Chat(ctx, llm.ChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   req.UserPrompt,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "llm chat failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, chatDebugResponse{
		TraceID: resp.ID,
		Model:   resp.Model,
		Output:  resp.Content,
	})
}

func (h *Handler) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	var req agentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if req.Task == "" {
		writeError(w, http.StatusBadRequest, "task is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.LLM.TimeoutSeconds*h.cfg.Agent.MaxSteps)*time.Second)
	defer cancel()

	res, err := h.agent.Run(ctx, req.Task)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "agent run failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentRunResponse{
		Task:        res.Task,
		FinalAnswer: res.FinalAnswer,
		Steps:       res.Steps,
	})

}
