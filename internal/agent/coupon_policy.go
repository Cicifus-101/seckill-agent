package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type CouponPolicy struct {
	Version string              `json:"version"`
	Scenes  []CouponScenePolicy `json:"scenes"` // 业务场景
}

// 一个业务场景的发券策略
type CouponScenePolicy struct {
	Name            string           `json:"name"`          //Agent/业务侧场景：PHOTO_REVIEW_REWARD、SERVICE_RECOVERY
	SeckillScene    string           `json:"seckill_scene"` //秒杀/发券服务识别的场景  PHOTO_REVIEW 、AFTER_SALE_COMPENSATION、GOOD_REVIEW
	Description     string           `json:"description"`
	Priority        int              `json:"priority"`         //多场景同时命中时优先级
	Conditions      CouponConditions `json:"conditions"`       // 场景使用条件
	Decision        CouponDecision   `json:"decision"`         // 场景期望决策结果
	CouponTemplates []CouponTemplate `json:"coupon_templates"` //具体模板
}

type CouponConditions struct {
	Score         []int    `json:"score,omitempty"`
	ServiceScore  []int    `json:"service_score,omitempty"`
	ExpressScore  []int    `json:"express_score,omitempty"`
	HasMedia      *bool    `json:"has_media,omitempty"`
	IsFirstReview *bool    `json:"is_first_review,omitempty"`
	Keywords      []string `json:"keywords,omitempty"` // 关键词
}

// 描述这个策略期望的决策结果
type CouponDecision struct {
	Action        string  `json:"action"` // 这个场景通常应该发券
	RiskLevel     string  `json:"risk_level"`
	ConfidenceMin float64 `json:"confidence_min"` // 模型置信度
}

// 具体券模板
type CouponTemplate struct {
	Type          string  `json:"type"`
	Rule          string  `json:"rule"`
	AmountCent    int64   `json:"amount_cent,omitempty"`    //满减金额
	ThresholdCent int64   `json:"threshold_cent,omitempty"` //使用门槛
	Discount      float64 `json:"discount,omitempty"`       //折扣
	ValidDays     int     `json:"valid_days,omitempty"`     //有效期
}

// 策略提供者
type CouponPolicyProvider struct {
	mu          sync.RWMutex
	path        string
	hotReload   bool          // 是否热加载（补修改代码的情况下更新券金额、使用门槛、有效期、场景映射）
	reloadEvery time.Duration // 热加载检查间隔
	lastLoad    time.Time     // 上一次检查/加载策略文件的时间
	lastMod     time.Time     // 上一次成功加载时，策略文件的修改时间
	policy      CouponPolicy  // 内存中已经加载好的优惠券策略
}

func NewCouponPolicyProvider(path string, hotReload bool, reloadEvery time.Duration) *CouponPolicyProvider {
	p := &CouponPolicyProvider{
		path:        strings.TrimSpace(path),
		hotReload:   hotReload,
		reloadEvery: reloadEvery,
		policy:      defaultCouponPolicy(),
	}
	if p.reloadEvery <= 0 {
		p.reloadEvery = 30 * time.Second
	}
	_ = p.reloadIfNeeded(true)
	return p
}

// 根据LLM决策里的scene找对应的策略
func (p *CouponPolicyProvider) Suggestion(decision Decision) string {
	policy := p.current()
	item := findPolicy(policy, decision.Scene)
	if item.Name == "" {
		item = findPolicy(policy, seckillSceneByBusinessScene(decision.Scene))
	}
	if item.Name == "" {
		return ""
	}
	return renderCouponSuggestion(policy.Version, item)
}

// SeckillScene 将Agent的业务scene转成发券服务需要的scene：SERVICE_RECOVERY -> AFTER_SALE_COMPENSATION
func (p *CouponPolicyProvider) SeckillScene(scene string) string {
	policy := p.current()
	item := findPolicy(policy, scene)
	if item.Name != "" && strings.TrimSpace(item.SeckillScene) != "" {
		return strings.ToUpper(strings.TrimSpace(item.SeckillScene))
	}
	return seckillSceneByBusinessScene(scene)
}

// 获取当前生效策略
func (p *CouponPolicyProvider) current() CouponPolicy {
	if p == nil {
		return defaultCouponPolicy()
	}
	_ = p.reloadIfNeeded(false)
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.policy
}

// reloadIfNeeded 判断是否要重新加载策略文件
func (p *CouponPolicyProvider) reloadIfNeeded(force bool) error {
	if p == nil || p.path == "" {
		return nil
	}
	if !force && !p.hotReload {
		return nil
	}
	if !force && time.Since(p.lastLoad) < p.reloadEvery {
		return nil
	}
	info, err := os.Stat(p.path)
	if err != nil {
		return err
	}
	if !force && !info.ModTime().After(p.lastMod) { // 文件未修改
		p.lastLoad = time.Now()
		return nil
	}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return err
	}
	var policy CouponPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return err
	}
	if policy.Version == "" || len(policy.Scenes) == 0 {
		return fmt.Errorf("coupon policy missing version or scenes")
	}
	p.mu.Lock()
	p.policy = policy
	p.lastLoad = time.Now()
	p.lastMod = info.ModTime()
	p.mu.Unlock()
	return nil
}

