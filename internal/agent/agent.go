package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"seckill-agent/internal/llm"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/tool"
	"strings"
)

type Agent struct {
	llmClient llm.Client
	prompts   *prompt.Manager
	registry  *tool.Registry
	maxSteps  int
}

func New(llmClient llm.Client, prompts *prompt.Manager, registry *tool.Registry, maxSteps int) *Agent {
	return &Agent{
		llmClient: llmClient,
		prompts:   prompts,
		registry:  registry,
		maxSteps:  maxSteps,
	}
}

func (a *Agent) Run(ctx context.Context, task string) (Result, error) {
	steps := make([]Step, 0, a.maxSteps)
	history := make([]prompt.StepRecord, 0, a.maxSteps)

	for step := 1; step <= a.maxSteps; step++ {
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
			return Result{
				Task:        task,
				FinalAnswer: decision.FinalAnswer,
				Steps:       steps,
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
		stepItem := Step{
			Step:      step,
			ToolName:  decision.ToolName,
			Arguments: string(decision.Arguments),
		}

		if err != nil {
			stepItem.Error = err.Error()
			steps = append(steps, stepItem)
			history = append(history, prompt.StepRecord{
				Step:      step,
				ToolName:  decision.ToolName,
				Action:    "tool_call",
				Arguments: string(decision.Arguments),
				Error:     err.Error(),
			})
			continue
		}

		stepItem.Observation = observation
		steps = append(steps, stepItem)
		history = append(history, prompt.StepRecord{
			Step:      step,
			ToolName:  decision.ToolName,
			Action:    "tool_call",
			Arguments: string(decision.Arguments),
			Output:    observation,
		})
	}

	return Result{
		Task:        task,
		FinalAnswer: "达到最大迭代次数，未收敛到最终答案",
		Steps:       steps,
	}, nil
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
