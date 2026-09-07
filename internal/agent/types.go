package agent

import "encoding/json"

type WorkflowStage string

const (
	WorkflowStageCollectReviewEvidence  WorkflowStage = "collect_review_evidence"
	WorkflowStageCollectESStatistics    WorkflowStage = "collect_es_statistics"
	WorkflowStageCollectRAGHistory      WorkflowStage = "collect_rag_history"
	WorkflowStageCollectCampaignContext WorkflowStage = "collect_campaign_context"
	WorkflowStageDecisionReady          WorkflowStage = "decision_ready"
)

// LLM的输出
type Decision struct {
	Type             string          `json:"type"`
	ToolName         string          `json:"tool_name,omitempty"`
	Arguments        json.RawMessage `json:"arguments,omitempty"`
	FinalAnswer      string          `json:"final_answer,omitempty"`
	Action           string          `json:"action,omitempty"`
	Scene            string          `json:"scene,omitempty"`
	RiskLevel        string          `json:"risk_level,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"` // 用于审计调试
	Confidence       float64         `json:"confidence,omitempty"`
	CouponSuggestion string          `json:"coupon_suggestion,omitempty"`
	// 命中的决策标签
	PolicyTags      []string `json:"policy_tags,omitempty"`
	NeedHumanReview bool     `json:"need_human_review,omitempty"`
}

type Step struct {
	Step        int    `json:"step"`
	ToolName    string `json:"tool_name,omitempty"`
	Arguments   string `json:"arguments,omitempty"`
	Observation string `json:"observation,omitempty"`
	Error       string `json:"error,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
}

type ExecutionInput struct {
	SessionID         string
	ExecutionID       string
	ParentExecutionID string
	Task              string // 本次Agent完成任务
	ReviewID          int64
	EvidenceVersion   int64
	StoreID           int64
	UserID            int64
	SkuID             int64
	SpuID             int64
	ActivityID        string
	PolicyVersion     string
}

// Agent 执行结果
type Result struct {
	SessionID         string `json:"session_id"`
	ExecutionID       string `json:"execution_id,omitempty"`
	ParentExecutionID string `json:"parent_execution_id,omitempty"`
	Task              string `json:"task"` //原始任务
	FinalAnswer       string `json:"final_answer"`
	Steps             []Step `json:"steps"`
	Summary           string `json:"summary,omitempty"` //memory生成的摘要
	Completed         bool   `json:"completed"`
}
