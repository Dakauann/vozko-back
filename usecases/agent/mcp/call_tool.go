package mcp

import (
	"context"
	"time"

	domainmcp "vozko/domain/agent/mcp"
)

type AuditRecorder interface {
	Record(ctx context.Context, ev AuditEvent)
}

type AuditEvent struct {
	WorkspaceID string
	SourceID    string
	Tool        string
	LatencyMS   int64
	Status      string
	ErrorClass  string
}

type NoopAudit struct{}

func (NoopAudit) Record(context.Context, AuditEvent) {}

type RateLimiter interface {
	Allow(ctx context.Context, ws string) error
}

type CallToolInput struct {
	WorkspaceID string
	Name        string
	Args        map[string]any
}

type CallToolUseCase struct {
	Registry *Registry
	Audit    AuditRecorder
	Limiter  RateLimiter
	Clock    func() time.Time
}

func NewCallToolUseCase(reg *Registry) *CallToolUseCase {
	return &CallToolUseCase{Registry: reg, Audit: NoopAudit{}, Clock: time.Now}
}

func (u *CallToolUseCase) Execute(ctx context.Context, in CallToolInput) (domainmcp.ToolResult, error) {
	ws := domainmcp.WorkspaceID(in.WorkspaceID)
	src, tool, err := u.Registry.Resolve(ctx, ws, in.Name)
	if err != nil {
		return domainmcp.ToolResult{}, err
	}
	if u.Limiter != nil {
		if err := u.Limiter.Allow(ctx, in.WorkspaceID); err != nil {
			return domainmcp.ToolResult{}, err
		}
	}
	start := u.now()
	res, callErr := src.CallTool(ctx, ws, tool, in.Args)
	u.audit(ctx, AuditEvent{
		WorkspaceID: in.WorkspaceID,
		SourceID:    src.ID(),
		Tool:        tool,
		LatencyMS:   u.now().Sub(start).Milliseconds(),
		Status:      statusFrom(callErr, res),
		ErrorClass:  errorClass(callErr),
	})
	return res, callErr
}

func (u *CallToolUseCase) now() time.Time {
	if u.Clock == nil {
		return time.Now()
	}
	return u.Clock()
}

func (u *CallToolUseCase) audit(ctx context.Context, ev AuditEvent) {
	if u.Audit == nil {
		return
	}
	u.Audit.Record(ctx, ev)
}

func statusFrom(err error, res domainmcp.ToolResult) string {
	switch {
	case err != nil:
		return "error"
	case res.IsError:
		return "tool_error"
	default:
		return "ok"
	}
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
