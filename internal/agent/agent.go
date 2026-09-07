package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"seckill-agent/internal/contextx"
	"seckill-agent/internal/llm"
	"seckill-agent/internal/memory"
	"seckill-agent/internal/prompt"
	"seckill-agent/internal/tool"
)

type Agent struct {
	llmClient   llm.Client
	prompts     *prompt.Manager
	registry    *tool.Registry
	memoryStore memory.Store
	finalizer   DecisionFinalizer
	budget      contextx.Budget
	maxSteps    int
	llmTimeout  time.Duration
	toolTimeout time.Duration
}

type Option func(*Agent)

func WithMemoryStore(store memory.Store, budget contextx.Budget) Option {
	return func(a *Agent) {
		a.memoryStore = store
		if budget == (contextx.Budget{}) {
			budget = contextx.DefaultBudget()
		}
		a.budget = budget
	}
}

func WithTimeouts(llmTimeout, toolTimeout time.Duration) Option {
	return func(a *Agent) {
		if llmTimeout > 0 {
			a.llmTimeout = llmTimeout
		}
		if toolTimeout > 0 {
			a.toolTimeout = toolTimeout
		}
	}
}

func WithDecisionFinalizer(finalizer DecisionFinalizer) Option {
	return func(a *Agent) { a.finalizer = finalizer }
}

