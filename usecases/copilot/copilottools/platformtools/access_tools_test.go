package platformtools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type diagnoseStub struct {
	got  workspace.DiagnoseAccessInput
	out  *workspace.AccessDiagnosis
	err  error
	hits int
}

func (s *diagnoseStub) Execute(in workspace.DiagnoseAccessInput) (*workspace.AccessDiagnosis, error) {
	s.hits++
	s.got = in
	return s.out, s.err
}

func kanbanDiagnosis(allowedView bool) *workspace.AccessDiagnosis {
	board, _ := workspace.FeatureByKey("crm_board")
	grants := []workspace.PermissionEntry{{Resource: workspace.ResourceStages, Action: workspace.ActionRead}}
	if allowedView {
		grants = append(grants, workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: workspace.ActionRead})
	}
	return &workspace.AccessDiagnosis{
		Member:           &workspace.Member{UserID: "user-1", Username: "Ana", Role: workspace.RoleMember},
		FeatureDiagnosis: board.Diagnose(workspace.NewGrants(workspace.RoleMember, false, grants), workspace.MemberScope{WorkspaceUsesDepartments: true}),
	}
}

func inboxDiagnosis(allowed bool) *workspace.AccessDiagnosis {
	inbox, _ := workspace.FeatureByKey("inbox")
	var grants []workspace.PermissionEntry
	if allowed {
		grants = []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}
	}
	return &workspace.AccessDiagnosis{
		Member:           &workspace.Member{UserID: "user-1", Role: workspace.RoleMember},
		FeatureDiagnosis: inbox.Diagnose(workspace.NewGrants(workspace.RoleMember, false, grants), workspace.MemberScope{}),
	}
}

func usedByKeys(t *testing.T, res copilot.Result) map[string]map[string]interface{} {
	t.Helper()
	out := map[string]map[string]interface{}{}
	for _, u := range tooltest.DataOf(res)["used_by"].([]map[string]interface{}) {
		out[u["capability"].(string)] = u
	}
	return out
}

func TestExplainPermissionSaysWhereItIsUsed(t *testing.T) {
	res := NewExplainPermissionTool().Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"permission": "stages:assign"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	uses := usedByKeys(t, res)
	drag, ok := uses["crm_board.move_stage"]
	if !ok || drag["feature"] != "Kanban de conversas" || drag["location"] == "" {
		t.Fatalf("stages:assign must point at the kanban drag, got %+v", uses)
	}
	if tooltest.DataOf(res)["description"] == "" {
		t.Error("the permission description is missing")
	}
}

func TestExplainPermissionListsDependenciesAndRisks(t *testing.T) {
	res := NewExplainPermissionTool().Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"permission": "whatsapp_templates:send"})
	requires := tooltest.DataOf(res)["requires"].([]map[string]interface{})
	if len(requires) != 3 {
		t.Fatalf("sending a template depends on three permissions, got %+v", requires)
	}
	if risks := tooltest.DataOf(res)["risks"].([]string); len(risks) != 2 {
		t.Fatalf("sending a template carries two risks, got %+v", risks)
	}
}

func TestExplainPermissionAdmitsWhenAPermissionDoesNothing(t *testing.T) {
	res := NewExplainPermissionTool().Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"permission": "workflows:read_details"})
	if res.Status != copilot.StatusOK || len(tooltest.DataOf(res)["used_by"].([]map[string]interface{})) != 0 || tooltest.DataOf(res)["effect"] == nil {
		t.Fatalf("an inert permission must be reported as having no effect, got %+v", res.Data)
	}
}

func TestExplainPermissionRejectsUnknownPermissions(t *testing.T) {
	res := NewExplainPermissionTool().Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"permission": "stages:fly"})
	if res.Status != copilot.StatusError {
		t.Fatalf("unknown permissions must fail, got %+v", res)
	}
}

