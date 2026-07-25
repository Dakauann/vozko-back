package crmbulk_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/label"
	"vozko/domain/stage"
)

// --- mutation-port mocks -----------------------------------------------------

type mockStageAssigner struct {
	calls   []stage.AssignEntryStageInput
	failIDs map[string]error // entryID -> error
}

func (m *mockStageAssigner) Execute(workspaceID string, input stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	m.calls = append(m.calls, input)
	if err, ok := m.failIDs[input.EntryID]; ok {
		return nil, err
	}
	return &stage.EntryStage{EntryID: input.EntryID, StageID: input.StageID}, nil
}

type mockLabelAssigner struct {
	calls   []label.AssignEntryLabelInput
	failIDs map[string]error
}

func (m *mockLabelAssigner) Execute(workspaceID string, input label.AssignEntryLabelInput) (*label.EntryLabel, error) {
	m.calls = append(m.calls, input)
	if err, ok := m.failIDs[input.EntryID]; ok {
		return nil, err
	}
	return &label.EntryLabel{EntryID: input.EntryID, LabelID: input.LabelID}, nil
}

type removeCall struct{ labelID, entryID, entryType string }

type mockLabelRemover struct {
	calls   []removeCall
	failIDs map[string]error
}

func (m *mockLabelRemover) Execute(workspaceID, labelID, entryID, entryType string) error {
	m.calls = append(m.calls, removeCall{labelID, entryID, entryType})
	if err, ok := m.failIDs[entryID]; ok {
		return err
	}
	return nil
}

type reassignCall struct{ entryID, entryType, businessPhoneID, workspaceID, userID string }

type mockEntryAssigner struct {
	calls   []reassignCall
	failIDs map[string]error
}

func (m *mockEntryAssigner) Reassign(entryID, entryType, businessPhoneID, workspaceID, userID string) error {
	m.calls = append(m.calls, reassignCall{entryID, entryType, businessPhoneID, workspaceID, userID})
	if err, ok := m.failIDs[entryID]; ok {
		return err
	}
	return nil
}

// --- authorizer + broadcaster mocks ------------------------------------------

type mockAuthorizer struct {
	allowPerm  map[string]bool // "resource:action" -> allowed
	denyEntry  map[string]bool // entryID -> true means CanAccessEntry returns false
	permCalls  []string        // resources:actions checked
	entryCalls []string        // entryIDs checked
}

func (m *mockAuthorizer) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	m.permCalls = append(m.permCalls, resource+":"+action)
	if isSystemAdmin {
		return true
	}
	return m.allowPerm[resource+":"+action]
}

func (m *mockAuthorizer) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	m.entryCalls = append(m.entryCalls, entryID)
	if isAdmin {
		return true
	}
	return !m.denyEntry[entryID]
}

// allowAll grants every action permission and every entry — the baseline a
// legitimate, in-scope actor sees; individual tests tighten it.
func allowAll() *mockAuthorizer {
	return &mockAuthorizer{allowPerm: map[string]bool{
		"stages:assign":        true,
		"conversations:assign": true,
		"labels:assign":        true,
	}}
}

type mockBroadcaster struct {
	stage, label, entry []string // entryIDs broadcast per channel
}

func (m *mockBroadcaster) BroadcastStageUpdate(workspaceID, entryID, entryType string) {
	m.stage = append(m.stage, entryID)
}
func (m *mockBroadcaster) BroadcastLabelUpdate(workspaceID, entryID, entryType string) {
	m.label = append(m.label, entryID)
}
func (m *mockBroadcaster) BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message) {
	m.entry = append(m.entry, entryID)
}

func targets(ids ...string) []EntryRef {
	out := make([]EntryRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, EntryRef{EntryID: id, EntryType: "whatsapp"})
	}
	return out
}

// --- happy paths (fan-out + correct broadcast) -------------------------------

func TestBulkApply_MoveStage_FansOutAndBroadcastsStage(t *testing.T) {
	sa := &mockStageAssigner{}
	bc := &mockBroadcaster{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), bc)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "actor-1", Action: ActionMoveStage,
		Value: "stage-9", Targets: targets("e1", "e2", "e3"),
	})

	if res.Succeeded != 3 || len(res.Failed) != 0 || res.Forbidden {
		t.Fatalf("expected 3/0/notForbidden, got %+v", res)
	}
	if len(sa.calls) != 3 {
		t.Fatalf("expected 3 fan-out calls, got %d", len(sa.calls))
	}
	for _, c := range sa.calls {
		if c.StageID != "stage-9" {
			t.Errorf("StageID not forwarded: %q", c.StageID)
		}
	}
	if got := bc.stage; len(got) != 3 {
		t.Errorf("expected a stage broadcast per success, got %v", got)
	}
}

