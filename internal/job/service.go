package job

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"seckill-agent/internal/agent"
)

const (
	StatusPending    = "PENDING"
	StatusRunning    = "RUNNING"
	StatusRetrying   = "RETRYING"
	StatusSucceeded  = "SUCCEEDED"
	StatusIncomplete = "INCOMPLETE"
	StatusFailed     = "FAILED"
)

type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) error
	XReadGroup(ctx context.Context, group, consumer, stream string, count int64, block time.Duration) ([]StreamMessage, error)
	XAutoClaim(ctx context.Context, stream, group, consumer string, minIdle time.Duration, start string, count int64) (string, []StreamMessage, error)
	XAck(ctx context.Context, stream, group string, ids ...string) error
	XAdd(ctx context.Context, stream string, values map[string]any) (string, error)
}

type StreamMessage struct {
	ID    string
	JobID string
}

type Config struct {
	Enabled           bool
	KeyPrefix         string
	WorkerCount       int
	MaxRetries        int
	JobTTL            time.Duration // job record 保存的时间
	QueueWait         time.Duration // 超时之后worker回到循环，可以检查应用context是否已经取消，否则将一直阻塞直到有数据
	RunTimeout        time.Duration
	RetryBackoff      time.Duration
	MaxBatchSize      int
	GlobalConcurrency int
	ScopeConcurrency  int
}

type Request struct {
	SessionID         string `json:"session_id,omitempty"`
	ExecutionID       string `json:"execution_id,omitempty"`
	ParentExecutionID string `json:"parent_execution_id,omitempty"`
	StoreID           int64  `json:"store_id,omitempty"`
	ReviewID          int64  `json:"review_id,omitempty"`
	EvidenceVersion   int64  `json:"evidence_version,omitempty"`
	UserID            int64  `json:"user_id,omitempty"`
	SkuID             int64  `json:"sku_id,omitempty"`
	SpuID             int64  `json:"spu_id,omitempty"`
	SourceEventID     string `json:"source_event_id,omitempty"` //"review-updated:9001:v3"，评价9001的第3版变更事件出发了实时评价任务
	CampaignID        string `json:"campaign_id,omitempty"`     //ActivityID 为空时使用
	ActivityID        string `json:"activity_id,omitempty"`     //"activity-2026-618"，属于618活动
	PolicyVersion     string `json:"policy_version,omitempty"`
	Scene             string `json:"scene,omitempty"`        // 无 activityID 时，这批业务属于什么业务场景。daily_review_coupon、manual_review、negative_review_care、after_sale_followup
	TriggerType       string `json:"trigger_type,omitempty"` // 任务如何触发
	Window            string `json:"window,omitempty"`       // session业务时间窗口，"20260716"代表任务归入2026-07-16 的业务窗口
	Task              string `json:"task"`
	MaxRetries        *int   `json:"max_retries,omitempty"`
}

type Record struct {
	ID                string             `json:"job_id"`
	SessionID         string             `json:"session_id"`
	ExecutionID       string             `json:"execution_id"`
	ParentExecutionID string             `json:"parent_execution_id,omitempty"`
	StoreID           int64              `json:"store_id,omitempty"`
	ReviewID          int64              `json:"review_id,omitempty"`
	EvidenceVersion   int64              `json:"evidence_version,omitempty"`
	UserID            int64              `json:"user_id,omitempty"`
	SkuID             int64              `json:"sku_id,omitempty"`
	SpuID             int64              `json:"spu_id,omitempty"`
	SourceEventID     string             `json:"source_event_id,omitempty"`
	CampaignID        string             `json:"campaign_id,omitempty"`
	ActivityID        string             `json:"activity_id,omitempty"`
	PolicyVersion     string             `json:"policy_version,omitempty"`
	Scene             string             `json:"scene,omitempty"`
	TriggerType       string             `json:"trigger_type,omitempty"`
	Window            string             `json:"window,omitempty"`
	Task              string             `json:"task"`
	Status            string             `json:"status"`
	Attempts          int                `json:"attempts"` // 执行次数
	MaxRetries        int                `json:"max_retries"`
	Error             string             `json:"error,omitempty"`
	Result            *agent.Result      `json:"result,omitempty"`
	Executions        []ExecutionAttempt `json:"executions,omitempty"` // 每次尝试的历史
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	StartedAt         *time.Time         `json:"started_at,omitempty"`
	FinishedAt        *time.Time         `json:"finished_at,omitempty"`
	Version           int64              `json:"version"`
}

