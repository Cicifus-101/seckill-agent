package agent

import "encoding/json"

type Decision struct {
	Type     string `json:"type"`
	ToolName string `json:"tool_name,omitempty"`
	// JSON格式不固定，这个字段只是存起来，不进行解析，等知道tool_name字段之后再进行解析
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	FinalAnswer string          `json:"final_answer,omitempty"`
}

type Step struct {
	Step        int    `json:"step"`
	ToolName    string `json:"tool_name,omitempty"`
	Arguments   string `json:"arguments,omitempty"`
	Observation string `json:"observation,omitempty"`
	Error       string `json:"error,omitempty"`
}

type Result struct {
	SessionID   string `json:"session_id"`
	Task        string `json:"task"`
	FinalAnswer string `json:"final_answer"`
	Steps       []Step `json:"steps"`
	Summary     string `json:"summary,omitempty"`
}