func New(llmClient llm.Client, prompts *prompt.Manager, registry *tool.Registry, maxSteps int, opts ...Option) *Agent {
	a := &Agent{
		llmClient:   llmClient,
		prompts:     prompts,
		registry:    registry,
		budget:      contextx.DefaultBudget(),
		maxSteps:    maxSteps,
		llmTimeout:  20 * time.Second,
		toolTimeout: 10 * time.Second,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	if a.memoryStore == nil {
		a.memoryStore = memory.NewInMemoryStore(memory.Config{MaxSessions: 1000, MaxRecentSteps: 8, MaxSummaryChars: 1200, SessionTTLSeconds: 1800})
	}
	if a.prompts == nil {
		a.prompts = prompt.NewManager()
	}
	if a.registry == nil {
		a.registry = tool.NewRegistry()
	}
	if a.maxSteps <= 0 {
		a.maxSteps = 5
	}
	return a
}

func (a *Agent) RunExecution(ctx context.Context, input ExecutionInput) (Result, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return Result{}, fmt.Errorf("session_id is required")
	}
	executionID := strings.TrimSpace(input.ExecutionID)
	if executionID == "" {
		return Result{}, fmt.Errorf("execution_id is required")
	}
	scopeID := executionScopeID(sessionID, executionID) // 生成 memory 的作用域
	task := strings.TrimSpace(input.Task)
	if task == "" {
		return Result{}, fmt.Errorf("task is required")
	}
	a.bootstrapExecution(ctx, scopeID, sessionID, executionID, task) // 初始化 memory
	if err := a.memoryError(); err != nil {
		return Result{}, fmt.Errorf("memory unavailable: %w", err)
	}
	steps := make([]Step, 0, a.maxSteps) // 本次调用最终返回给HTTP/Job的执行结果

	for stepNo := 1; stepNo <= a.maxSteps; stepNo++ {
		if err := a.memoryError(); err != nil {
			return Result{}, fmt.Errorf("memory unavailable: %w", err)
		}
		// 从 memory 里取当前 execution 已经执行过的步骤（memory 原始 RecentSteps）
		stage := a.workflowStage(a.workflowHistory(ctx, scopeID))
		// 为LLM构建构造“可放进prompt 的历史上下文”（压缩后的RecentSteps）
		promptContext := a.buildPromptContext(ctx, scopeID, task)
		// 判断调用工具，放进user prompt
		requiredTool := requiredToolForStage(stage)
		// 判断当前是否允许 LLM 输出最终决策
		allowFinal := stage == WorkflowStageDecisionReady

		// 调用LLM 将 当前阶段、必需工具、summary、历史记录 发给模型(分析上下文，选择工具或输出最终决策)
		resp, err := a.chat(ctx, task, stage, requiredTool, allowFinal, promptContext)
		if err != nil {
			return Result{}, err
		}
		// 解析决策
		decision, err := parseDecision(resp.Content)
		if err != nil {
			return Result{}, fmt.Errorf("parse llm decision failed: %w", err)
		}

		// Agent提出下一步决策，由后端检验是否真正能执行
		if strings.EqualFold(decision.Type, "final") {
			if !allowFinal {
				a.appendCorrectionRecord(ctx, scopeID, stepNo, stage, requiredTool, "final not allowed before decision_ready")
				continue
			}
			fullHistory := a.workflowHistory(ctx, scopeID) // 最终决策需要完整的工具结果，不是经过budget截断之后的历史
			decisionStep := (*Step)(nil)
			if a.finalizer != nil {
				// 获取分布式锁、保存决策，判断是否可以发券，使用评价幂等键防止重复发券
				finalized := a.finalizer.Finalize(ctx, FinalizeInput{
					SessionID:         sessionID,
					ExecutionID:       executionID,
					ParentExecutionID: strings.TrimSpace(input.ParentExecutionID),
					Task:              task,
					Decision:          decision,
					BusinessContext:   input,
					History:           fullHistory,
				})
				decision = finalized.Decision
				decisionStep = finalized.Step
			}
			// 最终决策写入memory，便于审计
			snapshotBytes, _ := json.Marshal(decision)
			a.appendStep(ctx, scopeID, contextx.StepRecord{Step: stepNo,
				ToolName: "llm", Action: "final_decision",
				Arguments: string(stage),
				Output:    string(snapshotBytes)})
			if decisionStep != nil { // 产生决策落库或者发券步骤
				steps = append(steps, *decisionStep) // job的返回结果
				// 将执行记录也写入memory
				a.appendStep(ctx, scopeID, contextx.StepRecord{
					Step:      decisionStep.Step,
					ToolName:  decisionStep.ToolName,
					Action:    "store_decision",
					Arguments: decisionStep.Arguments,
					Output:    decisionStep.Observation,
					Error:     decisionStep.Error,
				})
				if strings.TrimSpace(decisionStep.Error) != "" {
					return Result{}, fmt.Errorf("finalize decision failed: %s", decisionStep.Error)
				}
			}
			if err := a.memoryError(); err != nil {
				return Result{}, fmt.Errorf("memory unavailable: %w", err)
			}
			snapshot := a.snapshot(ctx, scopeID) // 获取summary
			return Result{SessionID: sessionID, ExecutionID: executionID,
				ParentExecutionID: strings.TrimSpace(input.ParentExecutionID),
				Task:              task, FinalAnswer: a.renderFinalAnswer(decision),
				Steps: steps, Summary: snapshot.Summary, Completed: true}, nil
		}
		if !strings.EqualFold(decision.Type, "tool_call") {
			a.appendCorrectionRecord(ctx, scopeID, stepNo,
				stage, requiredTool, "unsupported decision type: "+decision.Type)
			continue
		}
		if !a.toolAllowedForStage(stage, decision.ToolName) {
			a.appendCorrectionRecord(ctx, scopeID, stepNo, stage,
				requiredTool, fmt.Sprintf("tool %s not allowed in stage %s", decision.ToolName, stage))
			continue
		}
		step := a.executeTool(ctx, scopeID, stepNo, decision.ToolName, decision.Arguments) // 真正执行工具
		steps = append(steps, step)
		// evidence 后自动并发 stats + RAG
		if step.Error == "" && strings.EqualFold(decision.ToolName, "get_review_evidence") {
			steps = append(steps, a.runStatsRAGAndCampaignAfterEvidence(ctx, scopeID, step.Observation, input)...)
		}
	}
	if err := a.memoryError(); err != nil {
		return Result{}, fmt.Errorf("memory unavailable: %w", err)
	}
	snapshot := a.snapshot(ctx, scopeID) //把 memory 当前生成的 Summary 带回 Result
	return Result{SessionID: sessionID, ExecutionID: executionID,
		ParentExecutionID: strings.TrimSpace(input.ParentExecutionID), Task: task,
		FinalAnswer: "达到最大迭代次数，未收敛到最终答案", Steps: steps, Summary: snapshot.Summary, Completed: false}, nil
}

