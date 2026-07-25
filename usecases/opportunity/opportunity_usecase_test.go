package opportunity_usecase

import (
	"errors"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
)

// --- in-memory fakes implementing the domain ports ---

var errFakeNotFound = errors.New("fake: not found")

type fakeOppRepo struct {
	store map[string]*opportunity.Opportunity
}

func newFakeOppRepo() *fakeOppRepo { return &fakeOppRepo{store: map[string]*opportunity.Opportunity{}} }

func (r *fakeOppRepo) Create(o *opportunity.Opportunity) error {
	cp := *o
	r.store[o.ID] = &cp
	return nil
}
func (r *fakeOppRepo) Update(o *opportunity.Opportunity) error {
	if _, ok := r.store[o.ID]; !ok {
		return errFakeNotFound
	}
	cp := *o
	r.store[o.ID] = &cp
	return nil
}
func (r *fakeOppRepo) Delete(workspaceID, id string) error {
	if _, ok := r.store[id]; !ok {
		return errFakeNotFound
	}
	delete(r.store, id)
	return nil
}
func (r *fakeOppRepo) GetByID(workspaceID, id string) (*opportunity.Opportunity, error) {
	o, ok := r.store[id]
	if !ok || o.WorkspaceID != workspaceID {
		return nil, errFakeNotFound
	}
	cp := *o
	return &cp, nil
}
func (r *fakeOppRepo) ListByPipeline(workspaceID, pipelineID string) ([]*opportunity.Opportunity, error) {
	var out []*opportunity.Opportunity
	for _, o := range r.store {
		if o.WorkspaceID == workspaceID && o.PipelineID == pipelineID {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeOppRepo) ListByPipelineScoped(workspaceID, pipelineID string, _ []string, _ bool, _ string) ([]*opportunity.Opportunity, error) {
	return r.ListByPipeline(workspaceID, pipelineID)
}

// SearchByFilter / SumValueByFilter back the deal board and list. The fake keeps a
// minimal workspace-scoped in-memory version (it ignores the compiled predicate,
// which is exercised at the repository/compiler layer) so the usecase satisfies
// the extended opportunity.Repository port here.
func (r *fakeOppRepo) SearchByFilter(input opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error) {
	var out []*opportunity.Opportunity
	for _, o := range r.store {
		if o.WorkspaceID == input.WorkspaceID {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeOppRepo) SumValueByFilter(input opportunity.SearchByFilterInput) (int64, error) {
	var sum int64
	for _, o := range r.store {
		if o.WorkspaceID == input.WorkspaceID {
			sum += o.ValueCents
		}
	}
	return sum, nil
}

type fakeFieldRepo struct {
	defs []*customfield.Definition
}

func (r *fakeFieldRepo) Create(d *customfield.Definition) error { return nil }
func (r *fakeFieldRepo) Update(d *customfield.Definition) error { return nil }
func (r *fakeFieldRepo) Delete(workspaceID, id string) error    { return nil }
func (r *fakeFieldRepo) GetByID(workspaceID, id string) (*customfield.Definition, error) {
	return nil, errFakeNotFound
}
func (r *fakeFieldRepo) ListByObject(workspaceID, objectType string) ([]*customfield.Definition, error) {
	return r.defs, nil
}

func fieldsWith() *fakeFieldRepo {
	return &fakeFieldRepo{defs: []*customfield.Definition{
		{WorkspaceID: "ws1", ObjectType: "opportunity", Key: "segmento", Label: "Segmento",
			Type: customfield.TypeSelect, Options: []string{"enterprise", "smb"}, Required: true},
		{WorkspaceID: "ws1", ObjectType: "opportunity", Key: "score", Label: "Score",
			Type: customfield.TypeNumber},
	}}
}

func baseCreate() CreateInput {
	return CreateInput{
		PipelineID:   "pipe1",
		StageID:      "stage1",
		Title:        "Deal",
		ValueCents:   490000,
		CustomFields: map[string]any{"segmento": "enterprise", "score": float64(87)},
	}
}

func TestCreate_CustomFieldValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*CreateInput)
		wantErr error // nil = success
	}{
		{"valid", func(in *CreateInput) {}, nil},
		{"unknown key", func(in *CreateInput) { in.CustomFields["foo"] = "bar" }, ErrUnknownCustomField},
		{"bad select option", func(in *CreateInput) { in.CustomFields["segmento"] = "startup" }, customfield.ErrValueNotInOptions},
		{"wrong type", func(in *CreateInput) { in.CustomFields["score"] = "not-a-number-because-abc" }, customfield.ErrValueType},
		{"missing required", func(in *CreateInput) { delete(in.CustomFields, "segmento") }, customfield.ErrValueRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(newFakeOppRepo(), nil, fieldsWith())
			in := baseCreate()
			tc.mutate(&in)
			_, err := svc.Create("ws1", in)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCreate_SeedsOwnerFromConversationAssignee(t *testing.T) {
	svc := NewService(newFakeOppRepo(), nil, fieldsWith())
	in := baseCreate()
	in.ConversationAssigneeID = "agent-42"
	o, err := svc.Create("ws1", in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if o.OwnerID != "agent-42" {
		t.Fatalf("owner should be seeded from assignee, got %q", o.OwnerID)
	}

	// An explicit owner wins over the assignee seed.
	in2 := baseCreate()
	in2.OwnerID = "owner-1"
	in2.ConversationAssigneeID = "agent-42"
	o2, err := svc.Create("ws1", in2)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if o2.OwnerID != "owner-1" {
		t.Fatalf("explicit owner should win, got %q", o2.OwnerID)
	}
}

func TestMoveStage_LostReasonEnforced(t *testing.T) {
	repo := newFakeOppRepo()
	svc := NewService(repo, nil, fieldsWith())

	created, err := svc.Create("ws1", baseCreate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Moving to lost WITHOUT a reason is rejected by the entity rule.
	_, err = svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage-lost", Status: opportunity.StatusLost})
	if !errors.Is(err, opportunity.ErrLostReasonMissing) {
		t.Fatalf("expected ErrLostReasonMissing, got %v", err)
	}

	// Moving to lost WITH a reason succeeds and stamps a close date.
	moved, err := svc.MoveStage("ws1", created.ID, MoveStageInput{
		StageID: "stage-lost", Status: opportunity.StatusLost, LostReasonID: "reason-price",
	})
	if err != nil {
		t.Fatalf("MoveStage: %v", err)
	}
	if moved.Status != opportunity.StatusLost || moved.LostReasonID != "reason-price" {
		t.Fatalf("unexpected moved state: %#v", moved)
	}
	if moved.CloseDate == nil {
		t.Fatal("closing a deal should stamp a close date")
	}
}

func TestMoveStage_WonStampsClose(t *testing.T) {
	repo := newFakeOppRepo()
	svc := NewService(repo, nil, fieldsWith())
	created, err := svc.Create("ws1", baseCreate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	moved, err := svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage-won", Status: opportunity.StatusWon})
	if err != nil {
		t.Fatalf("MoveStage won: %v", err)
	}
	if moved.Status != opportunity.StatusWon || moved.CloseDate == nil {
		t.Fatalf("won deal should be closed with a close date: %#v", moved)
	}
}
