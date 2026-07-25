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

// --- in-memory fakes implementing the crmboard ports ---

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
	// Return a fresh copy per call, exactly like the real repo, so hydrating one
	// column's entries never aliases another column's slice.
	out := make([]conversation.EntryWithLastMessage, len(f.entries))
	copy(out, f.entries)
	return out, f.total, nil
}

type fakeStages struct {
	stages     []*stage.Stage            // the workspace default (ListByCampaign)
	byPipeline map[string][]*stage.Stage // a specific pipeline's stages (ListByPipeline)
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
	owner      bool // IsWorkspaceOwnerOrAdmin
	viewOthers bool // holds conversations:view_others
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

// Regression: selecting a NON-default pipeline must render THAT pipeline's stages,
// never fall back to the default's. A stage-group campaign gets its own pipeline;
// before the fix, pipelineStages filtered the default's stages by the new id (which
// never matched) and silently rendered the whole default board ("shows all").
func TestGetBoard_NonDefaultPipeline_RendersOwnStages_NotDefault(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{
		stages: []*stage.Stage{ // the workspace DEFAULT pipeline
			{ID: "d1", Name: "recebido", PipelineID: "default"},
			{ID: "d2", Name: "em atendimento", PipelineID: "default"},
			{ID: "d3", Name: "finalizado", PipelineID: "default"},
		},
		byPipeline: map[string][]*stage.Stage{ // the campaign's OWN funnel
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
			t.Errorf("column %d = %q, want %q — the board leaked the DEFAULT pipeline's stages", i, board.Columns[i].ID, want)
		}
	}
}

// The default (empty pipeline id) still resolves via ListByCampaign.
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

// filterHasStageScope reports whether f contains an IN predicate on the stage field
// whose values are EXACTLY the given pipeline stage ids (the whole-board scope), not a
// single-stage column predicate.
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

// Industry standard (HubSpot Board mode, Salesforce Kanban-over-list, Pipedrive/Kommo):
// a board is scoped to ONE pipeline, and switching the swimlane axis re-groups that same
// scoped set — it never widens to the whole workspace. On the LABEL axis (columns are
// workspace-global) the board must therefore inject a "stage IN <pipeline stages>" scope
// into every column search, else the funnel is ignored and all conversations leak in.
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
			t.Errorf("label column %d searched WITHOUT the pipeline stage scope — the funnel is ignored and the whole workspace leaks: %+v", i, call.Filter)
		}
	}
}

// "Todos os funis": an empty PipelineID on a global axis (owner/label) must NOT scope —
// the user is deliberately viewing every responsável/etiqueta across all pipelines
// (HubSpot's "All Pipelines"). No stage predicate may be injected.
func TestGetBoard_LabelAxis_AllFunnels_NotScoped(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	stages := &fakeStages{stages: []*stage.Stage{{ID: "d1"}, {ID: "d2"}}} // the workspace default
	labels := &fakeLabels{labels: []*label.Label{{ID: "l1", Name: "VIP"}}}
	svc := NewService(searcher, stages, labels, &fakeAuthorizer{allowed: true}, &fakeAssignments{})

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", GroupBy: savedview.GroupByLabel, PipelineID: ""}); err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for i, call := range searcher.calls {
		for _, g := range call.Filter.Groups {
			for _, p := range g.Predicates {
				if p.Field == crmfilter.FieldStage {
					t.Errorf("label column %d was scoped by a stage predicate under 'Todos os funis' — the global view must span all pipelines: %+v", i, p)
				}
			}
		}
	}
}

// Same rule on the OWNER axis (including the trailing "unassigned" swimlane).
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

// The stage axis must NOT get the redundant all-stages scope: each column already narrows
// to a SINGLE stage, so a whole-board "stage IN <all>" predicate would be dead weight.
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

// --- Scoping / authorization: the board & list must apply the SAME self-scope as
// the inbox. Regression for the leak where a plain member (diovanna, Sao Miguel)
// saw every member's entries on the table and kanban while her inbox scoped
// correctly. A member sees only their own (or unassigned) entries unless they are
// admin, workspace owner, or hold conversations:view_others. ---

// lastCall returns the most recent SearchByFilterInput the searcher received.
func lastCall(f *fakeSearcher) conversation.SearchByFilterInput {
	return f.calls[len(f.calls)-1]
}

// THE BUG: a plain member who is NOT department-restricted (restrict=false, so the
// department clause is empty) must still be pinned to their own entries. Before the
// fix, AssignedUserID was never set and the flat list showed everyone.
func TestGetEntries_PlainMember_NotDepartmentRestricted_ScopedToSelf(t *testing.T) {
	searcher := &fakeSearcher{entries: sampleEntries(), total: 2}
	auth := &fakeAuthorizer{allowed: true} // restrict=false, owner=false, viewOthers=false
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, auth, &fakeAssignments{})

	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws1", UserID: "diovanna"}); err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if got := lastCall(searcher).AssignedUserID; got != "diovanna" {
		t.Fatalf("plain member must be scoped to self: AssignedUserID = %q, want %q (LEAK: sees all members' entries)", got, "diovanna")
	}
}

// Every board column query must carry the same self-scope, not just the flat list.
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

// Department-restricted plain member: self-scope AND the department widener both
// apply, exactly like the inbox (own conversations stay visible out-of-department).
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

// Admin sees everyone: no self-scope.
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

// Workspace owner sees everyone: no self-scope.
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

// A member holding conversations:view_others sees everyone: no self-scope.
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
