package workflow_usecase

import (
	"context"
	"strings"

	"vozko/domain/agent"
	label_domain "vozko/domain/label"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	dept_domain "vozko/domain/workspace/workspace_department"
)

type modelLister interface {
	GetAvaibleModels(ctx context.Context) ([]string, error)
}
type agentLister interface {
	List(input agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error)
}
type departmentLister interface {
	ListDepartments(workspaceID string) ([]dept_domain.Department, error)
}
type labelLister interface {
	ListByWorkspace(workspaceID string) ([]*label_domain.Label, error)
}
type workflowLister interface {
	FindByWorkspaceID(workspaceID string) ([]*workflow.Workflow, error)
}
type memberLister interface {
	ListMembers(ctx context.Context, workspaceID, query string, limit int) ([]ResourceMatch, error)
}

type BuilderResourceResolverDeps struct {
	Models      modelLister
	Agents      agentLister
	Departments departmentLister
	Labels      labelLister
	Workflows   workflowLister
	Members     memberLister
}

type builderResourceResolver struct {
	deps BuilderResourceResolverDeps
}

func NewBuilderResourceResolver(deps BuilderResourceResolverDeps) ResourceResolver {
	return &builderResourceResolver{deps: deps}
}

func (r *builderResourceResolver) Search(ctx context.Context, workspaceID, kind, query string, limit int) ([]ResourceMatch, error) {
	if limit <= 0 || limit > 25 {
		limit = 5
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "*" || q == "%" {
		q = ""
	}
	match := func(name string) bool { return q == "" || strings.Contains(strings.ToLower(name), q) }

	switch kind {
	case "ai_models":
		if r.deps.Models == nil {
			return nil, nil
		}
		models, err := r.deps.Models.GetAvaibleModels(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]ResourceMatch, 0, limit)
		for _, m := range models {
			if !match(m) {
				continue
			}
			out = append(out, ResourceMatch{ID: m, Name: m})
			if len(out) >= limit {
				break
			}
		}
		return out, nil

	case "agents":
		if r.deps.Agents == nil {
			return nil, nil
		}
		res, err := r.deps.Agents.List(agent.ListAgentsInput{WorkspaceID: workspaceID, Search: query})
		if err != nil {
			return nil, err
		}
		out := make([]ResourceMatch, 0, limit)
		if res != nil {
			for _, a := range res.Items {
				if a == nil || a.WorkspaceID != workspaceID || !match(a.Name) {
					continue
				}
				out = append(out, ResourceMatch{ID: a.ID, Name: a.Name})
				if len(out) >= limit {
					break
				}
			}
		}
		return out, nil

	case "departments":
		if r.deps.Departments == nil {
			return nil, nil
		}
		deps, err := r.deps.Departments.ListDepartments(workspaceID)
		if err != nil {
			return nil, err
		}
		out := make([]ResourceMatch, 0, limit)
		for _, d := range deps {
			if d.WorkspaceID != workspaceID || !match(d.Name) {
				continue
			}
			out = append(out, ResourceMatch{ID: d.ID, Name: d.Name})
			if len(out) >= limit {
				break
			}
		}
		return out, nil

	case "labels":
		if r.deps.Labels == nil {
			return nil, nil
		}
		labels, err := r.deps.Labels.ListByWorkspace(workspaceID)
		if err != nil {
			return nil, err
		}
		out := make([]ResourceMatch, 0, limit)
		for _, l := range labels {
			if l == nil || l.WorkspaceID != workspaceID || !match(l.Name) {
				continue
			}
			out = append(out, ResourceMatch{ID: l.ID, Name: l.Name})
			if len(out) >= limit {
				break
			}
		}
		return out, nil

	case "workflows":
		if r.deps.Workflows == nil {
			return nil, nil
		}
		wfs, err := r.deps.Workflows.FindByWorkspaceID(workspaceID)
		if err != nil {
			return nil, err
		}
		out := make([]ResourceMatch, 0, limit)
		for _, w := range wfs {
			if w == nil || w.WorkspaceID != workspaceID || !match(w.Name) {
				continue
			}
			out = append(out, ResourceMatch{ID: w.ID, Name: w.Name})
			if len(out) >= limit {
				break
			}
		}
		return out, nil

	case "members":
		if r.deps.Members == nil {
			return nil, nil
		}
		members, err := r.deps.Members.ListMembers(ctx, workspaceID, query, limit)
		if err != nil {
			return nil, err
		}
		out := make([]ResourceMatch, 0, limit)
		for _, m := range members {
			if !match(m.Name) {
				continue
			}
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
		return out, nil

	default:
		return nil, nil
	}
}