func TestDiagnoseAccessUsesTheSessionAndReportsWhatIsMissing(t *testing.T) {
	stub := &diagnoseStub{out: kanbanDiagnosis(true)}
	res := NewDiagnoseAccessTool(AccessDeps{Diagnose: stub}).Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"feature": "crm_board"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	if stub.got.ActorID != "user-1" || stub.got.WorkspaceID != "ws-1" || stub.got.MemberUserID != "" || stub.got.Feature != "crm_board" || stub.got.CallerRole != "user" {
		t.Fatalf("the diagnosis must run as the session user, got %+v", stub.got)
	}
	var move map[string]interface{}
	for _, c := range tooltest.DataOf(res)["capabilities"].([]map[string]interface{}) {
		if c["capability"] == "crm_board.move_stage" {
			move = c
		}
	}
	missing := move["missing"].([]map[string]interface{})
	if move["allowed"] != false || len(missing) != 1 || missing[0]["permission"] != "stages:assign" || missing[0]["description"] == "" {
		t.Fatalf("moving cards must name stages:assign as missing, got %+v", move)
	}
	if scope := tooltest.DataOf(res)["scope"].([]map[string]interface{}); len(scope) == 0 {
		t.Fatal("the department warning must reach the model")
	}
}

func TestDiagnoseAccessMapsRefusals(t *testing.T) {
	cases := map[error]copilot.Status{
		workspace.ErrInsufficientPermissions: copilot.StatusDenied,
		workspace.ErrUnauthorized:            copilot.StatusDenied,
		workspace.ErrUnknownFeature:          copilot.StatusError,
		workspace.ErrMemberNotFound:          copilot.StatusError,
		errors.New("db down"):                copilot.StatusError,
	}
	for err, want := range cases {
		res := NewDiagnoseAccessTool(AccessDeps{Diagnose: &diagnoseStub{err: err}}).Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"feature": "crm_board", "user_id": "7b1c2f9e-0a51-4f7e-9f3a-2d5b8c1e4a10"})
		if res.Status != want || res.Data != nil {
			t.Errorf("%v: got %+v", err, res)
		}
	}
}

func TestDiagnoseAccessRejectsMalformedArguments(t *testing.T) {
	stub := &diagnoseStub{out: kanbanDiagnosis(true)}
	tool := NewDiagnoseAccessTool(AccessDeps{Diagnose: stub})
	for _, args := range []map[string]interface{}{{}, {"feature": "crm_board", "user_id": "not-an-id"}, {"feature": "nope"}} {
		if res := tool.Execute(context.Background(), tooltest.AccessCtx, args); res.Status != copilot.StatusError {
			t.Errorf("%v must be refused, got %+v", args, res)
		}
	}
	if stub.hits != 0 {
		t.Fatal("malformed arguments must never reach the use case")
	}
}

func TestOpenScreenShowsACardOnlyForScreensTheUserCanOpen(t *testing.T) {
	stub := &diagnoseStub{out: inboxDiagnosis(true)}
	res := NewOpenScreenTool(AccessDeps{Diagnose: stub}).Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"screen": "live_chat"})
	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.Destination.Screen != workspace.ScreenLiveChat {
		t.Fatalf("got %+v", res)
	}
	if stub.got.Feature != "inbox" || stub.got.MemberUserID != "" {
		t.Fatalf("the check must be the user's own access to the screen's feature, got %+v", stub.got)
	}
}

func TestOpenScreenRefusesScreensTheUserCannotOpen(t *testing.T) {
	res := NewOpenScreenTool(AccessDeps{Diagnose: &diagnoseStub{out: inboxDiagnosis(false)}}).Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"screen": "live_chat"})
	if res.Status != copilot.StatusDenied || res.Card != nil {
		t.Fatalf("a denied screen must not produce a card, got %+v", res)
	}
	if res.Data == nil || tooltest.DataOf(res)["missing"] == nil {
		t.Fatalf("the model must learn what is missing, got %+v", res)
	}
}

func TestOpenScreenFailsClosed(t *testing.T) {
	failing := NewOpenScreenTool(AccessDeps{Diagnose: &diagnoseStub{err: errors.New("db down")}})
	if res := failing.Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"screen": "live_chat"}); res.Status != copilot.StatusError || res.Card != nil {
		t.Fatalf("a failed access check must not produce a card, got %+v", res)
	}
	tool := NewOpenScreenTool(AccessDeps{Diagnose: &diagnoseStub{out: inboxDiagnosis(true)}})
	for _, args := range []map[string]interface{}{{"screen": "nowhere"}, {"screen": "agent_detail"}, {"screen": "agent_detail", "id": "../x"}} {
		if res := tool.Execute(context.Background(), tooltest.AccessCtx, args); res.Status != copilot.StatusError || res.Card != nil {
			t.Errorf("%v must be refused, got %+v", args, res)
		}
	}
}

