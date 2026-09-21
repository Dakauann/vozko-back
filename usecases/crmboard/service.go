package crmboard_usecase

import (
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/inbox_assignment"
	"vozko/domain/label"
	"vozko/domain/savedview"
	"vozko/domain/stage"
	"vozko/domain/workspace"
)

type EntrySearcher interface {
	SearchEntriesByFilter(input conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error)
}

type StageLister interface {
	ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
}

type LabelLister interface {
	Execute(workspaceID string) ([]*label.Label, error)
}

type Authorizer interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool
}

type AssignmentLookup interface {
	FindByEntries(workspaceID string, entryIDs []string) ([]*inbox_assignment.InboxAssignment, error)
}

var (
	ErrUnauthorized       = errors.New("crmboard: unauthorized")
	ErrUnsupportedGroupBy = errors.New("crmboard: unsupported groupBy for conversation board")
)

type Service struct {
	searcher    EntrySearcher
	stages      StageLister
	labels      LabelLister
	authorizer  Authorizer
	assignments AssignmentLookup
}

func NewService(searcher EntrySearcher, stages StageLister, labels LabelLister, authorizer Authorizer, assignments AssignmentLookup) *Service {
	return &Service{searcher: searcher, stages: stages, labels: labels, authorizer: authorizer, assignments: assignments}
}

type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Column struct {
	ID      string                              `json:"id"`
	Name    string                              `json:"name"`
	Color   string                              `json:"color,omitempty"`
	Total   int64                               `json:"total"`
	Entries []conversation.EntryWithLastMessage `json:"entries"`
}

type Board struct {
	GroupBy string   `json:"groupBy"`
	Columns []Column `json:"columns"`
}

type BoardInput struct {
	WorkspaceID          string
	UserID               string
	IsAdmin              bool
	SelectedDepartmentID string

	PipelineID string
	GroupBy    savedview.GroupBy
	Filter     crmfilter.Filter
	Owners     []Owner

	SortField string
	SortOrder string
	Page      int
	PageSize  int
}

type EntriesInput struct {
	WorkspaceID          string
	UserID               string
	IsAdmin              bool
	SelectedDepartmentID string

	Filter    crmfilter.Filter
	SortField string
	SortOrder string
	Page      int
	PageSize  int
}

