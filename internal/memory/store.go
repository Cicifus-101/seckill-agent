package memory

import (
	"context"
	"seckill-agent/internal/contextx"
	"sync"
	"time"
)

type Store interface {
	// memoryKey identifies one isolated memory record. The agent passes an
	// execution-scoped key here, not the parent session ID.
	Load(ctx context.Context, memoryKey string) (SessionState, bool)
	Save(ctx context.Context, state SessionState)
	ClearSession(ctx context.Context, memoryKey string)
	AppendStep(ctx context.Context, memoryKey string, step contextx.StepRecord)
	Snapshot(ctx context.Context, memoryKey string) View
}

type Config struct {
	MaxSessions       int
	MaxRecentSteps    int
	MaxSummaryChars   int
	SessionTTLSeconds int
}

// 这个实现的是临时存储，没有放进Redis或者数据库中
type InMemoryStore struct {
	mu     sync.Mutex
	cfg    Config
	states map[string]SessionState
}

func NewInMemoryStore(cfg Config) *InMemoryStore {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 1000
	}
	if cfg.MaxRecentSteps <= 0 {
		cfg.MaxRecentSteps = 8
	}
	if cfg.MaxSummaryChars <= 0 {
		cfg.MaxSummaryChars = 1200
	}
	if cfg.SessionTTLSeconds <= 0 {
		cfg.SessionTTLSeconds = 1800
	}

	return &InMemoryStore{
		cfg:    cfg,
		states: make(map[string]SessionState),
	}
}

func (s *InMemoryStore) Load(ctx context.Context, memoryKey string) (SessionState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if memoryKey == "" {
		memoryKey = "default"
	}

	state, ok := s.states[memoryKey]
	if !ok {
		return SessionState{}, false
	}

	if state.UpdatedAt.Add(time.Duration(s.cfg.SessionTTLSeconds) * time.Second).Before(time.Now()) {
		delete(s.states, memoryKey)
		return SessionState{}, false
	}

	state.MemoryKey = memoryKey
	return cloneState(state), true
}

func (s *InMemoryStore) Save(ctx context.Context, state SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state.SessionID == "" {
		state.SessionID = "default"
	}
	key := state.MemoryKey
	if key == "" {
		key = state.SessionID
	}

	state = normalizeState(state, s.cfg) // 会处理 maxSummary和 maxRecentSteps ，超出限制的旧步骤转入Summary
	state.MemoryKey = key
	s.states[key] = cloneState(state) // 保存状态，内存实现写入go map，redis实现写入redis key
	s.evictIfNeeded()                 // 容量淘汰，删除最旧的session
}

func (s *InMemoryStore) ClearSession(ctx context.Context, memoryKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if memoryKey == "" {
		memoryKey = "default"
	}

	delete(s.states, memoryKey)
}

func (s *InMemoryStore) AppendStep(ctx context.Context, memoryKey string, step contextx.StepRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if memoryKey == "" {
		memoryKey = "default"
	}

	state := s.states[memoryKey]
	if state.SessionID == "" {
		state.SessionID = memoryKey
	}
	state.MemoryKey = memoryKey
	state.UpdatedAt = time.Now()
	state.RecentSteps = append(state.RecentSteps, step)

	if len(state.RecentSteps) > s.cfg.MaxRecentSteps {
		overflow := len(state.RecentSteps) - s.cfg.MaxRecentSteps
		if overflow > 0 {
			state.Summary = mergeSummary(state.Summary, summarizeSteps(state.RecentSteps[:overflow]))
			state.RecentSteps = append([]contextx.StepRecord(nil), state.RecentSteps[overflow:]...)
			state.Summary = trimText(state.Summary, s.cfg.MaxSummaryChars)
		}
	}

	s.states[memoryKey] = cloneState(state)
	s.evictIfNeeded()
}

func (s *InMemoryStore) Snapshot(ctx context.Context, memoryKey string) View {
	state, ok := s.Load(ctx, memoryKey)
	if !ok {
		return View{SessionID: memoryKey}
	}

	return View{
		SessionID:   state.SessionID,
		ExecutionID: state.ExecutionID,
		Task:        state.Task,
		Summary:     state.Summary,
		RecentSteps: append([]contextx.StepRecord(nil), state.RecentSteps...),
	}
}

func normalizeState(state SessionState, cfg Config) SessionState {
	state.UpdatedAt = time.Now()
	state.Summary = trimText(state.Summary, cfg.MaxSummaryChars)

	if len(state.RecentSteps) > cfg.MaxRecentSteps {
		overflow := len(state.RecentSteps) - cfg.MaxRecentSteps
		state.Summary = mergeSummary(state.Summary, summarizeSteps(state.RecentSteps[:overflow]))
		state.RecentSteps = append([]contextx.StepRecord(nil), state.RecentSteps[overflow:]...)
		state.Summary = trimText(state.Summary, cfg.MaxSummaryChars)
	}

	return state
}

func cloneState(state SessionState) SessionState {
	cloned := state
	cloned.RecentSteps = append([]contextx.StepRecord(nil), state.RecentSteps...)
	return cloned
}

// lru 缓存淘汰
func (s *InMemoryStore) evictIfNeeded() {
	if len(s.states) <= s.cfg.MaxSessions {
		return
	}

	var oldestKey string
	var oldestTime time.Time
	first := true

	for k, v := range s.states {
		if first || v.UpdatedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.UpdatedAt
			first = false
		}
	}

	delete(s.states, oldestKey)
}

func mergeSummary(current, extra string) string {
	if current == "" {
		return extra
	}
	if extra == "" {
		return current
	}
	return current + "\n" + extra
}
