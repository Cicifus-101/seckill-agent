package memory

import (
	"context"
	"encoding/json"
	"seckill-agent/internal/contextx"
	"sync"
	"time"
)

// KeyValueClient
type KeyValueClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}

type RedisStore struct {
	client  KeyValueClient
	config  Config
	prefix  string     // 前缀是 agent:session；完整 key 还包含 execution scope
	mu      sync.Mutex //并发
	errMu   sync.RWMutex
	lastErr error
}

func NewRedisStore(client KeyValueClient, config Config, prefix string) *RedisStore {
	if config.MaxSessions <= 0 {
		config.MaxSessions = 1000
	}
	if config.MaxRecentSteps <= 0 {
		config.MaxRecentSteps = 8
	}
	if config.MaxSummaryChars <= 0 {
		config.MaxSummaryChars = 1200
	}
	if config.SessionTTLSeconds <= 0 {
		config.SessionTTLSeconds = 1800 // 30分钟
	}
	if prefix == "" {
		prefix = "agent:session:"
	}
	return &RedisStore{
		client: client,
		config: config,
		prefix: prefix,
	}
}

// 从redis 中加载一个完整对话
func (s *RedisStore) Load(ctx context.Context, memoryKey string) (SessionState, bool) {
	if memoryKey == "" {
		memoryKey = "default"
	}
	raw, err := s.client.Get(ctx, s.key(memoryKey))
	if err != nil || raw == "" {
		if err != nil {
			s.setError(err)
		} else {
			s.setError(nil)
		}
		return SessionState{}, false
	}

	var state SessionState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		s.setError(err)
		return SessionState{}, false
	}
	s.setError(nil)
	state.MemoryKey = memoryKey
	state = normalizeState(state, s.config) // 数据规范化
	return state, true
}

// 将SessionState 序列化后写入redis中
func (s *RedisStore) Save(ctx context.Context, state SessionState) {
	if state.SessionID == "" {
		state.SessionID = "default"
	}
	key := state.MemoryKey
	if key == "" {
		key = state.SessionID
	}
	state = normalizeState(state, s.config)
	state.MemoryKey = key
	raw, err := json.Marshal(state)
	if err != nil {
		s.setError(err)
		return
	}
	if err := s.client.Set(ctx, s.key(key), string(raw), time.Duration(s.config.SessionTTLSeconds)*time.Second); err != nil {
		s.setError(err)
		return
	}
	s.setError(nil)
}

func (s *RedisStore) AppendStep(ctx context.Context, memoryKey string, step contextx.StepRecord) {
	s.mu.Lock()
	defer s.mu.Unlock() // 只能保护当前Go进程，不能保护多个Agent实例之间的并发

	state, ok := s.Load(ctx, memoryKey)
	if !ok {
		state = SessionState{ // 不存在就创建新状态
			MemoryKey: memoryKey,
			SessionID: memoryKey,
		}
	}
	state.RecentSteps = append(state.RecentSteps, step)
	s.Save(ctx, state)
}

// 清除会话
func (s *RedisStore) ClearSession(ctx context.Context, memoryKey string) {
	if memoryKey == "" {
		memoryKey = "default"
	}
	if err := s.client.Del(ctx, s.key(memoryKey)); err != nil {
		s.setError(err)
		return
	}
	s.setError(nil)
}

func (s *RedisStore) Snapshot(ctx context.Context, memoryKey string) View {
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

func (s *RedisStore) key(memoryKey string) string {
	return s.prefix + memoryKey
}

// LastError exposes the most recent Redis/serialization failure to the Agent
// layer without changing the existing Store interface. A successful memory
// operation clears the error.
func (s *RedisStore) LastError() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.lastErr
}

func (s *RedisStore) setError(err error) {
	s.errMu.Lock()
	s.lastErr = err
	s.errMu.Unlock()
}
