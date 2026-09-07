package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"seckill-agent/internal/client/reviewjob"
	"seckill-agent/internal/client/seckill"
	"seckill-agent/internal/contextx"
)

type DecisionFinalizer interface {
	Finalize(ctx context.Context, input FinalizeInput) FinalizeResult
}

type FinalizeInput struct {
	SessionID         string
	ExecutionID       string
	ParentExecutionID string
	Task              string
	Decision          Decision
	History           []contextx.StepRecord
	BusinessContext   ExecutionInput // HTTP/Job传入的业务上下文
}

type FinalizeResult struct {
	Decision Decision //经过guard修正后的最终决策
	Step     *Step    //最终落库或发券步骤
}

type DecisionStore interface {
	StoreDecision(ctx context.Context, req reviewjob.StoreDecisionRequest) (*reviewjob.StoreDecisionResponse, error)
}

type DecisionLock interface {
	Acquire(ctx context.Context, key string) (token string, ok bool, err error)
	Release(ctx context.Context, key string, token string) error
	Renew(ctx context.Context, key string, token string) (bool, error)
}

// 实际发券
type CouponGrantService interface {
	GrantCouponByReview(ctx context.Context, req seckill.GrantCouponByReviewRequest) (*seckill.GrantCouponByReviewReply, error)
}

type ReviewCouponFinalizer struct {
	decisionStore DecisionStore
	decisionLock  DecisionLock
	couponGrant   CouponGrantService
	couponPolicy  *CouponPolicyProvider //场景和券模板策略
}

type ReviewCouponFinalizerOption func(*ReviewCouponFinalizer)

func NewReviewCouponFinalizer(opts ...ReviewCouponFinalizerOption) *ReviewCouponFinalizer {
	f := &ReviewCouponFinalizer{
		couponPolicy: NewCouponPolicyProvider("", false, 0),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(f)
		}
	}
	if f.couponPolicy == nil {
		f.couponPolicy = NewCouponPolicyProvider("", false, 0)
	}
	return f
}

func WithReviewCouponDecisionStore(store DecisionStore) ReviewCouponFinalizerOption {
	return func(f *ReviewCouponFinalizer) { f.decisionStore = store }
}

func WithReviewCouponDecisionLock(lock DecisionLock) ReviewCouponFinalizerOption {
	return func(f *ReviewCouponFinalizer) { f.decisionLock = lock }
}

func WithReviewCouponGrantService(service CouponGrantService) ReviewCouponFinalizerOption {
	return func(f *ReviewCouponFinalizer) { f.couponGrant = service }
}

func WithReviewCouponPolicy(provider *CouponPolicyProvider) ReviewCouponFinalizerOption {
	return func(f *ReviewCouponFinalizer) { f.couponPolicy = provider }
}

type couponGrantOutcome struct {
	Attempted       bool // 是否尝试发券
	Success         bool
	Duplicated      bool // 是否幂等重复
	Attempts        int  // 调用次数
	Retryable       bool
	CouponID        string
	Message         string //服务返回消息
	Error           string
	Scene           string //秒杀场景
	IdempotencyKey  string
	ExecutionStatus string //执行状态
}

