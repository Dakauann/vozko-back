package crmboard_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/inbox_assignment"
	"vozko/domain/label"
	"vozko/domain/savedview"
	"vozko/domain/stage"
)

type fakeSearcher struct {
	entries []conversation.EntryWithLastMessage
	total   int64
	calls   []conversation.SearchByFilterInput
	err     error
}

func (f *fakeSearcher) SearchEntriesByFilter(in conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]conversation.EntryWithLastMessage, len(f.entries))
	copy(out, f.entries)
	return out, f.total, nil
}

type fakeStages struct {
	stages     []*stage.Stage
	byPipeline map[string][]*stage.Stage
}

func (f *fakeStages) ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	return f.stages, nil
}

func (f *fakeStages) ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if f.byPipeline != nil {
		return f.byPipeline[pipelineID], nil
	}
	return f.stages, nil
}

type fakeLabels struct{ labels []*label.Label }

func (f *fakeLabels) Execute(workspaceID string) ([]*label.Label, error) { return f.labels, nil }

type fakeAuthorizer struct {
	scope      conversation.DepartmentAccessScope
	allowed    bool
	owner      bool
	viewOthers bool
}

func (f *fakeAuthorizer) GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool) {
	return f.scope, f.allowed
}

func (f *fakeAuthorizer) IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool {
	return f.owner
}

func (f *fakeAuthorizer) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	return f.viewOthers
}

type fakeAssignments struct{ byEntry map[string]string }

func (f *fakeAssignments) FindByEntries(workspaceID string, entryIDs []string) ([]*inbox_assignment.InboxAssignment, error) {
	var out []*inbox_assignment.InboxAssignment
	for _, id := range entryIDs {
		if uid, ok := f.byEntry[id]; ok {
			out = append(out, &inbox_assignment.InboxAssignment{EntryID: id, AssignedUserID: uid})
		}
	}
	return out, nil
}

func sampleEntries() []conversation.EntryWithLastMessage {
	return []conversation.EntryWithLastMessage{
		{EntryID: "e1", EntryType: "whatsapp", LeadName: "Alice"},
		{EntryID: "e2", EntryType: "voice", LeadName: "Bob"},
	}
}

func ownerOf(entries []conversation.EntryWithLastMessage, id string) string {
	for _, e := range entries {
		if e.EntryID == id {
			return e.AssignedUserID
		}
	}
	return ""
}

func allBoardEntries(b *Board) []conversation.EntryWithLastMessage {
	var out []conversation.EntryWithLastMessage
	for _, c := range b.Columns {
		out = append(out, c.Entries...)
	}
	return out
}

func TestGetBoard_NonDefaultPipeline_RendersOwnStages_NotDefault(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{
		stages: []*stage.Stage{
			{ID: "d1", Name: "recebido", PipelineID: "default"},
			{ID: "d2", Name: "em atendimento", PipelineID: "default"},
			{ID: "d3", Name: "finalizado", PipelineID: "default"},
		},
		byPipeline: map[string][]*stage.Stage{
			"pipe-new": {
				{ID: "n1", Name: "prospectando", PipelineID: "pipe-new"},
				{ID: "n2", Name: "interessado", PipelineID: "pipe-new"},
			},
		},
	}
	svc := NewService(searcher, stages, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	board, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByStage, PipelineID: "pipe-new"})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 2 {
		t.Fatalf("selecting pipe-new must render its 2 stages, got %d default-looking columns: %+v", len(board.Columns), board.Columns)
	}
	for i, want := range []string{"n1", "n2"} {
		if board.Columns[i].ID != want {
			t.Errorf("column %d = %q, want %q, the board leaked the DEFAULT pipeline's stages", i, board.Columns[i].ID, want)
		}
	}
}

func TestGetBoard_NoPipelineSelected_UsesDefault(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{
		stages:     []*stage.Stage{{ID: "d1", Name: "recebido", PipelineID: "default"}},
		byPipeline: map[string][]*stage.Stage{"pipe-new": {{ID: "n1"}}},
	}
	svc := NewService(searcher, stages, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	board, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByStage, PipelineID: ""})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 1 || board.Columns[0].ID != "d1" {
		t.Fatalf("no pipeline selected must render the default, got %+v", board.Columns)
	}
}

