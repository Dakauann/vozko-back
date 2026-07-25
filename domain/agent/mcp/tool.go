package mcp

type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema []byte
}

type ToolResult struct {
	Content []ToolContent
	IsError bool
}

type ToolContent struct {
	Type string
	Text string
	Data []byte
}

func TextResult(s string) ToolResult {
	return ToolResult{Content: []ToolContent{{Type: "text", Text: s}}}
}

func ErrorResult(s string) ToolResult {
	return ToolResult{Content: []ToolContent{{Type: "text", Text: s}}, IsError: true}
}