func (f *ReviewCouponFinalizer) Finalize(ctx context.Context, input FinalizeInput) FinalizeResult {
	decision := applyDecisionGuard(input.Decision, input.History, f.couponPolicy)
	if f.decisionStore == nil {
		return FinalizeResult{Decision: decision}
	}
	step := Step{Step: len(input.History) + 1, ToolName: "store_decision"}
	dc := extractDecisionContext(input.History) // 提取业务上下文
	dc.ActivityID = defaultIfBlank(dc.ActivityID, input.BusinessContext.ActivityID)
	dc.PolicyVersion = defaultIfBlank(dc.PolicyVersion, input.BusinessContext.PolicyVersion)
	if dc.ReviewID <= 0 || dc.StoreID <= 0 { // 构建决策ID
		step.Error = "missing review_id or store_id for decision persistence"
		return FinalizeResult{Decision: decision, Step: &step}
	}

	lockKey := fmt.Sprintf("decision:store:%d:review:%d", dc.StoreID, dc.ReviewID)
	if f.decisionLock != nil {
		token, ok, err := f.decisionLock.Acquire(ctx, lockKey)
		if err != nil {
			step.Error = fmt.Sprintf("acquire decision lock failed: %v", err)
			return FinalizeResult{Decision: decision, Step: &step}
		}
		if !ok {
			step.Error = "decision is already being processed by another agent instance"
			return FinalizeResult{Decision: decision, Step: &step}
		}

		finalizeCtx, stopLease := startDecisionLockLease(ctx, f.decisionLock, lockKey, token)
		defer stopLease()
		ctx = finalizeCtx

		// 释放锁，context.Background()为了避免原始请求Context已经取消之后，释放锁也立即被取消
		defer func() { _ = f.decisionLock.Release(context.Background(), lockKey, token) }()
	}

	grant := f.grantCouponIfNeeded(ctx, decision, dc)   //判断并执行发券
	decision = decisionWithCouponGrant(decision, grant) //根据发券结果修正最终决策
	llmOutput := map[string]any{
		"type": decision.Type, "final_answer": decision.FinalAnswer, "action": decision.Action, "scene": decision.Scene,
		"risk_level": decision.RiskLevel, "reason": decision.Reason, "reasoning": decision.Reasoning, "confidence": decision.Confidence,
		"coupon_suggestion": decision.CouponSuggestion, "policy_tags": decision.PolicyTags, "need_human_review": decision.NeedHumanReview, "task": input.Task,
		"session_id": input.SessionID, "execution_id": input.ExecutionID, "parent_execution_id": input.ParentExecutionID,
		"coupon_grant": map[string]any{"attempted": grant.Attempted, "success": grant.Success, "duplicated": grant.Duplicated,
			"attempts": grant.Attempts, "retryable": grant.Retryable, "coupon_id": grant.CouponID, "message": grant.Message,
			"error": grant.Error, "scene": grant.Scene, "idempotency_key": grant.IdempotencyKey},
	}
	// 构造决策落库请求
	req := reviewjob.StoreDecisionRequest{
		DecisionID: buildDecisionID(dc), ReviewID: dc.ReviewID, StoreID: dc.StoreID,
		SkuID: dc.SkuID, SpuID: dc.SpuID, EvidenceVersion: dc.EvidenceVersion, DecisionRevision: dc.EvidenceVersion,
		UserID: dc.UserID, Score: dc.Score, Content: dc.Content, Scene: defaultIfBlank(decision.Scene, "review_coupon"),
		Action: defaultIfBlank(decision.Action, "NO_COUPON"), RiskLevel: defaultIfBlank(decision.RiskLevel, "unknown"),
		CouponGranted: grant.Success, CouponID: grant.CouponID, Reason: reasonWithCouponGrant(defaultIfBlank(decision.Reason, decision.Reasoning), grant),
		TraceID: defaultIfBlank(input.ExecutionID, input.SessionID), EvidenceIDs: dc.EvidenceIDs, LLMOutput: llmOutput,
		ActivityID: dc.ActivityID, PolicyVersion: dc.PolicyVersion, ExecutionStatus: grant.ExecutionStatus, ReasonCodes: decision.PolicyTags,
	}
	if req.DecisionRevision <= 0 {
		req.DecisionRevision = 1
	}
	reqBytes, _ := json.Marshal(req)
	step.Arguments = string(reqBytes)
	start := time.Now()
	resp, err := f.decisionStore.StoreDecision(ctx, req)
	step.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		step.Error = err.Error()
		return FinalizeResult{Decision: decision, Step: &step}
	}
	respBytes, _ := json.Marshal(resp)
	step.Observation = string(respBytes)
	return FinalizeResult{Decision: decision, Step: &step}
}

