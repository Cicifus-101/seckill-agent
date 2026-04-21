package prompt

import (
	"encoding/json"
	"fmt"
	"seckill-agent/internal/contextx"
	"seckill-agent/internal/tool"
	"strings"
)

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) BuildSystemPrompt(tools []tool.Info) string {
	var b strings.Builder
	b.WriteString("你是秒杀项目的运营Agent大脑。\n")
	b.WriteString("你的职责是根据用户任务，决定是否调用工具。\n")
	b.WriteString("请严格输出JSON，不要输出多余解释。\n")
	b.WriteString("输出格式如下：\n")
	b.WriteString(`{"type":"tool_call","tool_name":"xxx","arguments":{...}}` + "\n")
	b.WriteString(`{"type":"final","final_answer":"..."} ` + "\n")
	b.WriteString("如果已经有足够信息，直接输出final。\n")
	b.WriteString("可用工具如下：\n")

	for _, t := range tools {
		b.WriteString(fmt.Sprintf("- name: %s\n", t.Name))
		b.WriteString(fmt.Sprintf("  description: %s\n", t.Description))
		b.WriteString(fmt.Sprintf("  input_schema: %s\n", t.InputSchema))
	}

	return b.String()
}

func (m *Manager) BuildUserPrompt(task string, history []contextx.StepRecord) string {
	payload := struct {
		Task    string                `json:"task"`
		History []contextx.StepRecord `json:"history"`
	}{
		Task:    task,
		History: history,
	}

	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}