func TestAccessToolDefinitionsOfferTheWholeCatalog(t *testing.T) {
	features := NewDiagnoseAccessTool(AccessDeps{}).Definition().Parameters["feature"].Enum
	if len(features) != len(workspace.Features) {
		t.Errorf("feature enum has %d entries, catalog has %d", len(features), len(workspace.Features))
	}
	screens := NewOpenScreenTool(AccessDeps{}).Definition().Parameters["screen"].Enum
	if len(screens) != len(workspace.Screens()) {
		t.Errorf("screen enum has %d entries, registry has %d", len(screens), len(workspace.Screens()))
	}
}

type projectShelf map[string]*studio.Project

func (s projectShelf) Get(_ context.Context, workspaceID, id string) (*studio.Project, error) {
	if p, ok := s[id]; ok && p.WorkspaceID == workspaceID {
		return p, nil
	}
	return nil, studio.ErrProjectNotFound
}

func studioDiagnosis() *workspace.AccessDiagnosis {
	feature, _ := workspace.FeatureByKey("studio")
	grants := []workspace.PermissionEntry{{Resource: workspace.ResourceMedia, Action: workspace.ActionRead}, {Resource: workspace.ResourceMedia, Action: workspace.ActionCreate}}
	return &workspace.AccessDiagnosis{
		Member:           &workspace.Member{UserID: "user-1", Role: workspace.RoleMember},
		FeatureDiagnosis: feature.Diagnose(workspace.NewGrants(workspace.RoleMember, false, grants), workspace.MemberScope{}),
	}
}

const existingProject = "7c1d2e3f-4a5b-4c6d-8e7f-90a1b2c3d4e5"

func studioOpener() copilot.Tool {
	shelf := projectShelf{existingProject: {ID: existingProject, WorkspaceID: "ws-1", Kind: studio.KindImage}}
	return NewOpenScreenTool(AccessDeps{Diagnose: &diagnoseStub{out: studioDiagnosis()}, Projects: shelf})
}

func TestOpenScreenSendsTheUserIntoAnExistingStudioProject(t *testing.T) {
	res := studioOpener().Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"screen": "studio_image", "id": existingProject})
	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.Destination.Params["projectId"] != existingProject {
		t.Fatalf("got %+v", res)
	}
}

func TestOpenScreenNeverLinksToAStudioProjectThatDoesNotExist(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"no id, the call that failed in the real chat": {"screen": "studio_image"},
		"a media id instead of a project id":           {"screen": "studio_image", "id": "0d6f3a2e-1b4c-4d8e-9f10-2a3b4c5d6e7f"},
		"a project of the other kind":                  {"screen": "studio_video", "id": existingProject},
	}
	for name, args := range cases {
		res := studioOpener().Execute(context.Background(), tooltest.AccessCtx, args)
		if res.Status != copilot.StatusError || res.Card != nil {
			t.Errorf("%s: a broken link must never reach the user, got %+v", name, res)
			continue
		}
		if !strings.Contains(res.Message, "new_studio_project") || !strings.Contains(res.Message, "projectId") {
			t.Errorf("%s: the refusal must name the id it wants and the tool that makes one, got %q", name, res.Message)
		}
	}
}

func TestOpenScreenNamesTheIdAScreenNeeds(t *testing.T) {
	res := NewOpenScreenTool(AccessDeps{Diagnose: &diagnoseStub{out: inboxDiagnosis(true)}}).Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"screen": "agent_detail"})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "agentId") {
		t.Fatalf("got %+v", res)
	}
}

func TestOpenScreenFailsClosedWithoutTheProjectLookup(t *testing.T) {
	tool := NewOpenScreenTool(AccessDeps{Diagnose: &diagnoseStub{out: studioDiagnosis()}})
	if res := tool.Execute(context.Background(), tooltest.AccessCtx, map[string]interface{}{"screen": "studio_image", "id": existingProject}); res.Status != copilot.StatusError || res.Card != nil {
		t.Fatalf("got %+v", res)
	}
}
