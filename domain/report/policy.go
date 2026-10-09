package report

import (
	"errors"
	"sort"
	"strings"

	"vozko/domain/workspace"
)

const KindLeads Kind = "leads"

const FailureForbidden FailureCode = "forbidden"

var (
	ErrNoPolicy       = errors.New("report: this kind declares no access policy")
	ErrKindNotOffered = errors.New("report: this kind is requested from its own page, not through /reports")
	ErrNotAllowed     = errors.New("report: you do not have the permissions this report needs")
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrNotAllowed, "report_forbidden"},
	{ErrKindNotOffered, "report_kind_not_offered"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

type Policy struct {
	Required      []workspace.PermissionEntry
	Tier          string
	RequesterOnly bool
}

func (p Policy) Allows(holds func(workspace.PermissionEntry) bool) bool {
	if len(p.Required) == 0 || holds == nil {
		return false
	}
	for _, required := range p.Required {
		if !holds(required) {
			return false
		}
	}
	return true
}

func (p Policy) Readers() []string {
	if p.RequesterOnly || len(p.Required) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(p.Required))
	keys := make([]string, 0, len(p.Required))
	for _, required := range p.Required {
		key := required.Key()
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func ReadersHeld(readers, held []string) bool {
	if len(readers) == 0 {
		return false
	}
	holds := make(map[string]bool, len(held))
	for _, key := range held {
		holds[key] = true
	}
	for _, key := range readers {
		if !holds[key] {
			return false
		}
	}
	return true
}

type Guard interface {
	Policy(job Job) (Policy, error)
}

type Internal interface {
	Internal() bool
}

func PolicyOf(r Renderer, job Job) (Policy, error) {
	guard, ok := r.(Guard)
	if !ok {
		return Policy{}, ErrNoPolicy
	}
	return guard.Policy(job)
}

func IsInternal(r Renderer) bool {
	internal, ok := r.(Internal)
	return ok && internal.Internal()
}

func (j Job) ReadableBy(userID string, p Policy, holds func(workspace.PermissionEntry) bool) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	if userID == j.RequestedBy {
		return true
	}
	return !p.RequesterOnly && p.Allows(holds)
}

func (r *formatRouter) Policy(job Job) (Policy, error) {
	for _, format := range r.order {
		if policy, err := PolicyOf(r.renderers[format], job); !errors.Is(err, ErrNoPolicy) {
			return policy, err
		}
	}
	return Policy{}, ErrNoPolicy
}

func (r *formatRouter) Internal() bool {
	for _, format := range r.order {
		if IsInternal(r.renderers[format]) {
			return true
		}
	}
	return false
}