func filterHasStageScope(f crmfilter.Filter, stageIDs []string) bool {
	for _, g := range f.Groups {
		for _, p := range g.Predicates {
			if p.Field != crmfilter.FieldStage || p.Operator != crmfilter.OpIn {
				continue
			}
			if len(p.Values) != len(stageIDs) {
				continue
			}
			have := map[string]bool{}
			for _, v := range p.Values {
				have[v] = true
			}
			all := true
			for _, want := range stageIDs {
				if !have[want] {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
	}
	return false
}

func TestGetBoard_LabelAxis_ScopedToSelectedPipeline(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{byPipeline: map[string][]*stage.Stage{
		"pipe-x": {{ID: "x1", PipelineID: "pipe-x"}, {ID: "x2", PipelineID: "pipe-x"}},
	}}
	labels := &fakeLabels{labels: []*label.Label{{ID: "l1", Name: "VIP"}, {ID: "l2", Name: "Lead"}}}
	svc := NewService(searcher, stages, labels, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByLabel, PipelineID: "pipe-x"}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(searcher.calls) != 2 {
		t.Fatalf("expected one search per label column, got %d", len(searcher.calls))
	}
	for i, call := range searcher.calls {
		if !filterHasStageScope(call.Filter, []string{"x1", "x2"}) {
			t.Errorf("label column %d searched WITHOUT the pipeline stage scope, the funnel is ignored and the whole workspace leaks: %+v", i, call.Filter)
		}
	}
}

func TestGetBoard_LabelAxis_AllFunnels_NotScoped(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{stages: []*stage.Stage{{ID: "d1"}, {ID: "d2"}}}
	labels := &fakeLabels{labels: []*label.Label{{ID: "l1", Name: "VIP"}}}
	svc := NewService(searcher, stages, labels, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByLabel, PipelineID: ""}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for i, call := range searcher.calls {
		for _, g := range call.Filter.Groups {
			for _, p := range g.Predicates {
				if p.Field == crmfilter.FieldStage {
					t.Errorf("label column %d was scoped by a stage predicate under 'Todos os funis', the global view must span all pipelines: %+v", i, p)
				}
			}
		}
	}
}

func TestGetBoard_OwnerAxis_ScopedToSelectedPipeline(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{byPipeline: map[string][]*stage.Stage{
		"pipe-x": {{ID: "x1"}, {ID: "x2"}},
	}}
	svc := NewService(searcher, stages, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	if _, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1", GroupBy: savedview.GroupByOwner, PipelineID: "pipe-x",
		Owners: []Owner{{ID: "agent-1", Name: "Agent One"}},
	}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for i, call := range searcher.calls {
		if !filterHasStageScope(call.Filter, []string{"x1", "x2"}) {
			t.Errorf("owner column %d searched WITHOUT the pipeline stage scope: %+v", i, call.Filter)
		}
	}
}

func TestGetBoard_StageAxis_NotDoubleScoped(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{byPipeline: map[string][]*stage.Stage{
		"pipe-x": {{ID: "x1"}, {ID: "x2"}},
	}}
	svc := NewService(searcher, stages, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByStage, PipelineID: "pipe-x"}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for i, call := range searcher.calls {
		if filterHasStageScope(call.Filter, []string{"x1", "x2"}) {
			t.Errorf("stage column %d got a redundant all-stages scope; columns must narrow to a single stage: %+v", i, call.Filter)
		}
	}
}

func TestGetBoard_StageAxisHydratesOwner(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{stages: []*stage.Stage{
		{ID: "s1", Name: "Novo", Color: "#111", PipelineID: "p1"},
		{ID: "s2", Name: "Fechado", Color: "#222", PipelineID: "p1"},
	}}
	assignments := &fakeAssignments{byEntry: map[string]string{"e1": "agent-1"}}
	svc := NewService(searcher, stages, &fakeLabels{}, &fakeAuthorizer{allowed: true}, assignments)

	board, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByStage, PipelineID: "p1"})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 2 {
		t.Fatalf("expected 2 stage columns, got %d", len(board.Columns))
	}
	if board.Columns[0].ID != "s1" || board.Columns[0].Name != "Novo" || board.Columns[0].Color != "#111" {
		t.Fatalf("unexpected first column: %+v", board.Columns[0])
	}
	all := allBoardEntries(board)
	if got := ownerOf(all, "e1"); got != "agent-1" {
		t.Fatalf("expected e1 owner agent-1, got %q", got)
	}
	if got := ownerOf(all, "e2"); got != "" {
		t.Fatalf("expected e2 unassigned, got %q", got)
	}
}

func TestGetBoard_OwnerAxisAddsUnassignedColumn(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})
	board, err := svc.GetBoard(BoardInput{
		WorkspaceID: "ws1", GroupBy: savedview.GroupByOwner,
		Owners: []Owner{{ID: "agent-1", Name: "Agent One"}},
	})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 2 {
		t.Fatalf("expected owner + unassigned = 2 columns, got %d", len(board.Columns))
	}
	last := board.Columns[len(board.Columns)-1]
	if last.ID != "__unassigned__" {
		t.Fatalf("expected trailing unassigned column, got %q", last.ID)
	}
}

