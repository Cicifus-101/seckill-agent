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

type SessionState struct {
	SessionID   string       `json:"session_id"`
	Task        string       `json:"task,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	RecentSteps []StepRecord `json:"recent_steps,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type View struct {
	SessionID   string       `json:"session_id"`
	Task        string       `json:"task"`
	Summary     string       `json:"summary,omitempty"`
	RecentSteps []StepRecord `json:"recent_steps"`
}
