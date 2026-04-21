package memory

import (
	"seckill-agent/internal/contextx"
	"time"
)

// 这个是系统内部存储的没有进行处理的上下文和历史，用于进行持久化和用于恢复session
type SessionState struct {
	SessionID   string                `json:"session_id"`
	Task        string                `json:"task,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	RecentSteps []contextx.StepRecord `json:"recent_steps,omitempty"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type View = contextx.View
