// Package crmboard_usecase renders the workspace-global CRM board and flat list
// from a reusable crmfilter.Filter. It is the read side of Phase 1: the board is
// Board = groupBy(axis) over filter(view), decoupled from any campaign. Every
// axis (stage / label / owner) reuses ONE searcher (SearchEntriesByFilter) by
// appending a per-column predicate to the shared filter, so a new axis is just a
// new predicate, never a new query.
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

// Ports (narrow, satisfied by existing implementations) -----------------------

// EntrySearcher is the filter-driven read path (conversation.MessageRepository).
type EntrySearcher interface {
	SearchEntriesByFilter(input conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error)
}

// StageLister returns the workspace-global conversation pipeline stages
// (stage.Repository.ListByCampaign, which is campaign-decoupled since the stage
// promotion migration).
type StageLister interface {
	ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error)
	// ListByPipeline returns exactly the stages of the given pipeline (its own
	// columns), so selecting a non-default funnel renders THAT funnel, not the default.
	ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error)
}

// LabelLister returns the workspace labels (label.ListLabelsUseCase).
type LabelLister interface {
	Execute(workspaceID string) ([]*label.Label, error)
}

// Authorizer resolves the caller's visibility (conversation.ConversationAuthorizer).
// Beyond department scope it answers whether the caller may see conversations
// assigned to OTHER members, the same check the inbox uses, so the board can
// apply the identical self-scope instead of leaking every member's entries.
type Authorizer interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool
}

// AssignmentLookup batch-resolves the inbox owner (responsável) of a set of
// entries (inbox_assignment.Repository). It lets the board/list cards show who
// owns each conversation without threading the assignee through the search SQL.
type AssignmentLookup interface {
	FindByEntries(workspaceID string, entryIDs []string) ([]*inbox_assignment.InboxAssignment, error)
}

var (
	ErrUnauthorized       = errors.New("crmboard: unauthorized")
	ErrUnsupportedGroupBy = errors.New("crmboard: unsupported groupBy for conversation board")
)

// Service renders boards and lists.
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

// Owner names one owner-axis column (id + display name), provided by the caller
// (the frontend already knows the assignable member list).
type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Column is one board column: its axis identity plus a counted, paginated slice.
type Column struct {
	ID      string                              `json:"id"`
	Name    string                              `json:"name"`
	Color   string                              `json:"color,omitempty"`
	Total   int64                               `json:"total"`
	Entries []conversation.EntryWithLastMessage `json:"entries"`
}

// Board is the rendered result.
type Board struct {
	GroupBy string   `json:"groupBy"`
	Columns []Column `json:"columns"`
}

// BoardInput drives GetBoard. WorkspaceID/UserID/IsAdmin resolve visibility;
// SelectedDepartmentID narrows it (mirroring the inbox department filter).
type BoardInput struct {
	WorkspaceID          string
	UserID               string
	IsAdmin              bool
	SelectedDepartmentID string

	PipelineID string
	GroupBy    savedview.GroupBy
	Filter     crmfilter.Filter
	Owners     []Owner // owner axis columns (optional)

	SortField string
	SortOrder string
	Page      int
	PageSize  int
}

// EntriesInput drives GetEntries (the flat list sharing the board's filter).
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

// GetBoard renders the columns for the requested axis. Each column reuses
// SearchEntriesByFilter with the base filter plus one appended predicate.
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

	// The selected pipeline scopes the ENTIRE board, not only the stage columns.
	// Resolve its stages once: they are the stage-axis columns AND the set every other
	// axis is scoped to. Without this, label/owner/none render the whole workspace and
	// silently ignore the chosen funnel, industry-standard CRMs (Kommo, HubSpot,
	// Pipedrive) scope every grouping of a board to its pipeline.
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
		// Trailing "unassigned" swimlane so nothing silently vanishes.
		specs = append(specs, colSpec{
			id: "__unassigned__", name: "Sem responsável",
			predicate: crmfilter.Predicate{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIsEmpty},
		})

	case savedview.GroupByNone:
		specs = append(specs, colSpec{id: "__all__", name: "Todos"})

	default:
		// carteira / custom are not modeled on a conversation entry yet.
		return nil, ErrUnsupportedGroupBy
	}

	// On non-stage axes the columns carry no stage predicate, so scope the shared base
	// filter to the pipeline's stages here (the stage axis is already scoped per column).
	// A conversation "belongs to" this pipeline iff its current stage is one of these,
	// so unstaged and other-pipeline conversations correctly drop out of every axis.
	// An empty PipelineID on a non-stage axis is the explicit "Todos os funis" (all
	// funnels) choice: responsável / etiqueta are workspace-global, so the user can
	// deliberately view them across every pipeline (HubSpot's "All Pipelines"). A
	// concrete id scopes them to that funnel; the stage axis always needs a concrete
	// funnel and is scoped column-by-column regardless.
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

	// Hydrate the responsável on every card in ONE batched lookup across all
	// columns (owner is a display enrichment; a lookup error never fails the board).
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

// GetEntries renders the flat list view over the same filter (no columns).
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

// ownerByEntry batches one inbox_assignment lookup and returns entryID -> ownerID
// for the given entries. Returns nil when there is no lookup, no ids, or the
// lookup errors: the responsável is a non-critical display enrichment, so it must
// never fail the board/list read.
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

// pipelineStages returns the columns for the board: the stages of the SELECTED
// pipeline. With no pipeline chosen, ListByCampaign("","") resolves the workspace
// default. A specific pipeline is loaded DIRECTLY by id, never by filtering the
// default's stages, which could never match a non-default pipeline and made every
// non-default funnel silently render the default board.
func (s *Service) pipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if pipelineID == "" {
		return s.stages.ListByCampaign(workspaceID, "", "")
	}
	return s.stages.ListByPipeline(workspaceID, pipelineID)
}

// resolveScope mirrors inbox_service.SearchInbox in full: department visibility
// from the authorizer (narrowed by an explicitly selected department), the
// department-scope widener that keeps a restricted member's own conversations
// visible, AND, the piece the board was missing, the member-level self-scope.
//
// assignedUserID is the self-scope: a caller who is not admin, not workspace
// owner, and lacks conversations:view_others may only see conversations assigned
// to them (or unassigned). Without it, a member who was not department-restricted
// (restrict=false -> no department clause) saw every member's entries on the
// table and kanban even though their inbox scoped correctly.
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
	// Self-scope, identical to inbox_service.SearchInbox: keep AssignedUserID unset
	// (see everyone) only for admins, workspace owners, and members holding
	// conversations:view_others; otherwise pin the caller to their own entries.
	if !isAdmin &&
		!s.authorizer.IsWorkspaceOwnerOrAdmin(userID, workspaceID) &&
		!s.authorizer.HasWorkspacePermission(userID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionViewOthers), false) {
		assignedUserID = userID
	}
	return deptIDs, restrict, assigneeOverride, assignedUserID, nil
}

// withPredicate returns a copy of base with an extra single-predicate AND group.
// Top-level groups combine with AND, so the column predicate always narrows the
// view. The base groups are copied so concurrent columns never share a slice.
func withPredicate(base crmfilter.Filter, p crmfilter.Predicate) crmfilter.Filter {
	if p.Field == "" && p.Operator == "" {
		// GroupByNone: no column predicate.
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
