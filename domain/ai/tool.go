package ai

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/tools"
)

type ToolExecutionMode string

const (
	ToolExecutionModeAuto ToolExecutionMode = "auto"
	ToolExecutionModeNone ToolExecutionMode = "none"
)

const DefaultMaxToolIterations = 3

type ToolCall struct {
	ID         string
	Name       string
	Arguments  map[string]interface{}
	Result     *tools.ExecutionResult
	DurationMs int64
}

func EnsureToolCallIDs(calls []ToolCall) {
	for i := range calls {
		if strings.TrimSpace(calls[i].ID) == "" {
			calls[i].ID = "call_" + uuid.NewString()
		}
	}
}