func (a *Agent) chat(ctx context.Context, task string, stage WorkflowStage, requiredTool string, allowFinal bool, promptContext contextx.CompressedContext) (llm.ChatResponse, error) {
	llmCtx, cancel := withTimeoutIfShorter(ctx, a.llmTimeout)
	defer cancel()
	return a.llmClient.Chat(llmCtx, llm.ChatRequest{
		SystemPrompt: a.prompts.BuildSystemPrompt(a.registry.List()),
		UserPrompt:   a.prompts.BuildUserPrompt(task, string(stage), requiredTool, allowFinal, promptContext.Summary, promptContext.RecentSteps),
	})
}

func (a *Agent) executeTool(ctx context.Context, sessionID string, stepNo int, toolName string, args json.RawMessage) Step {
	step := Step{Step: stepNo, ToolName: toolName, Arguments: string(args)}
	t, ok := a.registry.Get(toolName)
	if !ok {
		step.Error = "tool not found: " + toolName
		return step
	}
	start := time.Now()
	toolCtx, cancel := withTimeoutIfShorter(ctx, a.toolTimeout)
	observation, err := t.Execute(toolCtx, args)
	cancel()
	step.DurationMS = time.Since(start).Milliseconds()
	// 写入memory的轨迹记录
	record := contextx.StepRecord{Step: stepNo, ToolName: toolName, Action: "tool_call", Arguments: string(args)}
	if err != nil {
		step.Error = err.Error()
		record.Error = err.Error()
		a.appendStep(ctx, sessionID, record)
		return step
	}
	step.Observation = observation
	record.Output = observation
	a.appendStep(ctx, sessionID, record)
	return step
}

func (a *Agent) bootstrapExecution(ctx context.Context, scopeID, sessionID, executionID, task string) {
	// 初始化新的会话记录
	state := memory.SessionState{
		MemoryKey:   scopeID,
		SessionID:   sessionID,
		ExecutionID: executionID,
		Task:        task,
	}
	a.memoryStore.Save(ctx, state) // 将agent执行的初始化会话状态保存到memory中
}

func (a *Agent) appendStep(ctx context.Context, sessionID string, step contextx.StepRecord) {
	a.memoryStore.AppendStep(ctx, sessionID, step)
}

type memoryErrorReporter interface {
	LastError() error
}

func (a *Agent) memoryError() error {
	reporter, ok := a.memoryStore.(memoryErrorReporter)
	if !ok {
		return nil
	}
	return reporter.LastError()
}

// snapshot 从memory里读出当前execution的上下文视图：
// 1.构造 LLM 上下文时（每次调用LLM前）2.判断当前 workflow stage 时 3.finalizer 获取业务字段时 4.返回 Result 时 5.达到最大步数退出时
func (a *Agent) snapshot(ctx context.Context, sessionID string) memory.View {
	return a.memoryStore.Snapshot(ctx, sessionID)
}

// buildPromptContext
func (a *Agent) buildPromptContext(ctx context.Context, sessionID, task string) contextx.CompressedContext {
	view := a.memoryStore.Snapshot(ctx, sessionID)
	return contextx.Compose(view, task, a.budget)
}

// workflowHistory 获取工作流历史
func (a *Agent) workflowHistory(ctx context.Context, sessionID string) []contextx.StepRecord {
	return a.memoryStore.Snapshot(ctx, sessionID).RecentSteps
}

// appendCorrectionRecord 添加纠正记录
func (a *Agent) appendCorrectionRecord(ctx context.Context, sessionID string, step int, stage WorkflowStage, requiredTool string, reason string) {
	payload := map[string]any{"stage": stage, "required_tool": requiredTool, "reason": reason}
	raw, _ := json.Marshal(payload)
	a.appendStep(ctx, sessionID, contextx.StepRecord{Step: step, ToolName: "agent_guard", Action: "correction", Output: string(raw), Error: reason})
}

