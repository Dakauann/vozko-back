// Package copilot defines the domain contracts for the dashboard AI copilot — the
// in-app assistant that operates the product on the logged-in user's behalf
// through tools. A copilot Tool is a thin adapter over an existing usecase, so it
// inherits that usecase's validation, workspace scoping, and business rules
// instead of re-implementing them. The copilot runs on the shared agentloop
// harness (the same engine the workflow builder uses); these types are the
// domain-specific plug.
package copilot

import (
	"context"

	"vozko/domain/tools"
	"vozko/domain/workspace"
)

// Context is the authenticated scope for one copilot session, built server-side
// from the chat thread. The model never supplies any of these fields — they are
// injected into every usecase call so a tool can only ever touch the caller's
// own workspace (and, for restricted members, their own departments).
type Context struct {
	WorkspaceID string
	UserID      string
	Role        workspace.Role
	DeptScope   []string // nil = all departments; otherwise the allowed department ids
}

// Meta declares a tool's authorization + mutation semantics. The harness — not
// the model — reads these to enforce RBAC (via CheckAccess) and the human-approval
// gate, so a prompt-injection cannot talk its way past either.
type Meta struct {
	Mutating bool               // true ⇒ requires explicit user approval before it runs
	Resource workspace.Resource // RBAC resource, e.g. ResourceAgents
	Action   workspace.Action   // RBAC action, e.g. ActionRead / ActionCreate
}

// Status is the coarse outcome of executing a tool.
type Status string

const (
	StatusOK     Status = "ok"
	StatusDenied Status = "denied" // RBAC refused
	StatusError  Status = "error"  // the usecase/validation rejected the input
)

// Result is what a tool produced. On StatusOK, Data is serialized back to the
// model; otherwise Message carries a humanized note the model can act on.
type Result struct {
	Status  Status
	Data    interface{}
	Message string
}

// PendingAction is a mutating tool call awaiting explicit user approval. It is
// surfaced to the client as a confirmation card and replayed verbatim on approval.
type PendingAction struct {
	ID       string                 `json:"id"`
	ToolName string                 `json:"toolName"`
	Args     map[string]interface{} `json:"args"`
	Summary  string                 `json:"summary"`
}

// Tool is one copilot capability: a thin adapter over an existing usecase.
type Tool interface {
	Definition() tools.Definition
	Meta() Meta
	// Execute performs the real work (a read, or — once approved — a mutation).
	// Scope MUST be taken from cc, never from args.
	Execute(ctx context.Context, cc Context, args map[string]interface{}) Result
}
