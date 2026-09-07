package contextx

// 面向LLM压缩前的视图（无 MemoryKey（存储层内部实现细节） 和 updataAt（缓存管理信息，LLM不需要知道））
type View struct {
	SessionID   string       `json:"session_id"`
	ExecutionID string       `json:"execution_id,omitempty"`
	Task        string       `json:"task"`
	Summary     string       `json:"summary,omitempty"`
	RecentSteps []StepRecord `json:"recent_steps"`
}
