// Package oppboard_usecase renders the opportunity board (the sales "Funil de
// Vendas") and its flat list from a reusable crmfilter.Filter. It is the read
// side of the deal funnel: the board is Board = groupBy(axis) over filter, where
// every axis (stage / owner / custom) reuses ONE searcher (SearchByFilter) by
// appending a per-column predicate to the shared filter, so a new axis is just a
// new predicate, never a new query. It mirrors crmboard_usecase, but over
// opportunities instead of conversation entries, and every column additionally
// carries ValueTotal = SUM(value_cents) across all matches.
//
// MONEY GUARDRAIL: ValueTotal is summed value_cents (the customer's own BRL sales
// figure). It is NOT Vozko platform money and never touches the USD-micros
// wallet / saldo / ledger / FX.
package oppboard_usecase

import (
	"errors"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/opportunity"
	"vozko/domain/savedview"
	"vozko/domain/stage"
)

// Ports (narrow, satisfied by existing implementations) -----------------------

// OpportunitySearcher is the filter-driven read path (opportunity.Repository).
type OpportunitySearcher interface {
	SearchByFilter(input opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error)
	SumValueByFilter(input opportunity.SearchByFilterInput) (int64, error)
}

// StageLister returns the workspace's stages so the stage axis can be built from
// the requested pipeline's stages (stage.Repository.ListByWorkspace, filtered by
// pipeline_id here).
type StageLister interface {
	// EnsureDefaultOpportunityPipeline returns (creating + seeding if absent) the
	// workspace's default sales pipeline id, so the board renders out of the box.
	EnsureDefaultOpportunityPipeline(workspaceID string) (string, error)
	// ListByPipeline scopes columns to one pipeline's stages (not the whole
	// workspace), which is what keeps the deal board coherent.
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
}

// Authorizer gates board/list access AND supplies the department scope. A deal is
// scoped by its OWNER's department membership (owners play the role conversation
// assignees do), so a restricted user sees only their department's / their own deals.
// It may be nil (no gate, no scope).
type Authorizer interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

var (
	ErrUnauthorized       = errors.New("oppboard: unauthorized")
	ErrUnsupportedGroupBy = errors.New("oppboard: unsupported groupBy for opportunity board")
	ErrGroupByKeyMissing  = errors.New("oppboard: groupBy=custom requires a groupByKey")
)

// Service renders opportunity boards and lists.
type Service struct {
	searcher   OpportunitySearcher
	stages     StageLister
	authorizer Authorizer
}

func NewService(searcher OpportunitySearcher, stages StageLister, authorizer Authorizer) *Service {
	return &Service{searcher: searcher, stages: stages, authorizer: authorizer}
}

// Owner names one owner-axis column (id + display name), provided by the caller
// (the frontend already knows the assignable member list).
type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Option names one custom-axis column: the stored value plus its display label.
type Option struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// Column is one board column: its axis identity, a counted + value-summed header,
// and a page of opportunities.
type Column struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	// IsWon/IsLost mark terminal stage columns so the board can style them and the
	// client can send the right status (won / lost + reason) on a move into them.
	// Only populated on the stage axis.
	IsWon  bool `json:"isWon,omitempty"`
	IsLost bool `json:"isLost,omitempty"`
	// Total is the count of opportunities in the column across all matches.
	Total int64 `json:"total"`
	// ValueTotal is SUM(value_cents) across all matches (customer BRL cents).
	ValueTotal int64                      `json:"valueTotal"`
	Entries    []*opportunity.Opportunity `json:"entries"`
}

// Board is the rendered result.
type Board struct {
	GroupBy string   `json:"groupBy"`
	Columns []Column `json:"columns"`
}

// BoardInput drives GetBoard. WorkspaceID/UserID/IsAdmin gate visibility.
type BoardInput struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool

	PipelineID string
	GroupBy    savedview.GroupBy
	GroupByKey string // custom-field key when GroupBy == custom
	Filter     crmfilter.Filter
	Owners     []Owner  // owner axis columns
	Options    []Option // custom axis columns

	SortField string
	SortOrder string
	Page      int
	PageSize  int
}

// ListInput drives GetList (the flat list sharing the board's filter).
type ListInput struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool

	Filter    crmfilter.Filter
	SortField string
	SortOrder string
	Page      int
	PageSize  int
}

type colSpec struct {
	id, name, color string
	isWon, isLost   bool
	predicate       crmfilter.Predicate
}

// GetBoard renders the columns for the requested axis. Each column reuses
// SearchByFilter (+ SumValueByFilter) with the base filter plus one appended
// predicate.
func (s *Service) GetBoard(in BoardInput) (*Board, error) {
	deptIDs, restrict, assigneeOverride, err := s.resolveScope(in.UserID, in.WorkspaceID, in.IsAdmin)
	if err != nil {
		return nil, err
	}

	specs, err := s.columnSpecs(in)
	if err != nil {
		return nil, err
	}

	board := &Board{GroupBy: string(in.GroupBy), Columns: make([]Column, 0, len(specs))}
	for _, sp := range specs {
		colInput := opportunity.SearchByFilterInput{
			WorkspaceID:            in.WorkspaceID,
			Filter:                 withPredicate(in.Filter, sp.predicate),
			DepartmentIDs:          deptIDs,
			RestrictDepartments:    restrict,
			AssigneeOverrideUserID: assigneeOverride,
			SortField:              in.SortField,
			SortOrder:              in.SortOrder,
			Page:                   in.Page,
			PageSize:               in.PageSize,
		}
		entries, total, err := s.searcher.SearchByFilter(colInput)
		if err != nil {
			return nil, err
		}
		valueTotal, err := s.searcher.SumValueByFilter(colInput)
		if err != nil {
			return nil, err
		}
		board.Columns = append(board.Columns, Column{
			ID: sp.id, Name: sp.name, Color: sp.color,
			IsWon: sp.isWon, IsLost: sp.isLost,
			Total: total, ValueTotal: valueTotal, Entries: entries,
		})
	}
	return board, nil
}

