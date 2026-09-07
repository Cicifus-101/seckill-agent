package memory

import (
	"seckill-agent/internal/contextx"
	"time"
)

// 短期记忆的存储结构，用于进行持久化和用于恢复session
type SessionState struct {
	MemoryKey   string                `json:"-"`
	SessionID   string                `json:"session_id"`
	ExecutionID string                `json:"execution_id,omitempty"`
	Task        string                `json:"task,omitempty"`
	Summary     string                `json:"summary,omitempty"` //eg:已获取评价证据，评价为五星，包含图片，用户无申诉。
	RecentSteps []contextx.StepRecord `json:"recent_steps,omitempty"`
	UpdatedAt   time.Time             `json:"updated_at"` //TTL判断、最久未使用淘汰、Redis过期刷新
}

type View = contextx.View