func TestBulkApply_Assign_ForwardsUserAndBroadcastsEntry(t *testing.T) {
	ea := &mockEntryAssigner{}
	bc := &mockBroadcaster{}
	svc := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, ea, allowAll(), bc)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-7", ActorID: "a", Action: ActionAssign, Value: "user-42",
		Targets: targets("e1", "e2"),
	})

	if res.Succeeded != 2 || len(res.Failed) != 0 {
		t.Fatalf("expected 2/0, got %+v", res)
	}
	if ea.calls[0].userID != "user-42" || ea.calls[0].workspaceID != "ws-7" {
		t.Errorf("reassign did not forward user/workspace: %+v", ea.calls[0])
	}
	if len(bc.entry) != 2 {
		t.Errorf("assign must broadcast an entry update per success, got %v", bc.entry)
	}
}

func TestBulkApply_Labels_RouteAndBroadcast(t *testing.T) {
	la, lr, bc := &mockLabelAssigner{}, &mockLabelRemover{}, &mockBroadcaster{}
	svc := NewService(&mockStageAssigner{}, la, lr, &mockEntryAssigner{}, allowAll(), bc)

	add := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionAddLabel, Value: "lbl-1", Targets: targets("e1"),
	})
	if add.Succeeded != 1 || len(la.calls) != 1 || la.calls[0].LabelID != "lbl-1" {
		t.Fatalf("add_label routing wrong: %+v %+v", add, la.calls)
	}
	rem := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionRemoveLabel, Value: "lbl-2", Targets: targets("e2"),
	})
	if rem.Succeeded != 1 || len(lr.calls) != 1 || lr.calls[0].labelID != "lbl-2" {
		t.Fatalf("remove_label routing wrong: %+v %+v", rem, lr.calls)
	}
	if len(bc.label) != 2 {
		t.Errorf("both label ops must broadcast a label update, got %v", bc.label)
	}
}

// --- RBAC: per-action permission is enforced ---------------------------------

func TestBulkApply_RBAC_DeniedAction_TouchesNothing(t *testing.T) {
	// Actor holds labels:assign but NOT stages:assign — a move_stage bulk must be a
	// hard 403 and never reach the stage port, mirroring the single-entry route.
	authz := &mockAuthorizer{allowPerm: map[string]bool{"labels:assign": true}}
	sa, bc := &mockStageAssigner{}, &mockBroadcaster{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, authz, bc)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "s", Targets: targets("e1", "e2"),
	})

	if !res.Forbidden || res.Succeeded != 0 || len(res.Failed) != 0 {
		t.Fatalf("expected Forbidden with nothing touched, got %+v", res)
	}
	if len(sa.calls) != 0 || len(bc.stage) != 0 || len(authz.entryCalls) != 0 {
		t.Errorf("denied action must not mutate/broadcast/scope-check: calls=%d bc=%d scope=%d",
			len(sa.calls), len(bc.stage), len(authz.entryCalls))
	}
	if len(authz.permCalls) != 1 || authz.permCalls[0] != "stages:assign" {
		t.Errorf("expected the stages:assign permission to be the one checked, got %v", authz.permCalls)
	}
}

func TestActionPermission_MapsEachActionToItsResource(t *testing.T) {
	cases := map[string]string{
		ActionMoveStage:   "stages:assign",
		ActionAssign:      "conversations:assign",
		ActionAddLabel:    "labels:assign",
		ActionRemoveLabel: "labels:assign",
	}
	for action, want := range cases {
		r, a, ok := actionPermission(action)
		if !ok || r+":"+a != want {
			t.Errorf("actionPermission(%q) = %q:%q ok=%v, want %q", action, r, a, ok, want)
		}
	}
	if _, _, ok := actionPermission("nope"); ok {
		t.Error("unknown action must report ok=false")
	}
}

// --- ownership: cross-workspace + department scope ---------------------------

func TestBulkApply_ScopeGate_RejectsOutOfScopeEntries(t *testing.T) {
	// e2 is out of scope (another workspace OR outside the actor's department) —
	// CanAccessEntry returns false. It must fail with ErrForbiddenEntry, never be
	// mutated or broadcast, while the in-scope e1/e3 still succeed.
	authz := allowAll()
	authz.denyEntry = map[string]bool{"e2": true}
	sa, bc := &mockStageAssigner{}, &mockBroadcaster{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, authz, bc)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "s", Targets: targets("e1", "e2", "e3"),
	})

	if res.Succeeded != 2 || len(res.Failed) != 1 {
		t.Fatalf("expected 2 ok / 1 forbidden, got %+v", res)
	}
	if res.Failed[0].EntryID != "e2" || res.Failed[0].Error != ErrForbiddenEntry.Error() {
		t.Errorf("out-of-scope target not reported correctly: %+v", res.Failed[0])
	}
	for _, c := range sa.calls {
		if c.EntryID == "e2" {
			t.Error("out-of-scope entry must NOT be mutated")
		}
	}
	for _, id := range bc.stage {
		if id == "e2" {
			t.Error("out-of-scope entry must NOT be broadcast")
		}
	}
}