func startDecisionLockLease(parent context.Context, lock DecisionLock, key, token string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	interval := 10 * time.Second
	if provider, ok := lock.(interface{ TTL() time.Duration }); ok && provider.TTL() > 0 {
		interval = provider.TTL() / 3
		if interval < time.Second {
			interval = time.Second
		}
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
				ok, err := lock.Renew(renewCtx, key, token)
				renewCancel()
				if err != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() {
		close(done)
		cancel()
		<-finished
	}
}

// grantCouponIfNeeded 判断是否需要人工审核、活动状态、商品和用户资格、库存、预算和用户领取上线决定是否调用秒杀服务
func (f *ReviewCouponFinalizer) grantCouponIfNeeded(ctx context.Context, decision Decision, dc decisionContext) couponGrantOutcome {
	if !strings.EqualFold(decision.Action, "GRANT_COUPON") || decision.NeedHumanReview {
		status := "REJECTED"
		if decision.NeedHumanReview {
			status = "MANUAL_REVIEW"
		}
		return couponGrantOutcome{Message: "decision does not require coupon grant", ExecutionStatus: status}
	}
	if f.couponGrant == nil {
		return couponGrantOutcome{Message: "coupon grant service is not configured", ExecutionStatus: "PROPOSED"}
	}
	productID := firstInt64(0, dc.SkuID, dc.SpuID)
	if dc.ActivityID == "" || dc.StoreID <= 0 || dc.UserID <= 0 || productID <= 0 {
		return couponGrantOutcome{Message: "campaign context is missing activity, store, user, or product", ExecutionStatus: "REJECTED"}
	}
	// 检查实时活动约束
	if !dc.CampaignActive || !dc.ProductEligible || !dc.UserEligible || dc.RemainingStock <= 0 || dc.RemainingBudgetCent <= 0 || (dc.UserGrantLimit > 0 && dc.UserGrantedCount >= dc.UserGrantLimit) {
		return couponGrantOutcome{Message: "campaign constraints rejected coupon grant", ExecutionStatus: "REJECTED"}
	}
	scene := f.couponPolicy.SeckillScene(decision.Scene) // 场景映射
	if scene == "" {
		return couponGrantOutcome{Message: "unknown coupon scene", ExecutionStatus: "REJECTED"}
	}
	key := BuildReviewCouponGrantIdempotencyKey(dc.ReviewID, dc.StoreID) // 构建发券幂等键
	// 构建发券请求（携带业务资格字段、审计字段、策略字段、幂等字段）
	req := seckill.GrantCouponByReviewRequest{StoreID: dc.StoreID, UserID: dc.UserID, ReviewID: dc.ReviewID,
		OrderNo: orderNoFromID(dc.OrderID), ProductID: productID, Rating: dc.Score,
		HasImage: dc.HasMedia, IsFirstReview: dc.IsFirstReview, Scene: scene, ActivityID: dc.ActivityID,
		PolicyVersion: dc.PolicyVersion, EvidenceVersion: dc.EvidenceVersion, CouponTemplateID: dc.CouponTemplateID,
		IdempotencyKey: key}
	// 发券重试（context被取消，立即退出）
	resp, attempts, err := f.grantCouponWithRetry(ctx, req)
	if err != nil {
		return couponGrantOutcome{Attempted: true, Success: false, Attempts: attempts,
			Retryable: isRetryableCouponGrantError(err), Scene: scene,
			IdempotencyKey: key, Error: err.Error(), Message: "coupon grant failed; decision stored as not granted",
			ExecutionStatus: "GRANT_FAILED"}
	}
	status := "REJECTED"
	if resp.Success {
		status = "GRANTED"
	}
	return couponGrantOutcome{Attempted: true, Success: resp.Success, Duplicated: resp.Duplicated,
		Attempts: attempts, CouponID: couponIDFromResponse(resp.Coupon), Message: resp.Message,
		Scene: scene, IdempotencyKey: key, ExecutionStatus: status}
}

func (f *ReviewCouponFinalizer) grantCouponWithRetry(ctx context.Context, req seckill.GrantCouponByReviewRequest) (*seckill.GrantCouponByReviewReply, int, error) {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := f.couponGrant.GrantCouponByReview(ctx, req)
		if err == nil {
			return resp, attempt, nil
		}
		last = err
		if !isRetryableCouponGrantError(err) || attempt == 3 {
			return nil, attempt, err
		}
		timer := time.NewTimer(time.Duration(attempt*100) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, attempt, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, 3, last
}

// 判断是否是可以重试的类型
func isRetryableCouponGrantError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline", "connection refused", "connection reset", "temporary", "eof", "status 500", "status 502", "status 503", "status 504"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func decisionWithCouponGrant(decision Decision, grant couponGrantOutcome) Decision {
	if !strings.EqualFold(decision.Action, "GRANT_COUPON") {
		return decision
	}
	switch {
	case !grant.Attempted:
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "COUPON_GRANT_NOT_CONFIGURED")
		decision.FinalAnswer = appendSentence(decision.FinalAnswer, "当前仅生成发券建议，发券服务未配置，实际未发券。")
	case grant.Success:
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "COUPON_GRANTED")
		decision.FinalAnswer = appendSentence(decision.FinalAnswer, "优惠券已成功发放。")
	default:
		if grant.ExecutionStatus == "REJECTED" {
			decision.Action = "NO_COUPON"
			decision.CouponSuggestion = ""
			decision.PolicyTags = appendMissingTag(decision.PolicyTags, "CAMPAIGN_CONSTRAINT_REJECTED")
		}
		decision.PolicyTags = appendMissingTag(decision.PolicyTags, "COUPON_GRANT_FAILED")
		decision.FinalAnswer = appendSentence(decision.FinalAnswer, "发券调用失败，系统已按未发券状态落库，后续可依据幂等键重试补发。")
	}
	return decision
}

// 将发券状态写进最终llm执行结果的原因
func reasonWithCouponGrant(reason string, grant couponGrantOutcome) string {
	switch {
	case !grant.Attempted:
		return joinReason(reason, "发券未执行："+grant.Message)
	case grant.Success:
		return joinReason(reason, "发券成功："+grant.Message)
	default:
		msg := grant.Error
		if msg == "" {
			msg = grant.Message
		}
		return joinReason(reason, "发券失败，已按未发券落库："+msg)
	}
}

func buildDecisionID(dc decisionContext) string {
	return fmt.Sprintf("agent:store:%d:review:%d", dc.StoreID, dc.ReviewID)
}

func orderNoFromID(orderID int64) string {
	if orderID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", orderID)
}

func couponIDFromResponse(coupon map[string]any) string {
	for _, key := range []string{"coupon_id", "couponId", "id"} {
		if value := anyString(coupon[key]); value != "" {
			return value
		}
	}
	return ""
}

func appendSentence(base, sentence string) string {
	base, sentence = strings.TrimSpace(base), strings.TrimSpace(sentence)
	if base == "" {
		return sentence
	}
	if sentence == "" {
		return base
	}
	return base + sentence
}

func joinReason(base, extra string) string {
	base, extra = strings.TrimSpace(base), strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + "；" + extra
}
