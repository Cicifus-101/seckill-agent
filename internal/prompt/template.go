package prompt

// prompt程序化管理器

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"seckill-agent/internal/contextx"
	"seckill-agent/internal/tool"
)

const defaultPromptDir = "prompts/coupon_agent"

type Manager struct {
	baseDir string
	cache   map[string]string // 缓存已经读取的模板内容
}

func NewManager() *Manager {
	return &Manager{
		baseDir: defaultPromptDir,
		cache:   make(map[string]string),
	}
}

func NewManagerWithDir(baseDir string) *Manager {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = defaultPromptDir
	}
	return &Manager{
		baseDir: baseDir,
		cache:   make(map[string]string),
	}
}

func (m *Manager) BuildSystemPrompt(tools []tool.Info) string {
	var b strings.Builder
	b.WriteString(m.loadTemplate("system_v1.md", fallbackSystemPrompt))
	b.WriteString("\n\n## RAG Guidance\n")
	b.WriteString(m.loadTemplate("rag_guidance_v1.md", fallbackRAGGuidance))
	b.WriteString("\n\n## Policy Rules\n") //业务策略和风控规则
	b.WriteString(m.loadTemplate("policy_rules_v1.yaml", fallbackPolicyRules))
	b.WriteString("\n\n## Decision Schema\n")
	b.WriteString(m.loadTemplate("decision_schema_v1.json", fallbackDecisionSchema))
	b.WriteString("\n\n## Tools\n")

	for _, t := range tools {
		b.WriteString(fmt.Sprintf("- name: %s\n", t.Name))
		b.WriteString(fmt.Sprintf("  description: %s\n", t.Description))
		b.WriteString(fmt.Sprintf("  input_schema: %s\n", t.InputSchema))
	}

	return b.String()
}

// 每轮动态生成
func (m *Manager) BuildUserPrompt(task string, stage string, requiredNextTool string, allowFinal bool, summary string, history []contextx.StepRecord) string {
	payload := struct {
		Task          string                `json:"task"`
		WorkflowStage string                `json:"workflow_stage"`
		RequiredTool  string                `json:"required_next_tool,omitempty"`
		AllowFinal    bool                  `json:"allow_final"`
		Guidance      string                `json:"guidance"`
		Summary       string                `json:"summary,omitempty"`
		History       []contextx.StepRecord `json:"history"`
	}{
		Task:          task,
		WorkflowStage: stage,
		RequiredTool:  requiredNextTool,
		AllowFinal:    allowFinal,
		Guidance:      stageGuidance(stage),
		Summary:       summary,
		History:       history,
	}

	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func (m *Manager) loadTemplate(name, fallback string) string {
	if m == nil {
		return fallback
	}
	if value, ok := m.cache[name]; ok {
		return value
	}
	path := filepath.Join(m.baseDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		m.cache[name] = fallback
		return fallback
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		value = fallback
	}
	m.cache[name] = value
	return value
}

func stageGuidance(stage string) string {
	switch stage {
	case "collect_review_evidence":
		return "当前只允许获取评价事实证据，确认review_id、评分、内容、回复/申诉状态。不得直接final。"
	case "collect_es_statistics":
		return "当前已获得评价事实，下一步获取ES结构化统计，用于判断店铺/用户风险、近7/30天差评和政策标签。不得直接final。"
	case "collect_rag_history":
		return "当前已获得评价事实和ES统计。若评价涉及发券、拦截、人工复核或需要历史参考，调用retrieve_rag。aspects必须包含SUMMARY和DECISION，并按场景追加对应aspect；top_k建议为10。不得直接final。"
	case "decision_ready":
		return "当前信息已足够，结合事实、统计和历史案例输出最终秒杀优惠券决策。只输出final JSON，不要继续调用工具。"
	default:
		return "按渐进式披露顺序调用工具，避免一次性暴露过多上下文。"
	}
}

const fallbackSystemPrompt = `你是秒杀优惠券Agent大脑。
你的职责是基于评价事实、ES统计和历史案例，生成是否发券、为什么发券的结构化决策。
必须遵循渐进式披露：get_review_evidence -> get_review_stats -> retrieve_rag -> final。
当workflow_stage不是decision_ready时，禁止输出final，必须输出当前阶段所需tool_call。
当workflow_stage是decision_ready时，禁止继续调用tool_call，必须直接输出final。
只有活动上下文确认评价所属店铺、对应商品、活动和用户均满足资格时，才可建议GRANT_COUPON。
同一评价对同一店铺和具体商品最多实际发券一次；活动、场景和策略版本只能用于选择和审计，不能产生新的发券资格。
请严格输出JSON，不要输出多余解释。`

const fallbackRAGGuidance = `调用retrieve_rag时，chunk_types固定覆盖review_summary、review_aspect、coupon_decision；aspects包含SUMMARY和DECISION，再按场景追加LOGISTICS、SERVICE、QUALITY、PRICE、REPEAT、RECOMMEND。REPEAT表示复购意图，REPURCHASE仅作为兼容别名。RAG用于历史参考，不得机械照搬。`

const fallbackPolicyRules = `rules:
  - has_appeal=true时不得自动发券
  - 质量问题且score<=2时不得自动发券
  - MANUAL_REVIEW不得标记为已发券
  - 自动发券必须确认店铺、商品、活动和用户资格，且同一评价对同一店铺和具体商品只能实际发券一次
  - ES risk_level代表统计风险，不等于单条评价严重程度`

const fallbackDecisionSchema = `{"type":"final","final_answer":"...","action":"GRANT_COUPON|NO_COUPON|MANUAL_REVIEW","scene":"...","risk_level":"low|medium|high","reason":"...","reasoning":"...","confidence":0.85,"coupon_suggestion":"...","policy_tags":["..."],"need_human_review":false}`
