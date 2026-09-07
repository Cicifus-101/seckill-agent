package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"seckill-agent/internal/agent"
	"seckill-agent/internal/config"
	"seckill-agent/internal/job"
	"seckill-agent/internal/llm"
	"seckill-agent/pkg/logx"
	"strings"
	"time"
)

type Handler struct {
	cfg    config.Config
	logger *slog.Logger
	llm    llm.Client
	agent  *agent.Agent
	jobs   *job.Service
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
	SessionID         string `json:"session_id,omitempty"`
	ExecutionID       string `json:"execution_id,omitempty"`
	ParentExecutionID string `json:"parent_execution_id,omitempty"`
	Task              string `json:"task"`
}

type agentRunResponse struct {
	SessionID         string       `json:"session_id"`
	ExecutionID       string       `json:"execution_id,omitempty"`
	ParentExecutionID string       `json:"parent_execution_id,omitempty"`
	Task              string       `json:"task"`
	FinalAnswer       string       `json:"final_answer"`
	Summary           string       `json:"summary,omitempty"`
	Steps             []agent.Step `json:"steps"`
}

type agentJobSubmitResponse struct {
	JobID             string `json:"job_id"`
	SessionID         string `json:"session_id"`
	ExecutionID       string `json:"execution_id"`
	ParentExecutionID string `json:"parent_execution_id,omitempty"`
	Status            string `json:"status"`
}

type agentJobBatchRequest struct {
	Items []job.Request `json:"items"`
}

type agentJobBatchResponse struct {
	Jobs []agentJobSubmitResponse `json:"jobs"`
}

func NewHandler(cfg config.Config, logger *slog.Logger, llmClient llm.Client, ag *agent.Agent, jobs *job.Service) *Handler {
	return &Handler{
		cfg:    cfg,
		logger: logger,
		llm:    llmClient,
		agent:  ag,
		jobs:   jobs,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("POST /api/v1/llm/chat", h.handleChatDebug)
	mux.HandleFunc("POST /api/v1/agent/run", h.handleAgentRun)
	mux.HandleFunc("POST /api/v1/agent/jobs", h.handleAgentJobSubmit)
	mux.HandleFunc("POST /api/v1/agent/jobs/batch", h.handleAgentJobBatchSubmit)
	mux.HandleFunc("GET /api/v1/agent/jobs/{job_id}", h.handleAgentJobGet)
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
		logx.WithContext(h.logger, r.Context()).ErrorContext(r.Context(), "llm chat failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, chatDebugResponse{
		TraceID: resp.ID,
		Model:   resp.Model,
		Output:  resp.Content,
	})
}

// handleAgentRun 同步执行
func (h *Handler) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	var req agentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if strings.TrimSpace(req.Task) == "" {
		writeError(w, http.StatusBadRequest, "task is required")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "sync:default" // 同步请求默认对话
	}
	executionID := strings.TrimSpace(req.ExecutionID)
	if executionID == "" {
		executionID = "sync:" + time.Now().Format("20060102150405.000000000")
	}

	// 总超时=（LLM超时+ 工具超时） × 最大步数=4分钟
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.LLM.TimeoutSeconds*h.cfg.Agent.MaxSteps+h.cfg.Agent.ToolCallTimeoutSeconds*h.cfg.Agent.MaxSteps)*time.Second)
	defer cancel()

	res, err := h.agent.RunExecution(ctx, agent.ExecutionInput{
		SessionID:         sessionID,
		ExecutionID:       executionID,
		ParentExecutionID: strings.TrimSpace(req.ParentExecutionID),
		Task:              req.Task,
	})
	if err != nil {
		logx.WithContext(h.logger, r.Context()).ErrorContext(r.Context(), "agent run failed", "error", err, "session_id", sessionID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentRunResponse{
		SessionID:         res.SessionID,
		ExecutionID:       res.ExecutionID,
		ParentExecutionID: res.ParentExecutionID,
		Task:              res.Task,
		FinalAnswer:       res.FinalAnswer,
		Summary:           res.Summary,
		Steps:             res.Steps,
	})
}

// handleAgentJobSubmit 异步执行
func (h *Handler) handleAgentJobSubmit(w http.ResponseWriter, r *http.Request) {
	// 这里jobs类型为指针，主要是可以表示“不存在”
	if h.jobs == nil || !h.jobs.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "agent job queue is not enabled")
		return
	}
	var req job.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	record, err := h.jobs.Submit(r.Context(), req) // 将job record写入redis，将job_id 推入redis队列
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 202 请求接收，但是任务尚未完成
	writeJSON(w, http.StatusAccepted, agentJobSubmitResponse{JobID: record.ID, SessionID: record.SessionID, ExecutionID: record.ExecutionID, ParentExecutionID: record.ParentExecutionID, Status: record.Status})
}

func (h *Handler) handleAgentJobBatchSubmit(w http.ResponseWriter, r *http.Request) {
	if h.jobs == nil || !h.jobs.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "agent job queue is not enabled")
		return
	}
	var req agentJobBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required")
		return
	}
	if len(req.Items) > h.cfg.AgentJob.MaxBatchSize {
		writeError(w, http.StatusBadRequest, "items exceeds max_batch_size")
		return
	}
	resp := agentJobBatchResponse{Jobs: make([]agentJobSubmitResponse, 0, len(req.Items))}
	for _, item := range req.Items {
		record, err := h.jobs.Submit(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp.Jobs = append(resp.Jobs, agentJobSubmitResponse{JobID: record.ID, SessionID: record.SessionID, ExecutionID: record.ExecutionID, ParentExecutionID: record.ParentExecutionID, Status: record.Status})
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// 任务状态查询
func (h *Handler) handleAgentJobGet(w http.ResponseWriter, r *http.Request) {
	if h.jobs == nil || !h.jobs.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "agent job queue is not enabled")
		return
	}
	id := strings.TrimSpace(r.PathValue("job_id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "job_id is required")
		return
	}
	record, ok, err := h.jobs.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, record)
}
