package memory

import (
	"context"
	"seckill-agent/internal/contextx"
	"sync"
	"time"
)

type Store interface {
	Load(ctx context.Context, sessionID string) (SessionState, bool)
	Save(ctx context.Context, state SessionState)
	AppendStep(ctx context.Context, sessionID string, step contextx.StepRecord)
	Snapshot(ctx context.Context, sessionID string) View
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

func (s *InMemoryStore) Load(ctx context.Context, sessionID string) (SessionState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID == "" {
		sessionID = "default"
	}

	state, ok := s.states[sessionID]
	if !ok {
		return SessionState{}, false
	}

	if state.UpdatedAt.Add(time.Duration(s.cfg.SessionTTLSeconds) * time.Second).Before(time.Now()) {
		delete(s.states, sessionID)
		return SessionState{}, false
	}

	return cloneState(state), true
}

func (s *InMemoryStore) Save(ctx context.Context, state SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state.SessionID == "" {
		state.SessionID = "default"
	}

	state = normalizeState(state, s.cfg)
	s.states[state.SessionID] = cloneState(state)
	s.evictIfNeeded()
}

func (s *InMemoryStore) AppendStep(ctx context.Context, sessionID string, step contextx.StepRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID == "" {
		sessionID = "default"
	}

	state := s.states[sessionID]
	state.SessionID = sessionID
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

	s.states[sessionID] = cloneState(state)
	s.evictIfNeeded()
}

func (s *InMemoryStore) Snapshot(ctx context.Context, sessionID string) View {
	state, ok := s.Load(ctx, sessionID)
	if !ok {
		return View{SessionID: sessionID}
	}

	return View{
		SessionID:   state.SessionID,
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