func TestGetBoard_NoneAxisSingleColumn(t *testing.T) {
	svc := NewService(&fakeSearcher{entries: sampleEntries(), total: 2}, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})
	board, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByNone})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Columns) != 1 || board.Columns[0].ID != "__all__" {
		t.Fatalf("expected single __all__ column, got %+v", board.Columns)
	}
}

func TestGetBoard_UnsupportedGroupBy(t *testing.T) {
	svc := NewService(&fakeSearcher{}, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})
	_, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByCarteira})
	if !errors.Is(err, ErrUnsupportedGroupBy) {
		t.Fatalf("expected ErrUnsupportedGroupBy, got %v", err)
	}
}

func TestGetBoard_Unauthorized(t *testing.T) {
	svc := NewService(&fakeSearcher{}, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: false}, &fakeAssignments{})
	_, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByStage})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGetEntries_HydratesOwner(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	assignments := &fakeAssignments{byEntry: map[string]string{"e2": "agent-9"}}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true}, assignments)
	entries, total, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1"})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if got := ownerOf(entries, "e2"); got != "agent-9" {
		t.Fatalf("expected e2 owner agent-9, got %q", got)
	}
}

func lastCall(f *fakeSearcher) conversation.SearchByFilterInput {
	return f.calls[len(f.calls)-1]
}

func TestGetEntries_PlainMember_NotDepartmentRestricted_ScopedToSelf(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	auth := &fakeAuthorizer{allowed: true}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, auth, &fakeAssignments{})

	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1", UserID: "diovanna"}); err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if got := lastCall(searcher).AssignedUserID; got != "diovanna" {
		t.Fatalf("plain member must be scoped to self: AssignedUserID = %q, want %q (LEAK: sees all members' entries)", got, "diovanna")
	}
}

func TestGetBoard_PlainMember_EveryColumnScopedToSelf(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{stages: []*stage.Stage{{ID: "s1", Name: "novo"}, {ID: "s2", Name: "fechado"}}}
	auth := &fakeAuthorizer{allowed: true}
	svc := NewService(searcher, stages, &fakeLabels{}, auth, &fakeAssignments{})

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", UserID: "diovanna", GroupBy: savedview.GroupByStage}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(searcher.calls) == 0 {
		t.Fatal("expected column searches")
	}
	for i, c := range searcher.calls {
		if c.AssignedUserID != "diovanna" {
			t.Errorf("column %d not self-scoped: AssignedUserID = %q, want %q", i, c.AssignedUserID, "diovanna")
		}
	}
}

func TestGetEntries_DepartmentRestrictedMember_ScopedToSelfAndOverride(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	auth := &fakeAuthorizer{allowed: true, scope: conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dept-1"}}}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, auth, &fakeAssignments{})

	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1", UserID: "diovanna"}); err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	c := lastCall(searcher)
	if c.AssignedUserID != "diovanna" {
		t.Errorf("self-scope missing: AssignedUserID = %q", c.AssignedUserID)
	}
	if c.AssigneeOverrideUserID != "diovanna" {
		t.Errorf("department widener missing: AssigneeOverrideUserID = %q", c.AssigneeOverrideUserID)
	}
	if !c.RestrictDepartments {
		t.Error("expected RestrictDepartments=true")
	}
}

func TestGetEntries_Admin_SeesAll(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true}, &fakeAssignments{})
	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1", UserID: "boss", IsAdmin: true}); err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if got := lastCall(searcher).AssignedUserID; got != "" {
		t.Fatalf("admin must not be self-scoped: AssignedUserID = %q, want empty", got)
	}
}

func TestGetEntries_WorkspaceOwner_SeesAll(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true, owner: true}, &fakeAssignments{})
	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1", UserID: "owner"}); err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if got := lastCall(searcher).AssignedUserID; got != "" {
		t.Fatalf("owner must not be self-scoped: AssignedUserID = %q, want empty", got)
	}
}

func TestGetEntries_ViewOthersPermission_SeesAll(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, &fakeAuthorizer{allowed: true, viewOthers: true}, &fakeAssignments{})
	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1", UserID: "supervisor"}); err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if got := lastCall(searcher).AssignedUserID; got != "" {
		t.Fatalf("view_others member must not be self-scoped: AssignedUserID = %q, want empty", got)
	}
}
