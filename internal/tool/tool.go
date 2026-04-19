package tool

import (
	"context"
	"encoding/json"
)

type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema string `json:"input_schema"`
}

type Tool interface {
	Info() Info
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}
