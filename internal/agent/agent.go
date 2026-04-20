package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"seckill-agent/internal/contextx"
	"seckill-agent/internal/llm"
	"seckill-agent/internal/memory"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/tool"
)

type Agent struct {
	llmClient   llm.Client
	prompts     *prompt.Manager
	registry    *tool.Registry
	memoryStore memory.Store
	budget      contextx.Budget
	maxSteps    int
}

// 函数类型，实现之后可以选择性进行配置
type Option func(*Agent)

func WithMemoryStore(store memory.Store, budget contextx.Budget) Option {
	return func(a *Agent) {
		a.memoryStore = store
		if budget == (contextx.Budget{}) { // 如果没有指定预算
			budget = contextx.DefaultBudget()
		}
		a.budget = budget
	}
}

func New(llmClient llm.Client, prompts *prompt.Manager, registry *tool.Registry, maxSteps int, opts ...Option) *Agent {
	a := &Agent{
		llmClient: llmClient,
		prompts:   prompts,
		registry:  registry,
		budget:    contextx.DefaultBudget(),
		maxSteps:  maxSteps,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(a) // 函数可选项
		}
	}

	if a.maxSteps <= 0 {
		a.maxSteps = 1
	}

	return a
}

func (a *Agent) Run(ctx context.Context, task string) (Result, error) {
	return a.RunWithSession(ctx, "default", task)
}

func (a *Agent) RunWithSession(ctx context.Context, sessionID, task string) (Result, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	if strings.TrimSpace(task) == "" {
		return Result{}, fmt.Errorf("task is required")
	}

	a.bootstrapSession(ctx, sessionID, task)

	steps := make([]Step, 0, a.maxSteps)

	for step := 1; step <= a.maxSteps; step++ {
		// history的构建依赖View结构体，而View是LLM在每次调用时临时生成的，不进行短期存储，View的构建依赖SessionState
		//每调用一个工具，SessionState通过appendStep记录到最近调用工具里面，所以history在每次循环时都需要进行重构
		history := a.buildHistory(ctx, sessionID, task)

		systemPrompt := a.prompts.BuildSystemPrompt(a.registry.List())
		userPrompt := a.prompts.BuildUserPrompt(task, history)

		resp, err := a.llmClient.Chat(ctx, llm.ChatRequest{
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
		})
		if err != nil {
			return Result{}, err
		}

		decision, err := parseDecision(resp.Content)
		if err != nil {
			return Result{}, fmt.Errorf("parse llm decision failed: %w", err)
		}

		if strings.EqualFold(decision.Type, "final") {
			snapshot := a.snapshot(ctx, sessionID)
			return Result{
				SessionID:   sessionID,
				Task:        task,
				FinalAnswer: decision.FinalAnswer,
				Steps:       steps,
				Summary:     snapshot.Summary,
			}, nil
		}

		if !strings.EqualFold(decision.Type, "tool_call") {
			return Result{}, fmt.Errorf("unsupported decision type: %s", decision.Type)
		}

		t, ok := a.registry.Get(decision.ToolName)
		if !ok {
			return Result{}, fmt.Errorf("tool not found: %s", decision.ToolName)
		}

		observation, err := t.Execute(ctx, decision.Arguments)
		stepItem := Step{ // 向API进行返回
			Step:      step,
			ToolName:  decision.ToolName,
			Arguments: string(decision.Arguments),
		}

		record := memory.StepRecord{ // 面向内部记忆系统
			Step:      step,
			ToolName:  decision.ToolName,
			Action:    "tool_call",
			Arguments: string(decision.Arguments),
		}

		if err != nil {
			stepItem.Error = err.Error()
			record.Error = err.Error()
			steps = append(steps, stepItem)
			a.appendStep(ctx, sessionID, record) // 这里设计将错误返回给LLM，让他进行下一步的操作指示
			continue
		}

		stepItem.Observation = observation
		record.Output = observation
		steps = append(steps, stepItem)
		a.appendStep(ctx, sessionID, record)
	}

	snapshot := a.snapshot(ctx, sessionID)
	return Result{
		SessionID:   sessionID,
		Task:        task,
		FinalAnswer: "达到最大迭代次数，未收敛到最终答案",
		Steps:       steps,
		Summary:     snapshot.Summary,
	}, nil
}

func (a *Agent) bootstrapSession(ctx context.Context, sessionID, task string) {
	if a.memoryStore == nil {
		return
	}

	state, ok := a.memoryStore.Load(ctx, sessionID)
	if !ok {
		state = memory.SessionState{SessionID: sessionID}
	}

	state.SessionID = sessionID
	state.Task = task
	state.UpdatedAt = time.Now()
	a.memoryStore.Save(ctx, state)
}

func (a *Agent) appendStep(ctx context.Context, sessionID string, step memory.StepRecord) {
	if a.memoryStore == nil {
		return
	}
	a.memoryStore.AppendStep(ctx, sessionID, step)
}

func (a *Agent) snapshot(ctx context.Context, sessionID string) memory.View {
	if a.memoryStore == nil {
		return memory.View{SessionID: sessionID}
	}
	return a.memoryStore.Snapshot(ctx, sessionID)
}

func (a *Agent) buildHistory(ctx context.Context, sessionID, task string) []prompt.StepRecord {
	if a.memoryStore == nil {
		return nil
	}

	view := a.snapshot(ctx, sessionID)
	compressed := contextx.Compose(view, task, a.budget)

	history := make([]prompt.StepRecord, 0, len(compressed.RecentSteps)+2)

	// 历史记录的元数据，记录摘要和压缩
	if compressed.Summary != "" {
		history = append(history, prompt.StepRecord{
			Step:     0,
			ToolName: "memory",
			Action:   "memory_summary",
			Output:   compressed.Summary,
		})
	}

	if compressed.Truncated {
		history = append(history, prompt.StepRecord{
			Step:     0,
			ToolName: "memory",
			Action:   "context_truncated",
			Output:   fmt.Sprintf("estimated_tokens=%d", compressed.EstimatedTokens),
		})
	}

	for _, item := range compressed.RecentSteps {
		history = append(history, prompt.StepRecord{
			Step:      item.Step,
			ToolName:  item.ToolName,
			Action:    item.Action,
			Arguments: item.Arguments,
			Output:    item.Output,
			Error:     item.Error,
		})
	}

	return history
}

func parseDecision(content string) (Decision, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")

	var d Decision
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return Decision{}, err
	}
	return d, nil
}