// workflowStage 判断工作流状态
func (a *Agent) workflowStage(history []contextx.StepRecord) WorkflowStage {
	hasEvidence, hasStats, hasRAG, hasCampaign := false, false, false, false
	for _, item := range history {
		if item.Action != "tool_call" || strings.TrimSpace(item.Error) != "" {
			continue
		}
		switch item.ToolName {
		case "get_review_evidence":
			hasEvidence = true
		case "get_review_stats":
			hasStats = true
		case "retrieve_rag":
			hasRAG = true
		case "get_campaign_context":
			hasCampaign = true
		}
	}
	switch {
	case !hasEvidence:
		return WorkflowStageCollectReviewEvidence
	case !hasStats:
		return WorkflowStageCollectESStatistics
	case !hasRAG:
		return WorkflowStageCollectRAGHistory
	case !hasCampaign:
		return WorkflowStageCollectCampaignContext
	default:
		return WorkflowStageDecisionReady
	}
}

// executionScopeID 生成作用域ID
func executionScopeID(sessionID, executionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return sessionID
	}
	return sessionID + ":execution:" + executionID
}

// parseDecision 解析决策JSON
func parseDecision(content string) (Decision, error) {
	content = strings.TrimSpace(content)
	var d Decision
	if err := json.Unmarshal([]byte(content), &d); err == nil {
		return d, nil
	}
	// 宽松解析
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(content[start:end+1]), &d); err == nil {
			return d, nil
		}
	}
	return Decision{}, fmt.Errorf("invalid decision json")
}

// requiredToolForStage 返回阶段必须调用的工具
func requiredToolForStage(stage WorkflowStage) string {
	switch stage {
	case WorkflowStageCollectReviewEvidence:
		return "get_review_evidence"
	case WorkflowStageCollectESStatistics:
		return "get_review_stats"
	case WorkflowStageCollectRAGHistory:
		return "retrieve_rag"
	case WorkflowStageCollectCampaignContext:
		return "get_campaign_context"
	default:
		return ""
	}
}

func (a *Agent) toolAllowedForStage(stage WorkflowStage, toolName string) bool {
	required := requiredToolForStage(stage)
	return required != "" && strings.EqualFold(required, toolName)
}

// renderFinalAnswer 生成最终调用方看的 FinalAnswer
func (a *Agent) renderFinalAnswer(decision Decision) string {
	if strings.TrimSpace(decision.FinalAnswer) != "" {
		return decision.FinalAnswer
	}
	parts := []string{}
	for _, item := range []string{decision.Action, decision.Scene, decision.RiskLevel, decision.CouponSuggestion, decision.Reason, decision.Reasoning} {
		if strings.TrimSpace(item) != "" {
			parts = append(parts, item)
		}
	}
	if len(parts) == 0 {
		return "LLM已完成决策，但未输出final_answer"
	}
	return strings.Join(parts, "；")
}

// 最终业务上下文：将最终业务字段集中到一个结构体中
type decisionContext struct {
	ReviewID            int64
	StoreID             int64 //店铺隔离 / 分布式锁 /决策存储/发券幂等范围
	UserID              int64 // 领取数量/调用秒杀服务发券/用户是否有资格
	OrderID             int64
	SkuID               int64
	SpuID               int64  //商品资格校验、发券请求和决策审计
	Score               int32  //决策落库
	Content             string //评价正文 决策落库和 RAG 记忆
	HasMedia            bool
	IsFirstReview       bool     //首评奖励判断
	EvidenceIDs         []string //审计证据链
	EvidenceVersion     int64    //证据更新后产生新的决策版本
	ActivityID          string
	PolicyVersion       string
	CouponTemplateID    string //匹配的优惠券模板
	CampaignActive      bool
	ProductEligible     bool  // 对应的商品是否属于活动商品范围
	UserEligible        bool  // 用户是否满足活动资格
	RemainingStock      int64 //剩余库存
	RemainingBudgetCent int64 //剩余预算（单位：分）
	UserGrantLimit      int32 //用户最多领取数量
	UserGrantedCount    int32 // 已经领取数量
}