func TestBulkApply_Admin_BypassesPermissionAndScope(t *testing.T) {
	// Admin: no explicit permission grant, and every entry marked out-of-scope, yet
	// isAdmin short-circuits both gates (matches the real authorizer).
	authz := &mockAuthorizer{denyEntry: map[string]bool{"e1": true, "e2": true}}
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, authz, &mockBroadcaster{})

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "admin", IsAdmin: true, Action: ActionMoveStage,
		Value: "s", Targets: targets("e1", "e2"),
	})

	if res.Forbidden || res.Succeeded != 2 || len(res.Failed) != 0 {
		t.Fatalf("admin should bypass both gates, got %+v", res)
	}
	if len(sa.calls) != 2 {
		t.Errorf("admin bulk should mutate all targets, got %d", len(sa.calls))
	}
}

// --- failure handling + fail-safe --------------------------------------------

func TestBulkApply_MutationFailure_NoBroadcastForThatTarget(t *testing.T) {
	boom := errors.New("stage not found")
	sa := &mockStageAssigner{failIDs: map[string]error{"e2": boom}}
	bc := &mockBroadcaster{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), bc)

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "s", Targets: targets("e1", "e2", "e3"),
	})

	if res.Succeeded != 2 || len(res.Failed) != 1 || res.Failed[0].EntryID != "e2" {
		t.Fatalf("expected e2 failed, rest ok: %+v", res)
	}
	if len(sa.calls) != 3 {
		t.Errorf("a failing target must not abort the rest, got %d attempts", len(sa.calls))
	}
	for _, id := range bc.stage {
		if id == "e2" {
			t.Error("a failed mutation must NOT broadcast")
		}
	}
	if len(bc.stage) != 2 {
		t.Errorf("expected exactly 2 broadcasts (the successes), got %v", bc.stage)
	}
}

func TestBulkApply_UnknownAction_IsForbiddenAndTouchesNothing(t *testing.T) {
	sa, la, lr, ea := &mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}
	svc := NewService(sa, la, lr, ea, allowAll(), &mockBroadcaster{})

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: "detonate", Value: "x", Targets: targets("e1", "e2"),
	})

	if !res.Forbidden || res.Succeeded != 0 {
		t.Fatalf("unknown action must be Forbidden, got %+v", res)
	}
	if len(sa.calls)+len(la.calls)+len(lr.calls)+len(ea.calls) != 0 {
		t.Error("unknown action must not touch any single-entry port")
	}
}

func TestBulkApply_NilAuthorizer_FailsClosed(t *testing.T) {
	// Defense in depth: without an authorizer the service must deny, never mutate.
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, nil, &mockBroadcaster{})

	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "s", Targets: targets("e1"),
	})

	if !res.Forbidden || len(sa.calls) != 0 {
		t.Fatalf("nil authorizer must fail closed, got %+v (calls=%d)", res, len(sa.calls))
	}
}

func TestBulkApply_NoTargets_IsNoOp(t *testing.T) {
	svc := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), &mockBroadcaster{})
	res := svc.BulkApply(context.Background(), BulkInput{WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "s"})
	if res.Forbidden || res.Succeeded != 0 || len(res.Failed) != 0 {
		t.Fatalf("expected empty non-forbidden result, got %+v", res)
	}
}

// applyOne is only reached for a known action (BulkApply gates the rest), but its
// default is a fail-safe if ever called directly — exercise it to lock the contract.
func TestApplyOne_UnknownAction_FailSafe(t *testing.T) {
	svc := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), nil)
	err := svc.applyOne(context.Background(), BulkInput{Action: "bogus"}, EntryRef{EntryID: "e1", EntryType: "whatsapp"})
	if !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("expected ErrUnknownAction, got %v", err)
	}
}

func TestBulkApply_NilBroadcaster_StillSucceeds(t *testing.T) {
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), nil)
	res := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "s", Targets: targets("e1"),
	})
	if res.Succeeded != 1 || len(sa.calls) != 1 {
		t.Fatalf("mutation must succeed even without a broadcaster, got %+v", res)
	}
}
