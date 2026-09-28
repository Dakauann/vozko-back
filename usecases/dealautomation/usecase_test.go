package dealautomation_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	"vozko/domain/stage"
	"vozko/domain/workspace"
)

type memoryRepo struct {
	rows map[string]dealautomation.Setting
	err  error
}

func key(ws string, c dealautomation.Channel, id string) string {
	return ws + "|" + string(c.EntryType) + "|" + string(c.Kind) + "|" + id
}

func (r *memoryRepo) Find(ws string, c dealautomation.Channel, id string) (*dealautomation.Setting, error) {
	if r.err != nil {
		return nil, r.err
	}
	if s, ok := r.rows[key(ws, c, id)]; ok {
		return &s, nil
	}
	return nil, nil
}

func (r *memoryRepo) Save(s dealautomation.Setting) error {
	r.rows[key(s.WorkspaceID, s.Channel, s.ContainerID)] = s
	return nil
}

func (r *memoryRepo) Delete(ws string, c dealautomation.Channel, id string) error {
	delete(r.rows, key(ws, c, id))
	return nil
}

type accessStub struct{ granted map[string]bool }

func (a accessStub) Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error {
	if a.granted[userID+"|"+string(resource)+":"+string(action)] {
		return nil
	}
	return workspace.ErrInsufficientPermissions
}

type funnelsStub struct{}

func (funnelsStub) PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	if pipelineID != "deals" {
		return nil, errors.New("not a deal funnel")
	}
	return []*stage.Stage{{ID: "s1"}}, nil
}

var instagram = dealautomation.Channel{EntryType: shared.EntryTypeInstagram, Kind: conversation.ContainerKindAccount}

func fixture() (*UseCase, *memoryRepo) {
	repo := &memoryRepo{rows: map[string]dealautomation.Setting{}}
	access := accessStub{granted: map[string]bool{
		"editor|instagram_accounts:read":   true,
		"editor|instagram_accounts:update": true,
		"viewer|instagram_accounts:read":   true,
	}}
	uc := New(repo, access, funnelsStub{})
	uc.now = func() time.Time { return time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC) }
	return uc, repo
}

func TestEditorTurnsAutoDealsOnAndOff(t *testing.T) {
	uc, repo := fixture()
	editor := shared.Person{UserID: "editor"}
	saved, err := uc.Set(editor, "ws", instagram, "acc-1", "deals")
	if err != nil || !saved.Enabled() || saved.UpdatedBy != "editor" {
		t.Fatalf("set = %+v, %v", saved, err)
	}
	if pipeline, _ := uc.PipelineFor("ws", instagram, "acc-1"); pipeline != "deals" {
		t.Fatalf("analysis sees %q", pipeline)
	}
	off, err := uc.Set(editor, "ws", instagram, "acc-1", " ")
	if err != nil || off.Enabled() || len(repo.rows) != 0 {
		t.Fatalf("turning off = %+v, %v, rows %d", off, err, len(repo.rows))
	}
}

func TestReadingNeedsViewAndSavingNeedsEdit(t *testing.T) {
	uc, _ := fixture()
	viewer := shared.Person{UserID: "viewer"}
	if got, err := uc.Get(viewer, "ws", instagram, "acc-1"); err != nil || got.Enabled() {
		t.Fatalf("viewer read = %+v, %v", got, err)
	}
	if _, err := uc.Set(viewer, "ws", instagram, "acc-1", "deals"); !errors.Is(err, workspace.ErrInsufficientPermissions) {
		t.Fatalf("viewer save error = %v", err)
	}
	if _, err := uc.Get(shared.Person{UserID: "stranger"}, "ws", instagram, "acc-1"); err == nil {
		t.Fatal("a person without the channel permission cannot read it")
	}
	if _, err := uc.Set(shared.Person{}, "ws", instagram, "acc-1", "deals"); !errors.Is(err, workspace.ErrUnauthorized) {
		t.Fatalf("anonymous save error = %v", err)
	}
}

func TestOnlyADealFunnelCanBeChosen(t *testing.T) {
	uc, repo := fixture()
	if _, err := uc.Set(shared.Person{UserID: "editor"}, "ws", instagram, "acc-1", "conversations"); err == nil || len(repo.rows) != 0 {
		t.Fatalf("a non-deal funnel was accepted: %v", err)
	}
}

func TestUnsupportedChannelsAreRefused(t *testing.T) {
	uc, _ := fixture()
	voice := dealautomation.Channel{EntryType: "voice"}
	if _, err := uc.Set(shared.Person{SystemAdmin: true}, "ws", voice, "x", "deals"); !errors.Is(err, dealautomation.ErrUnsupportedChannel) {
		t.Fatalf("error = %v", err)
	}
	if pipeline, err := uc.PipelineFor("ws", voice, "x"); err != nil || pipeline != "" {
		t.Fatalf("analysis on an unsupported channel = %q, %v", pipeline, err)
	}
}

func TestTheAnalysisSeesFailuresInsteadOfAnEmptySetting(t *testing.T) {
	uc, repo := fixture()
	repo.err = errors.New("db down")
	if _, err := uc.PipelineFor("ws", instagram, "acc-1"); err == nil {
		t.Fatal("a lookup failure must surface")
	}
}

func TestSettingsAreScopedToTheWorkspace(t *testing.T) {
	uc, _ := fixture()
	_, _ = uc.Set(shared.Person{UserID: "editor"}, "ws", instagram, "acc-1", "deals")
	if pipeline, _ := uc.PipelineFor("other-ws", instagram, "acc-1"); pipeline != "" {
		t.Fatalf("another workspace sees %q", pipeline)
	}
}
