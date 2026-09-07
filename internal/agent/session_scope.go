package agent

import (
	"fmt"
	"strings"
)

type SessionScope struct {
	StoreID    int64
	CampaignID string
	Scene      string
	WindowID   string
}

func BuildScopedSessionID(scope SessionScope) string {
	window := sanitizeSessionPart(scope.WindowID)
	// 没有店铺ID，告诉job层退回到独立job session
	if scope.StoreID <= 0 {
		return "job" //job提交接口要求要store_id，因此无店铺场景通常不可达
	}

	store := fmt.Sprintf("store:%d", scope.StoreID)
	campaignID := sanitizeOptionalSessionPart(scope.CampaignID)
	if campaignID != "" {
		return strings.Join([]string{store, "campaign", campaignID, "window", window}, ":")
	}

	scene := sanitizeOptionalSessionPart(scope.Scene)
	if scene == "" {
		scene = "daily_review_coupon"
	}
	return strings.Join([]string{store, "scene", scene, "window", window}, ":")
}

func sanitizeSessionPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, ":", "-")
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "default"
	}
	return value
}

func sanitizeOptionalSessionPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return sanitizeSessionPart(value)
}
