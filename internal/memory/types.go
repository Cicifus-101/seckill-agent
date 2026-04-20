package memory

import "time"

type StepRecord struct {
	Step      int    `json:"step"`
	ToolName  string `json:"tool_name,omitempty"`
	Action    string `json:"action"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
}

// 这个是系统内部存储的没有进行处理的上下文和历史，用于进行持久化和用于恢复session
type SessionState struct {
	SessionID   string       `json:"session_id"`
	Task        string       `json:"task,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	RecentSteps []StepRecord `json:"recent_steps,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// 压缩后的视图，包括历史摘要和最近几步，面向LLM
type View struct {
	SessionID   string       `json:"session_id"`
	Task        string       `json:"task"`
	Summary     string       `json:"summary,omitempty"`
	RecentSteps []StepRecord `json:"recent_steps"`
}