// 每次尝试的记录，执行摘要（不保存完整的 Agent memory），完整的在memmory key中
type ExecutionAttempt struct {
	Attempt           int        `json:"attempt"`
	ExecutionID       string     `json:"execution_id"`
	ParentExecutionID string     `json:"parent_execution_id,omitempty"`
	Status            string     `json:"status"`
	Error             string     `json:"error,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
}

type Service struct {
	cfg    Config
	client RedisClient
	agent  ExecutionRunner
	logger *slog.Logger
	group  string
}

type ExecutionRunner interface {
	RunExecution(ctx context.Context, input agent.ExecutionInput) (agent.Result, error)
}

func NewService(cfg Config, client RedisClient, ag ExecutionRunner, logger *slog.Logger) *Service {
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "seckill-agent:"
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.JobTTL <= 0 {
		cfg.JobTTL = 24 * time.Hour
	}
	if cfg.QueueWait <= 0 {
		cfg.QueueWait = 2 * time.Second
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 2 * time.Minute
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 2 * time.Second
	}
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 50
	}
	if cfg.GlobalConcurrency <= 0 {
		cfg.GlobalConcurrency = cfg.WorkerCount
	}
	if cfg.ScopeConcurrency <= 0 {
		cfg.ScopeConcurrency = 2
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{cfg: cfg, client: client, agent: ag, logger: logger, group: cfg.KeyPrefix + "agent:jobs:workers"}
}

// 即使nil指针调用，也不会因为访问字段 panic
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.client != nil && s.agent != nil
}

func (s *Service) Submit(ctx context.Context, req Request) (Record, error) {
	if !s.Enabled() {
		return Record{}, errors.New("agent job queue is not enabled")
	}
	req.Task = strings.TrimSpace(req.Task)
	if req.Task == "" {
		return Record{}, errors.New("task is required")
	}
	if req.ReviewID <= 0 {
		return Record{}, errors.New("review_id is required")
	}
	if req.StoreID <= 0 {
		return Record{}, errors.New("store_id is required")
	}
	if req.EvidenceVersion <= 0 {
		return Record{}, errors.New("evidence_version is required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = buildSessionID(req)
		if sessionID == "job" { // 缺少店铺ID专程随机session
			sessionID = "job:" + newID()
		}
	}
	executionID := strings.TrimSpace(req.ExecutionID)
	if executionID == "" {
		executionID = "exec:" + newID()
	}
	maxRetries := s.cfg.MaxRetries
	if req.MaxRetries != nil && *req.MaxRetries >= 0 {
		maxRetries = *req.MaxRetries
	}
	now := time.Now()
	record := Record{
		ID:                newID(),
		SessionID:         sessionID,
		ExecutionID:       executionID,
		ParentExecutionID: strings.TrimSpace(req.ParentExecutionID),
		StoreID:           req.StoreID,
		ReviewID:          req.ReviewID,
		EvidenceVersion:   req.EvidenceVersion,
		UserID:            req.UserID,
		SkuID:             req.SkuID,
		SpuID:             req.SpuID,
		SourceEventID:     strings.TrimSpace(req.SourceEventID),
		CampaignID:        strings.TrimSpace(req.CampaignID),
		ActivityID:        strings.TrimSpace(req.ActivityID),
		PolicyVersion:     strings.TrimSpace(req.PolicyVersion),
		Scene:             strings.TrimSpace(req.Scene),
		TriggerType:       normalizeTriggerType(req.TriggerType),
		Window:            strings.TrimSpace(req.Window),
		Task:              req.Task,
		Status:            StatusPending,
		MaxRetries:        maxRetries,
		CreatedAt:         now,
		UpdatedAt:         now,
		Version:           1,
	}
	created, existingID, err := s.createAndEnqueue(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if !created {
		existing, ok, getErr := s.Get(ctx, existingID)
		if getErr != nil {
			return Record{}, getErr
		}
		if !ok {
			return Record{}, fmt.Errorf("source event points to missing job: %s", existingID)
		}
		return existing, nil
	}
	s.logger.Info("agent job submitted", "job_id", record.ID, "session_id", record.SessionID, "execution_id", record.ExecutionID)
	return record, nil
}

func (s *Service) Get(ctx context.Context, id string) (Record, bool, error) {
	if !s.Enabled() {
		return Record{}, false, errors.New("agent job queue is not enabled")
	}
	raw, err := s.client.Get(ctx, s.jobKey(strings.TrimSpace(id)))
	if err != nil {
		return Record{}, false, err
	}
	if raw == "" {
		return Record{}, false, nil
	}
	var record Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

func (s *Service) Start(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	if err := s.client.XGroupCreateMkStream(ctx, s.queueKey(), s.group, "0"); err != nil && !strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		s.logger.Error("create agent job stream group failed", "error", err)
		return
	}
	for i := 0; i < s.cfg.WorkerCount; i++ {
		workerID := i + 1
		go s.worker(ctx, workerID)
	}
	go s.recoverProcessing(ctx, "recovery-"+newID())
}

func (s *Service) worker(ctx context.Context, workerID int) {
	logger := s.logger.With("worker_id", workerID)
	consumer := fmt.Sprintf("worker-%d-%s", workerID, newID())
	logger.Info("agent job worker started")
	for {
		select {
		case <-ctx.Done():
			logger.Info("agent job worker stopped")
			return
		default:
		}
		messages, err := s.client.XReadGroup(ctx, s.group, consumer, s.queueKey(), 1, s.cfg.QueueWait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		if len(messages) == 0 {
			continue
		}
		s.process(ctx, messages[0], logger)
	}
}

func (s *Service) process(parent context.Context, message StreamMessage, logger *slog.Logger) {
	id := strings.TrimSpace(message.JobID)
	if id == "" {
		_ = s.client.XAck(parent, s.queueKey(), s.group, message.ID)
		return
	}
	claimToken := newID()
	claimed, err := s.client.SetNX(parent, s.claimKey(id), claimToken, s.claimTTL())
	if err != nil {
		logger.Warn("claim agent job failed", "job_id", id, "error", err)
		return
	}
	if !claimed {
		return
	}
	defer s.releaseToken(s.claimKey(id), claimToken)

	record, ok, err := s.Get(parent, id)
	if err != nil {
		logger.Warn("agent job missing or invalid", "job_id", id, "error", err)
		return
	}
	if !ok {
		s.ackMessage(parent, message.ID)
		return
	}
	if isTerminal(record.Status) {
		s.ackMessage(parent, message.ID)
		return
	}
	if record.Status == StatusRetrying && time.Now().Before(record.UpdatedAt.Add(s.cfg.RetryBackoff)) {
		return
	}
	if record.Status == StatusRunning {
		failedAt := time.Now()
		record.finishLatestExecution(StatusFailed, "worker lease expired before completion", failedAt)
		record.ParentExecutionID = record.ExecutionID
		record.ExecutionID = "exec:" + newID()
		record.Status = StatusRetrying
		record.Error = "worker lease expired before completion"
	}

	reviewToken := newID()
	reviewClaimed, err := s.client.SetNX(parent, s.reviewExecutionKey(record), reviewToken, s.claimTTL())
	if err != nil {
		logger.Warn("claim review execution failed", "job_id", id, "error", err)
		return
	}
	if !reviewClaimed {
		return
	}
	defer s.releaseToken(s.reviewExecutionKey(record), reviewToken)

	limitToken := newID()
	allowed, err := s.acquireConcurrency(parent, record, limitToken)
	if err != nil {
		logger.Warn("acquire job concurrency slot failed", "job_id", id, "error", err)
		return
	}
	if !allowed {
		return
	}
	defer s.releaseConcurrency(record, limitToken)

	now := time.Now()
	record.Status = StatusRunning
	record.Attempts++
	record.UpdatedAt = now
	record.StartedAt = &now
	record.Error = ""
	record.Executions = append(record.Executions, ExecutionAttempt{
		Attempt:           record.Attempts,
		ExecutionID:       record.ExecutionID,
		ParentExecutionID: record.ParentExecutionID,
		Status:            StatusRunning,
		StartedAt:         now,
	})
	if err := s.save(parent, &record); err != nil {
		logger.Warn("save running agent job failed", "job_id", id, "error", err)
		return
	}

	runCtx, cancel := context.WithTimeout(parent, s.cfg.RunTimeout)
	stopLease := s.startLeaseRenewal(parent, id, record, claimToken, reviewToken, limitToken, cancel, logger)
	start := time.Now()
	result, runErr := s.agent.RunExecution(runCtx, agent.ExecutionInput{
		SessionID:         record.SessionID,
		ExecutionID:       record.ExecutionID,
		ParentExecutionID: record.ParentExecutionID,
		Task:              record.Task,
		ReviewID:          record.ReviewID,
		EvidenceVersion:   record.EvidenceVersion,
		StoreID:           record.StoreID,
		UserID:            record.UserID,
		SkuID:             record.SkuID,
		SpuID:             record.SpuID,
		ActivityID:        firstNonEmpty(record.ActivityID, record.CampaignID),
		PolicyVersion:     record.PolicyVersion,
	})
	stopLease()
	cancel() // 任务提前完成，也要释放Timer等资源
	duration := time.Since(start)
	if runErr == nil {
		finished := time.Now()
		record.Status = StatusSucceeded
		if !result.Completed {
			record.Status = StatusIncomplete
		}
		record.Result = &result
		record.UpdatedAt = finished
		record.FinishedAt = &finished
		record.finishLatestExecution(record.Status, "", finished)
		if err := s.save(parent, &record); err != nil {
			logger.Warn("save completed agent job failed", "job_id", id, "error", err)
			return
		}
		s.ackMessage(parent, message.ID)
		logger.Info("agent job completed", "job_id", id, "status", record.Status, "session_id", record.SessionID, "execution_id", record.ExecutionID, "attempts", record.Attempts, "duration_ms", duration.Milliseconds(), "steps", len(result.Steps))
		return
	}

	record.Error = runErr.Error()
	failedAt := time.Now()
	record.finishLatestExecution(StatusFailed, record.Error, failedAt)
	if record.Attempts <= record.MaxRetries {
		record.ParentExecutionID = record.ExecutionID
		record.ExecutionID = "exec:" + newID()
		record.Status = StatusRetrying
		record.UpdatedAt = time.Now()
		if err := s.save(parent, &record); err != nil {
			logger.Warn("save retrying agent job failed", "job_id", id, "error", err)
			return
		}
		logger.Warn("agent job retrying", "job_id", id, "session_id", record.SessionID, "next_execution_id", record.ExecutionID, "parent_execution_id", record.ParentExecutionID, "attempts", record.Attempts, "max_retries", record.MaxRetries, "error", record.Error, "duration_ms", duration.Milliseconds())
		return
	}
	finished := time.Now()
	record.Status = StatusFailed
	record.UpdatedAt = finished
	record.FinishedAt = &finished
	if err := s.save(parent, &record); err != nil {
		logger.Warn("save failed agent job failed", "job_id", id, "error", err)
		return
	}
	if _, err := s.client.XAdd(parent, s.deadLetterKey(), map[string]any{
		"job_id": record.ID, "execution_id": record.ExecutionID, "error": record.Error,
	}); err != nil {
		logger.Warn("write failed agent job to dead letter stream failed", "job_id", id, "error", err)
	}
	s.ackMessage(parent, message.ID)
	logger.Error("agent job failed", "job_id", id, "attempts", record.Attempts, "error", record.Error, "duration_ms", duration.Milliseconds())
}

// startLeaseRenewal keeps the worker claim, review claim, and concurrency
// slot alive while the agent is running. If ownership is lost, cancel the
// execution so an expired worker cannot continue making business changes.
func (s *Service) startLeaseRenewal(ctx context.Context, jobID string, record Record, claimToken, reviewToken, limitToken string, cancel context.CancelFunc, logger *slog.Logger) func() {
	interval := s.claimTTL() / 3
	if interval < time.Second {
		interval = time.Second
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(ctx, 3*time.Second)
				ok, err := s.renewTokenLease(renewCtx, s.claimKey(jobID), claimToken)
				if err == nil && ok {
					ok, err = s.renewTokenLease(renewCtx, s.reviewExecutionKey(record), reviewToken)
				}
				if err == nil && ok {
					ok, err = s.renewConcurrencyLease(renewCtx, record, limitToken)
				}
				renewCancel()
				if err != nil || !ok {
					logger.Warn("agent lease lost; cancelling execution", "job_id", jobID, "error", err)
					cancel()
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

const renewTokenLeaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`

func (s *Service) renewTokenLease(ctx context.Context, key, token string) (bool, error) {
	result, err := s.client.Eval(ctx, renewTokenLeaseScript, []string{key}, token, s.claimTTL().Milliseconds())
	if err != nil {
		return false, err
	}
	return anyInt64(result) == 1, nil
}

const renewConcurrencyLeaseScript = `
local current = redis.call("zscore", KEYS[1], ARGV[1])
if not current then return 0 end
local expires = tonumber(ARGV[2])
redis.call("zadd", KEYS[1], expires, ARGV[1])
redis.call("zadd", KEYS[2], expires, ARGV[1])
redis.call("pexpire", KEYS[1], ARGV[3])
redis.call("pexpire", KEYS[2], ARGV[3])
return 1`

func (s *Service) renewConcurrencyLease(ctx context.Context, record Record, token string) (bool, error) {
	ttl := s.claimTTL()
	result, err := s.client.Eval(ctx, renewConcurrencyLeaseScript,
		[]string{s.cfg.KeyPrefix + "agent:jobs:semaphore:global", s.scopeConcurrencyKey(record)},
		token, time.Now().Add(ttl).UnixMilli(), ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	return anyInt64(result) == 1, nil
}

// 修改最后一个 ExecutionAttempt
func (r *Record) finishLatestExecution(status, err string, finished time.Time) {
	if len(r.Executions) == 0 {
		return
	}
	idx := len(r.Executions) - 1
	r.Executions[idx].Status = status
	r.Executions[idx].Error = err
	r.Executions[idx].FinishedAt = &finished
}

func (s *Service) queueKey() string {
	return s.cfg.KeyPrefix + "agent:jobs:stream"
}

func (s *Service) jobKey(id string) string {
	return s.cfg.KeyPrefix + "agent:jobs:" + id
}

func (s *Service) sourceEventKey(id string) string {
	return s.cfg.KeyPrefix + "source-event:" + strings.TrimSpace(id)
}

func (s *Service) deadLetterKey() string {
	return s.cfg.KeyPrefix + "agent:jobs:dead-letter"
}

func (s *Service) claimKey(id string) string {
	return s.cfg.KeyPrefix + "agent:jobs:claim:" + id
}

func (s *Service) reviewExecutionKey(record Record) string {
	return fmt.Sprintf("%sagent:jobs:review:store:%d:review:%d", s.cfg.KeyPrefix, record.StoreID, record.ReviewID)
}

const createJobScript = `
if redis.call('EXISTS', KEYS[1]) == 1 then
  return {0, ARGV[3]}
end
local use_source = ARGV[4] == '1'
if use_source then
  local existing = redis.call('GET', KEYS[2])
  if existing then
    if redis.call('EXISTS', ARGV[5] .. existing) == 1 then
      return {0, existing}
    end
    redis.call('DEL', KEYS[2])
  end
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
if use_source then
  redis.call('SET', KEYS[2], ARGV[3], 'PX', ARGV[2])
end
redis.call('XADD', KEYS[3], '*', 'job_id', ARGV[3])
return {1, ARGV[3]}
`

func (s *Service) createAndEnqueue(ctx context.Context, record Record) (bool, string, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return false, "", err
	}
	useSource := "0"
	sourceKey := s.sourceEventKey("_unused")
	if record.SourceEventID != "" {
		useSource = "1"
		sourceKey = s.sourceEventKey(record.SourceEventID)
	}
	result, err := s.client.Eval(ctx, createJobScript,
		[]string{s.jobKey(record.ID), sourceKey, s.queueKey()},
		string(raw), s.cfg.JobTTL.Milliseconds(), record.ID, useSource, s.cfg.KeyPrefix+"agent:jobs:")
	if err != nil {
		return false, "", err
	}
	items, ok := result.([]any)
	if !ok || len(items) < 2 {
		return false, "", fmt.Errorf("unexpected create job response: %v", result)
	}
	created := anyInt64(items[0]) == 1
	existingID := fmt.Sprint(items[1])
	return created, existingID, nil
}

const saveJobScript = `
local raw = redis.call('GET', KEYS[1])
if not raw then return -2 end
local ok, current = pcall(cjson.decode, raw)
if not ok then return -3 end
local version = tonumber(current['version'] or 0)
if version ~= tonumber(ARGV[1]) then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
if ARGV[4] == '1' then
  local owner = redis.call('GET', KEYS[2])
  if not owner or owner == ARGV[5] then
    redis.call('SET', KEYS[2], ARGV[5], 'PX', ARGV[3])
  end
end
return 1
`

func (s *Service) save(ctx context.Context, record *Record) error {
	if record == nil {
		return errors.New("job record is nil")
	}
	expected := record.Version
	record.Version++
	raw, err := json.Marshal(record)
	if err != nil {
		record.Version = expected
		return err
	}
	useSource := "0"
	sourceKey := s.sourceEventKey("_unused")
	if record.SourceEventID != "" {
		useSource = "1"
		sourceKey = s.sourceEventKey(record.SourceEventID)
	}
	result, err := s.client.Eval(ctx, saveJobScript,
		[]string{s.jobKey(record.ID), sourceKey},
		expected, string(raw), s.cfg.JobTTL.Milliseconds(), useSource, record.ID)
	if err != nil {
		record.Version = expected
		return err
	}
	if anyInt64(result) != 1 {
		record.Version = expected
		return fmt.Errorf("job record compare-and-set failed: job_id=%s result=%v", record.ID, result)
	}
	return nil
}

const releaseTokenScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`

func (s *Service) releaseToken(key, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := s.client.Eval(ctx, releaseTokenScript, []string{key}, token); err != nil {
		s.logger.Warn("release job claim failed", "key", key, "error", err)
	}
}

func (s *Service) claimTTL() time.Duration {
	return s.cfg.RunTimeout + 30*time.Second
}

const acquireConcurrencyScript = `
local now = tonumber(ARGV[2])
local expires = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[4]) then return 0 end
if redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[5]) then return 0 end
redis.call('ZADD', KEYS[1], expires, ARGV[1])
redis.call('ZADD', KEYS[2], expires, ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[6])
redis.call('PEXPIRE', KEYS[2], ARGV[6])
return 1
`

func (s *Service) acquireConcurrency(ctx context.Context, record Record, token string) (bool, error) {
	now := time.Now()
	ttl := s.claimTTL()
	result, err := s.client.Eval(ctx, acquireConcurrencyScript,
		[]string{s.cfg.KeyPrefix + "agent:jobs:semaphore:global", s.scopeConcurrencyKey(record)},
		token, now.UnixMilli(), now.Add(ttl).UnixMilli(), s.cfg.GlobalConcurrency, s.cfg.ScopeConcurrency, ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	return anyInt64(result) == 1, nil
}

const releaseConcurrencyScript = `
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`

func (s *Service) releaseConcurrency(record Record, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.client.Eval(ctx, releaseConcurrencyScript,
		[]string{s.cfg.KeyPrefix + "agent:jobs:semaphore:global", s.scopeConcurrencyKey(record)}, token)
	if err != nil {
		s.logger.Warn("release job concurrency slot failed", "job_id", record.ID, "error", err)
	}
}

func (s *Service) scopeConcurrencyKey(record Record) string {
	scope := firstNonEmpty(record.ActivityID, record.CampaignID)
	if scope == "" {
		scope = firstNonEmpty(record.Scene, "daily_review_coupon")
	}
	scope = strings.NewReplacer(":", "-", " ", "-").Replace(scope)
	return fmt.Sprintf("%sagent:jobs:semaphore:scope:store:%d:%s", s.cfg.KeyPrefix, record.StoreID, scope)
}

func (s *Service) ackMessage(ctx context.Context, messageID string) {
	if err := s.client.XAck(ctx, s.queueKey(), s.group, messageID); err != nil {
		s.logger.Warn("ack processed agent job failed", "message_id", messageID, "error", err)
	}
}

func (s *Service) recoverProcessing(ctx context.Context, consumer string) {
	interval := s.cfg.QueueWait
	if interval < time.Second {
		interval = time.Second
	}
	start := "0-0"
	start = s.recoverProcessingOnce(ctx, consumer, start)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start = s.recoverProcessingOnce(ctx, consumer, start)
		}
	}
}

func (s *Service) recoverProcessingOnce(ctx context.Context, consumer, start string) string {
	next, messages, err := s.client.XAutoClaim(ctx, s.queueKey(), s.group, consumer, s.cfg.RetryBackoff, start, int64(s.cfg.MaxBatchSize))
	if err != nil {
		s.logger.Warn("claim pending agent jobs failed", "error", err)
		return start
	}
	now := time.Now()
	for _, message := range messages {
		if strings.TrimSpace(message.JobID) == "" {
			s.ackMessage(ctx, message.ID)
			continue
		}
		record, ok, getErr := s.Get(ctx, message.JobID)
		if getErr != nil {
			s.logger.Warn("recover agent job read failed", "job_id", message.JobID, "error", getErr)
			continue
		}
		if !ok || isTerminal(record.Status) {
			s.ackMessage(ctx, message.ID)
			continue
		}
		if record.Status == StatusRunning && now.Before(record.UpdatedAt.Add(s.claimTTL())) {
			continue
		}
		s.process(ctx, message, s.logger.With("worker_id", "recovery"))
	}
	return next
}

func isTerminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusIncomplete, StatusFailed:
		return true
	default:
		return false
	}
}

func anyInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var out int64
		_, _ = fmt.Sscan(v, &out)
		return out
	default:
		return 0
	}
}

func buildSessionID(req Request) string {
	window := strings.TrimSpace(req.Window)
	if window == "" {
		window = time.Now().Format("20060102")
	}
	return agent.BuildScopedSessionID(agent.SessionScope{
		StoreID:    req.StoreID,
		CampaignID: firstNonEmpty(req.ActivityID, req.CampaignID),
		Scene:      req.Scene,
		WindowID:   window,
	})
}

// 作为 job 元数据
func normalizeTriggerType(value string) string {
	switch strings.TrimSpace(value) {
	// 预热、实时审核、定时任务批处理、手动触发
	case "campaign_warmup", "realtime_review", "daily_batch", "manual":
		return strings.TrimSpace(value)
	default:
		return "manual"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// 随机ID
func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
