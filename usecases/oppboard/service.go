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

type OpportunitySearcher interface {
	SearchByFilter(input opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error)
	SumValueByFilter(input opportunity.SearchByFilterInput) (int64, error)
}

type StageLister interface {
	EnsureDefaultOpportunityPipeline(workspaceID string) (string, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
}

type Authorizer interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

var (
	ErrUnauthorized       = errors.New("oppboard: unauthorized")
	ErrUnsupportedGroupBy = errors.New("oppboard: unsupported groupBy for opportunity board")
	ErrGroupByKeyMissing  = errors.New("oppboard: groupBy=custom requires a groupByKey")
)

type Service struct {
	searcher   OpportunitySearcher
	stages     StageLister
	authorizer Authorizer
}

func NewService(searcher OpportunitySearcher, stages StageLister, authorizer Authorizer) *Service {
	return &Service{searcher: searcher, stages: stages, authorizer: authorizer}
}

type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Option struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type Column struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Color      string                     `json:"color,omitempty"`
	IsWon      bool                       `json:"isWon,omitempty"`
	IsLost     bool                       `json:"isLost,omitempty"`
	Total      int64                      `json:"total"`
	ValueTotal int64                      `json:"valueTotal"`
	Entries    []*opportunity.Opportunity `json:"entries"`
}

type Board struct {
	GroupBy string   `json:"groupBy"`
	Columns []Column `json:"columns"`
}

type BoardInput struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool

	PipelineID string
	GroupBy    savedview.GroupBy
	GroupByKey string
	Filter     crmfilter.Filter
	Owners     []Owner
	Options    []Option

	SortField string
	SortOrder string
	Page      int
	PageSize  int
}

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

func (s *Service) pipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	pipelineID = strings.TrimSpace(pipelineID)
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
	if !isAdmin && restrict {
		assigneeOverride = userID
	}
	return deptIDs, restrict, assigneeOverride, nil
}

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