func (s *Service) GetBoard(in BoardInput) (*Board, error) {
	deptIDs, restrict, assigneeOverride, assignedUserID, err := s.resolveScope(in.WorkspaceID, in.UserID, in.IsAdmin, in.SelectedDepartmentID)
	if err != nil {
		return nil, err
	}

	base := conversation.SearchByFilterInput{
		WorkspaceID:            in.WorkspaceID,
		DepartmentIDs:          deptIDs,
		RestrictDepartments:    restrict,
		AssigneeOverrideUserID: assigneeOverride,
		AssignedUserID:         assignedUserID,
		SortField:              in.SortField,
		SortOrder:              in.SortOrder,
		Page:                   in.Page,
		PageSize:               in.PageSize,
	}

	pipeStages, err := s.pipelineStages(in.WorkspaceID, in.PipelineID)
	if err != nil {
		return nil, err
	}

	type colSpec struct {
		id, name, color string
		predicate       crmfilter.Predicate
	}
	var specs []colSpec

	switch in.GroupBy {
	case savedview.GroupByStage, "":
		for _, st := range pipeStages {
			specs = append(specs, colSpec{
				id: st.ID, name: st.Name, color: st.Color,
				predicate: crmfilter.Predicate{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{st.ID}},
			})
		}

	case savedview.GroupByLabel:
		labels, err := s.labels.Execute(in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		for _, lb := range labels {
			specs = append(specs, colSpec{
				id: lb.ID, name: lb.Name, color: lb.Color,
				predicate: crmfilter.Predicate{Field: crmfilter.FieldLabel, Operator: crmfilter.OpIn, Values: []string{lb.ID}},
			})
		}

	case savedview.GroupByOwner:
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

	case savedview.GroupByNone:
		specs = append(specs, colSpec{id: "__all__", name: "Todos"})

	default:
		return nil, ErrUnsupportedGroupBy
	}

	boardFilter := in.Filter
	if in.GroupBy != savedview.GroupByStage && in.GroupBy != "" && in.PipelineID != "" && len(pipeStages) > 0 {
		stageIDs := make([]string, len(pipeStages))
		for i, st := range pipeStages {
			stageIDs[i] = st.ID
		}
		boardFilter = withPredicate(in.Filter, crmfilter.Predicate{
			Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: stageIDs,
		})
	}

	board := &Board{GroupBy: string(in.GroupBy), Columns: make([]Column, 0, len(specs))}
	for _, sp := range specs {
		colInput := base
		colInput.Filter = withPredicate(boardFilter, sp.predicate)
		entries, total, err := s.searcher.SearchEntriesByFilter(colInput)
		if err != nil {
			return nil, err
		}
		board.Columns = append(board.Columns, Column{
			ID: sp.id, Name: sp.name, Color: sp.color, Total: total, Entries: entries,
		})
	}

	ids := make([]string, 0)
	for ci := range board.Columns {
		for ei := range board.Columns[ci].Entries {
			if id := board.Columns[ci].Entries[ei].EntryID; id != "" {
				ids = append(ids, id)
			}
		}
	}
	if owners := s.ownerByEntry(in.WorkspaceID, ids); owners != nil {
		for ci := range board.Columns {
			for ei := range board.Columns[ci].Entries {
				if uid, ok := owners[board.Columns[ci].Entries[ei].EntryID]; ok {
					board.Columns[ci].Entries[ei].AssignedUserID = uid
				}
			}
		}
	}
	return board, nil
}

func (s *Service) GetEntries(in EntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	deptIDs, restrict, assigneeOverride, assignedUserID, err := s.resolveScope(in.WorkspaceID, in.UserID, in.IsAdmin, in.SelectedDepartmentID)
	if err != nil {
		return nil, 0, err
	}
	entries, total, err := s.searcher.SearchEntriesByFilter(conversation.SearchByFilterInput{
		WorkspaceID:            in.WorkspaceID,
		DepartmentIDs:          deptIDs,
		RestrictDepartments:    restrict,
		AssigneeOverrideUserID: assigneeOverride,
		AssignedUserID:         assignedUserID,
		Filter:                 in.Filter,
		SortField:              in.SortField,
		SortOrder:              in.SortOrder,
		Page:                   in.Page,
		PageSize:               in.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(entries))
	for i := range entries {
		if entries[i].EntryID != "" {
			ids = append(ids, entries[i].EntryID)
		}
	}
	if owners := s.ownerByEntry(in.WorkspaceID, ids); owners != nil {
		for i := range entries {
			if uid, ok := owners[entries[i].EntryID]; ok {
				entries[i].AssignedUserID = uid
			}
		}
	}
	return entries, total, nil
}

func (s *Service) ownerByEntry(workspaceID string, ids []string) map[string]string {
	if s.assignments == nil || len(ids) == 0 {
		return nil
	}
	assignments, err := s.assignments.FindByEntries(workspaceID, ids)
	if err != nil {
		return nil
	}
	byEntry := make(map[string]string, len(assignments))
	for _, a := range assignments {
		if a != nil && a.AssignedUserID != "" {
			byEntry[a.EntryID] = a.AssignedUserID
		}
	}
	return byEntry
}

func (s *Service) pipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if pipelineID == "" {
		return s.stages.ListByCampaign(workspaceID, "", "")
	}
	return s.stages.ListByPipeline(workspaceID, pipelineID)
}

func (s *Service) resolveScope(workspaceID, userID string, isAdmin bool, selectedDepartmentID string) (deptIDs []string, restrict bool, assigneeOverride string, assignedUserID string, err error) {
	if s.authorizer == nil {
		return nil, false, "", "", nil
	}
	scope, allowed := s.authorizer.GetDepartmentScope(userID, workspaceID, isAdmin)
	if !allowed {
		return nil, false, "", "", ErrUnauthorized
	}
	deptIDs = scope.DepartmentIDs
	restrict = scope.Restrict
	if selectedDepartmentID != "" && (scope.Restrict || scope.WorkspaceHasDepartments) {
		if scope.Restrict {
			found := false
			for _, id := range scope.DepartmentIDs {
				if id == selectedDepartmentID {
					found = true
					break
				}
			}
			if !found {
				return nil, false, "", "", ErrUnauthorized
			}
		}
		deptIDs = []string{selectedDepartmentID}
		restrict = true
	}
	if !isAdmin && restrict {
		assigneeOverride = userID
	}
	if !isAdmin &&
		!s.authorizer.IsWorkspaceOwnerOrAdmin(userID, workspaceID) &&
		!s.authorizer.HasWorkspacePermission(userID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionViewOthers), false) {
		assignedUserID = userID
	}
	return deptIDs, restrict, assigneeOverride, assignedUserID, nil
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