// 根据Decision生成优惠券建议，避免provider为空时panic
func couponSuggestionForDecisionWithPolicy(decision Decision, provider *CouponPolicyProvider) string {
	if provider == nil {
		provider = NewCouponPolicyProvider("", false, 0)
	}
	return provider.Suggestion(decision)
}

func couponPolicyVersion(provider *CouponPolicyProvider) string {
	if provider == nil {
		return "policy-v1"
	}
	version := strings.TrimSpace(provider.current().Version)
	if version == "" {
		return "policy-v1"
	}
	return version
}

// renderCouponSuggestion 把策略里的券模板渲染成中文建议
func renderCouponSuggestion(version string, policy CouponScenePolicy) string {
	templates := make([]string, 0, len(policy.CouponTemplates))
	for _, item := range policy.CouponTemplates {
		days := item.ValidDays
		if days <= 0 {
			days = 14
		}
		templates = append(templates, fmt.Sprintf("%s（%s，有效期%d天）", item.Type, item.Rule, days))
	}
	if len(templates) == 0 {
		templates = append(templates, "5元无门槛券（有效期7天）")
	}
	scene := policy.SeckillScene //秒杀券场景
	if scene == "" {
		scene = policy.Name //业务场景
	}
	return fmt.Sprintf("建议发放%s；业务场景=%s，秒杀券场景=%s，策略版本=%s。", strings.Join(templates, " 或 "), policy.Name, scene, version)
}

// findPolicy 按scene找策略
func findPolicy(policy CouponPolicy, scene string) CouponScenePolicy {
	scene = strings.ToUpper(strings.TrimSpace(scene))
	for _, item := range policy.Scenes {
		if strings.EqualFold(item.Name, scene) || strings.EqualFold(item.SeckillScene, scene) {
			return item
		}
	}
	return CouponScenePolicy{}
}

// seckillSceneByBusinessScene 业务 scene 到秒杀 scene 的兜底映射
func seckillSceneByBusinessScene(scene string) string {
	scene = strings.ToUpper(strings.TrimSpace(scene))
	switch scene {
	case "FIRST_REVIEW":
		return "FIRST_REVIEW"
	case "PHOTO_REVIEW_REWARD", "PHOTO_REVIEW", "MEDIA_REWARD":
		return "PHOTO_REVIEW"
	case "INVITE_NEW", "RECOMMEND_INCENTIVE":
		return "INVITE_NEW"
	case "AFTER_SALE_COMPENSATION", "SERVICE_RECOVERY", "LOGISTICS_COMPENSATION":
		return "AFTER_SALE_COMPENSATION"
	default:
		return ""
	}
}

// 默认策略，提高基础策略
func defaultCouponPolicy() CouponPolicy {
	return CouponPolicy{
		Version: "fallback-v1",
		Scenes: []CouponScenePolicy{
			{Name: "FIRST_REVIEW", SeckillScene: "FIRST_REVIEW", CouponTemplates: []CouponTemplate{{Type: "首评奖励券", Rule: "6元首评新人券", AmountCent: 600, ValidDays: 7}}},
			{Name: "GOOD_REVIEW_REWARD", SeckillScene: "GOOD_REVIEW", CouponTemplates: []CouponTemplate{{Type: "好评返券", Rule: "满2000减300", AmountCent: 30000, ThresholdCent: 200000, ValidDays: 14}}},
			{Name: "PHOTO_REVIEW_REWARD", SeckillScene: "PHOTO_REVIEW", CouponTemplates: []CouponTemplate{{Type: "晒图返券", Rule: "满2000减300", AmountCent: 30000, ThresholdCent: 200000, ValidDays: 14}}},
			{Name: "AFTER_SALE_COMPENSATION", SeckillScene: "AFTER_SALE_COMPENSATION", CouponTemplates: []CouponTemplate{{Type: "售后补偿券", Rule: "75折补偿券", Discount: 0.75, ValidDays: 7}}},
			{Name: "INVITE_NEW", SeckillScene: "INVITE_NEW", CouponTemplates: []CouponTemplate{{Type: "拉新券", Rule: "满100减10", AmountCent: 1000, ThresholdCent: 10000, ValidDays: 14}}},
			{Name: "REPURCHASE_INCENTIVE", SeckillScene: "GOOD_REVIEW", CouponTemplates: []CouponTemplate{{Type: "复购券", Rule: "满50减10", AmountCent: 1000, ThresholdCent: 5000, ValidDays: 14}}},
			{Name: "PRICE_SENSITIVE_INCENTIVE", SeckillScene: "GOOD_REVIEW", CouponTemplates: []CouponTemplate{{Type: "价格敏感券", Rule: "满50减10", AmountCent: 1000, ThresholdCent: 5000, ValidDays: 14}}},
			{Name: "SERVICE_RECOVERY", SeckillScene: "AFTER_SALE_COMPENSATION", CouponTemplates: []CouponTemplate{{Type: "服务挽回券", Rule: "75折服务补偿券", Discount: 0.75, ValidDays: 7}}},
		},
	}
}