// extractDecisionContext 提取决策上下文：将memory中保存的各个工具输出重新解析成一个统一的decision，供最终决策、落库和发券使用
func extractDecisionContext(history []contextx.StepRecord) decisionContext {
	var dc decisionContext
	seen := map[string]bool{} //防止用一个证据ID被重复加入
	for _, item := range history {
		if strings.TrimSpace(item.Output) == "" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(item.Output), &payload) != nil {
			continue
		}
		switch item.ToolName {
		case "get_review_evidence":
			if ev, ok := payload["evidence"].(map[string]any); ok {
				dc.ReviewID = firstInt64(dc.ReviewID, anyInt64(ev["reviewId"]), anyInt64(ev["review_id"]))
				dc.StoreID = firstInt64(dc.StoreID, anyInt64(ev["storeId"]), anyInt64(ev["store_id"]))
				dc.UserID = firstInt64(dc.UserID, anyInt64(ev["userId"]), anyInt64(ev["user_id"]))
				dc.OrderID = firstInt64(dc.OrderID, anyInt64(ev["orderId"]), anyInt64(ev["order_id"]))
				dc.SkuID = firstInt64(dc.SkuID, anyInt64(ev["skuId"]), anyInt64(ev["sku_id"]))
				dc.SpuID = firstInt64(dc.SpuID, anyInt64(ev["spuId"]), anyInt64(ev["spu_id"]))
				dc.Score = int32(anyInt64(ev["score"]))
				dc.Content = anyString(ev["content"])
				dc.HasMedia = anyBool(ev["hasMedia"]) || anyBool(ev["has_media"])
				dc.IsFirstReview = anyBool(ev["isFirstReview"]) || anyBool(ev["is_first_review"])
				dc.EvidenceVersion = firstInt64(dc.EvidenceVersion, anyInt64(ev["evidenceVersion"]), anyInt64(ev["evidence_version"]))
				id := fmt.Sprintf("review:%d", dc.ReviewID)
				if dc.ReviewID > 0 && !seen[id] {
					dc.EvidenceIDs = append(dc.EvidenceIDs, id)
					seen[id] = true
				}
			}
		case "retrieve_rag":
			collectRAGEvidenceIDs(payload, seen, &dc)
		case "get_campaign_context":
			if campaign, ok := payload["campaign"].(map[string]any); ok {
				dc.ActivityID = defaultIfBlank(dc.ActivityID, anyString(campaign["activity_id"]))
				dc.PolicyVersion = defaultIfBlank(dc.PolicyVersion, anyString(campaign["policy_version"]))
				dc.CouponTemplateID = defaultIfBlank(dc.CouponTemplateID, anyString(campaign["coupon_template_id"]))
				dc.CampaignActive = strings.EqualFold(anyString(campaign["status"]), "ACTIVE")
				dc.ProductEligible = anyBool(campaign["product_eligible"])
				dc.UserEligible = anyBool(campaign["user_eligible"])
				dc.RemainingStock = anyInt64(campaign["remaining_stock"])
				dc.RemainingBudgetCent = anyInt64(campaign["remaining_budget_cent"])
				dc.UserGrantLimit = int32(anyInt64(campaign["user_grant_limit"]))
				dc.UserGrantedCount = int32(anyInt64(campaign["user_granted_count"]))
			}
		}
	} // defaultIfBlank 适用于 ActivityID、PolicyVersion、CouponTemplateID
	return dc
}

// collectRAGEvidenceIDs 收集RAG证据ID
func collectRAGEvidenceIDs(payload map[string]any, seen map[string]bool, dc *decisionContext) {
	rag, _ := payload["rag"].(map[string]any)
	chunks, _ := rag["chunks"].([]any) // 获取历史片段
	for _, item := range chunks {
		chunk, _ := item.(map[string]any)
		id := anyString(chunk["chunk_id"])
		if id != "" && !seen[id] { // 去重
			dc.EvidenceIDs = append(dc.EvidenceIDs, id)
			seen[id] = true
		}
	}
}

// 落库时设置默认值，优先保留已有值
func defaultIfBlank(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}

func withTimeoutIfShorter(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

// 因为json.Unmarshal 到 map[string]any，通常变成float64
func anyInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func anyString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func anyBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return false
	}
}

func firstInt64(current int64, candidates ...int64) int64 {
	if current > 0 {
		return current
	}
	for _, candidate := range candidates {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}
