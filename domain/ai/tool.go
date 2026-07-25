package ai

import "vozko/domain/tools"

type ToolExecutionMode string

const (
	ToolExecutionModeAuto ToolExecutionMode = "auto"
	ToolExecutionModeNone ToolExecutionMode = "none"
)

const DefaultMaxToolIterations = 3

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
	Result    *tools.ExecutionResult
}
