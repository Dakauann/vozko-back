package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmcp "vozko/domain/agent/mcp"
	memrepo "vozko/infra/repositories/agent/mcp"
)

type recAudit struct{ events []AuditEvent }

func (r *recAudit) Record(_ context.Context, ev AuditEvent) { r.events = append(r.events, ev) }

type denyLimiter struct{ err error }

func (d denyLimiter) Allow(context.Context, string) error { return d.err }

func setupCallTool(t *testing.T, fs *fakeSource) (*CallToolUseCase, *recAudit) {
	t.Helper()
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", Builder: func(*domainmcp.Credential) domainmcp.ToolSource { return fs }})
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "k", Status: domainmcp.StatusConnected})
	r := NewRegistry(cat, bindings, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), newVault(t))
	uc := NewCallToolUseCase(r)
	au := &recAudit{}
	uc.Audit = au
	return uc, au
}

func TestCallToolHappy(t *testing.T) {
	fs := &fakeSource{id: "builtin:k", kind: domainmcp.KindBuiltin, calls: map[string]domainmcp.ToolResult{"t": domainmcp.TextResult("hi")}}
	uc, au := setupCallTool(t, fs)
	res, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].Text != "hi" {
		t.Fatal()
	}
	if len(au.events) != 1 || au.events[0].Status != "ok" {
		t.Fatalf("%+v", au.events)
	}
}

func TestCallToolToolError(t *testing.T) {
	fs := &fakeSource{id: "builtin:k", calls: map[string]domainmcp.ToolResult{"t": domainmcp.ErrorResult("e")}}
	uc, au := setupCallTool(t, fs)
	_, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"})
	if err != nil {
		t.Fatal()
	}
	if au.events[0].Status != "tool_error" {
		t.Fatal()
	}
}

func TestCallToolErr(t *testing.T) {
	fs := &fakeSource{id: "builtin:k", err: errors.New("boom")}
	uc, au := setupCallTool(t, fs)
	_, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"})
	if err == nil {
		t.Fatal()
	}
	if au.events[0].Status != "error" || au.events[0].ErrorClass == "" {
		t.Fatalf("%+v", au.events)
	}
}

func TestCallToolResolveError(t *testing.T) {
	uc, _ := setupCallTool(t, &fakeSource{id: "builtin:k"})
	_, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "bad"})
	if err == nil {
		t.Fatal()
	}
}

func TestCallToolLimiter(t *testing.T) {
	fs := &fakeSource{id: "builtin:k", calls: map[string]domainmcp.ToolResult{"t": domainmcp.TextResult("x")}}
	uc, _ := setupCallTool(t, fs)
	uc.Limiter = denyLimiter{err: errors.New("rate")}
	if _, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"}); err == nil {
		t.Fatal()
	}
	uc.Limiter = denyLimiter{err: nil}
	if _, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"}); err != nil {
		t.Fatal(err)
	}
}

func TestCallToolClockNilNow(t *testing.T) {
	fs := &fakeSource{id: "builtin:k", calls: map[string]domainmcp.ToolResult{"t": domainmcp.TextResult("x")}}
	uc, _ := setupCallTool(t, fs)
	uc.Clock = nil
	if _, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"}); err != nil {
		t.Fatal(err)
	}
}

func TestCallToolNilAudit(t *testing.T) {
	fs := &fakeSource{id: "builtin:k", calls: map[string]domainmcp.ToolResult{"t": domainmcp.TextResult("x")}}
	uc, _ := setupCallTool(t, fs)
	uc.Audit = nil
	if _, err := uc.Execute(context.Background(), CallToolInput{WorkspaceID: "ws", Name: "builtin:k.t"}); err != nil {
		t.Fatal(err)
	}
}

func TestNoopAudit(t *testing.T) { NoopAudit{}.Record(context.Background(), AuditEvent{}) }

func TestStatusFromMatrix(t *testing.T) {
	if statusFrom(nil, domainmcp.ToolResult{}) != "ok" {
		t.Fatal()
	}
	if statusFrom(nil, domainmcp.ErrorResult("x")) != "tool_error" {
		t.Fatal()
	}
	if statusFrom(errors.New("x"), domainmcp.ToolResult{}) != "error" {
		t.Fatal()
	}
	if errorClass(nil) != "" {
		t.Fatal()
	}
	if errorClass(errors.New("z")) != "z" {
		t.Fatal()
	}
}

var _ = time.Now