// GetList renders the flat list view over the same filter (no columns).
func (s *Service) GetList(in ListInput) ([]*opportunity.Opportunity, int64, error) {
	deptIDs, restrict, assigneeOverride, err := s.resolveScope(in.UserID, in.WorkspaceID, in.IsAdmin)
	if err != nil {
		return nil, 0, err
	}
	return s.searcher.SearchByFilter(opportunity.SearchByFilterInput{
		WorkspaceID:            in.WorkspaceID,
		Filter:                 in.Filter,
		DepartmentIDs:          deptIDs,
		RestrictDepartments:    restrict,
		AssigneeOverrideUserID: assigneeOverride,
		SortField:              in.SortField,
		SortOrder:              in.SortOrder,
		Page:                   in.Page,
		PageSize:               in.PageSize,
	})
}

// columnSpecs builds the per-axis column specs. Stage columns come from the
// pipeline's stages; owner/custom columns are provided by the caller.
func (s *Service) columnSpecs(in BoardInput) ([]colSpec, error) {
	switch in.GroupBy {
	case savedview.GroupByStage, "":
		stages, err := s.pipelineStages(in.WorkspaceID, in.PipelineID)
		if err != nil {
			return nil, err
		}
		specs := make([]colSpec, 0, len(stages))
		for _, st := range stages {
			specs = append(specs, colSpec{
				id: st.ID, name: st.Name, color: st.Color,
				isWon: st.IsWon, isLost: st.IsLost,
				predicate: crmfilter.Predicate{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{st.ID}},
			})
		}
		return specs, nil

	case savedview.GroupByOwner:
		specs := make([]colSpec, 0, len(in.Owners)+1)
		for _, o := range in.Owners {
			specs = append(specs, colSpec{
				id: o.ID, name: o.Name,
				predicate: crmfilter.Predicate{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIn, Values: []string{o.ID}},
			})
		}
		// Trailing "unassigned" swimlane so nothing silently vanishes.
		specs = append(specs, colSpec{
			id: "__unassigned__", name: "Sem responsável",
			predicate: crmfilter.Predicate{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIsEmpty},
		})
		return specs, nil

	case savedview.GroupByCustom:
		key := strings.TrimSpace(in.GroupByKey)
		if key == "" {
			return nil, ErrGroupByKeyMissing
		}
		specs := make([]colSpec, 0, len(in.Options))
		for _, opt := range in.Options {
			name := opt.Name
			if strings.TrimSpace(name) == "" {
				name = opt.Value
			}
			specs = append(specs, colSpec{
				id: opt.Value, name: name,
				predicate: crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: key, Operator: crmfilter.OpEquals, Values: []string{opt.Value}},
			})
		}
		return specs, nil

	default:
		return nil, ErrUnsupportedGroupBy
	}
}

// pipelineStages returns the requested pipeline's stages. When a pipeline id is
// given, only its stages are used; when empty, the whole workspace stage set is
// returned (the caller is expected to scope the funnel to one pipeline).
func (s *Service) pipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	pipelineID = strings.TrimSpace(pipelineID)
	// No pipeline pinned: self-heal to the workspace's default sales pipeline so the
	// board always has coherent, single-pipeline columns (never the whole
	// workspace's stage set from every pipeline).
	if pipelineID == "" {
		pid, err := s.stages.EnsureDefaultOpportunityPipeline(workspaceID)
		if err != nil {
			return nil, err
		}
		pipelineID = pid
	}
	if pipelineID == "" {
		return []*stage.Stage{}, nil
	}
	return s.stages.ListByPipeline(workspaceID, pipelineID)
}

// resolveScope gates access AND resolves the department scope threaded into every
// board/list query, so a department-restricted user only sees deals owned by their
// department (or owned by themselves). Mirrors crmboard.resolveScope; owner plays the
// role the conversation assignee does. A nil authorizer means no gate and no scope.
func (s *Service) resolveScope(userID, workspaceID string, isAdmin bool) (deptIDs []string, restrict bool, assigneeOverride string, err error) {
	if s.authorizer == nil {
		return nil, false, "", nil
	}
	scope, allowed := s.authorizer.GetDepartmentScope(userID, workspaceID, isAdmin)
	if !allowed {
		return nil, false, "", ErrUnauthorized
	}
	deptIDs = scope.DepartmentIDs
	restrict = scope.Restrict
	// A restricted non-admin can always see their OWN deals even if owned outside
	// their departments (matches the conversation board's assignee override).
	if !isAdmin && restrict {
		assigneeOverride = userID
	}
	return deptIDs, restrict, assigneeOverride, nil
}

// withPredicate returns a copy of base with an extra single-predicate AND group.
// Top-level groups combine with AND, so the column predicate always narrows the
// view. The base groups are copied so concurrent columns never share a slice.
func withPredicate(base crmfilter.Filter, p crmfilter.Predicate) crmfilter.Filter {
	if p.Field == "" && p.Operator == "" {
		return base
	}
	groups := make([]crmfilter.Group, 0, len(base.Groups)+1)
	groups = append(groups, base.Groups...)
	groups = append(groups, crmfilter.Group{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{p},
	})
	return crmfilter.Filter{Groups: groups}
}
