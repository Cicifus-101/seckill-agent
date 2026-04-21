package contextx

type StepRecord struct {
	Step      int    `json:"step"`
	ToolName  string `json:"tool_name,omitempty"`
	Action    string `json:"action"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
}
