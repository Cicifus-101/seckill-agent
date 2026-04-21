package contextx

// 压缩后的视图，包括历史摘要和最近几步，面向LLM
type View struct {
	SessionID   string       `json:"session_id"`
	Task        string       `json:"task"`
	Summary     string       `json:"summary,omitempty"`
	RecentSteps []StepRecord `json:"recent_steps"`
}
