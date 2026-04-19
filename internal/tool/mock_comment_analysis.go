package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type MockCommentAnalysisTool struct{}

type CommentAnalysisInput struct {
	Days      int    `json:"days"`
	Sentiment string `json:"sentiment"`
}

func (t *MockCommentAnalysisTool) Info() Info {
	return Info{
		Name:        "analyze_comments",
		Description: "分析最近评论区反馈，返回符合条件的用户画像和候选列表。",
		InputSchema: `{"days":7,"sentiment":"positive"}`,
	}
}

func (t *MockCommentAnalysisTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var req CommentAnalysisInput
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid comment analysis input: %w", err)
	}
	if req.Days <= 0 {
		req.Days = 7
	}
	if req.Sentiment == "" {
		req.Sentiment = "positive"
	}

	resp := map[string]any{
		"days":      req.Days,
		"sentiment": req.Sentiment,
		"users": []map[string]any{
			{"user_id": 1001, "nickname": "alice", "score": 0.96},
			{"user_id": 1002, "nickname": "bob", "score": 0.92},
		},
	}
	data, _ := json.Marshal(resp)
	return string(data), nil
}
