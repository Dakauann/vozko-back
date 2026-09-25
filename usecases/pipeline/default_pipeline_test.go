package pipeline_usecase

import (
	"errors"
	"testing"

	"vozko/domain/pipeline"
	wd "vozko/domain/workspace/workspace_department"
)

type defaultRepo struct {
	pipeline.Repository

	byID map[string]*pipeline.Pipeline

	created  []*pipeline.Pipeline
	updated  []*pipeline.Pipeline
	promoted []promotion

	promoteErr error
	createErr  error
}

type promotion struct {
	workspaceID string
	objectType  string
	pipelineID  string
}

func newDefaultRepo(ps ...*pipeline.Pipeline) *defaultRepo {
	r := &defaultRepo{byID: map[string]*pipeline.Pipeline{}}
	for _, p := range ps {
		r.byID[p.ID] = p
	}
	return r
}

func (r *defaultRepo) GetByID(workspaceID, id string) (*pipeline.Pipeline, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, pipeline.ErrNotFound
	}
	copied := *p
	return &copied, nil
}

func (r *defaultRepo) Create(p *pipeline.Pipeline) error {
	if r.createErr != nil {
		return r.createErr
	}
	copied := *p
	r.byID[p.ID] = &copied
	r.created = append(r.created, &copied)
	return nil
}

func (r *defaultRepo) Update(p *pipeline.Pipeline) error {
	copied := *p
	r.byID[p.ID] = &copied
	r.updated = append(r.updated, &copied)
	return nil
}

func (r *defaultRepo) PromoteDefault(workspaceID, objectType, pipelineID string) error {
	if r.promoteErr != nil {
		return r.promoteErr
	}
	r.promoted = append(r.promoted, promotion{workspaceID, objectType, pipelineID})
	for id, p := range r.byID {
		if p.WorkspaceID != workspaceID || string(p.ObjectType) != objectType {
			continue
		}
		p.IsDefault = id == pipelineID
	}
	return nil
}

func (r *defaultRepo) ListByWorkspace(workspaceID, objectType string) ([]*pipeline.Pipeline, error) {
	var out []*pipeline.Pipeline
	for _, p := range r.byID {
		if p.WorkspaceID != workspaceID {
			continue
		}
		if objectType != "" && string(p.ObjectType) != objectType {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *defaultRepo) defaults(objectType pipeline.ObjectType) []string {
	var out []string
	for id, p := range r.byID {
		if p.IsDefault && p.ObjectType == objectType {
			out = append(out, id)
		}
	}
	return out
}

func convPipe(id string, isDefault bool) *pipeline.Pipeline {
	return &pipeline.Pipeline{
		ID: id, WorkspaceID: "ws", Name: id,
		ObjectType: pipeline.ObjectConversation, IsDefault: isDefault,
	}
}

func boolPtr(b bool) *bool { return &b }

func TestPromotingADefaultDemotesThePreviousOne(t *testing.T) {
	repo := newDefaultRepo(convPipe("old", true), convPipe("new", false))
	uc := NewUpdatePipelineUseCase(repo, departmentsOf{})

	if _, err := uc.Execute("ws", "new", pipeline.UpdatePipelineInput{
		IsDefault: boolPtr(true),
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := repo.defaults(pipeline.ObjectConversation)
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("defaults after promotion = %v, want exactly [new]", got)
	}

	if len(repo.promoted) != 1 {
		t.Fatalf("PromoteDefault called %d times, want 1", len(repo.promoted))
	}
	if repo.promoted[0].objectType != string(pipeline.ObjectConversation) {
		t.Errorf("promotion scoped to object type %q", repo.promoted[0].objectType)
	}
}

func TestPromotingDoesNotDemoteTheOtherObjectKind(t *testing.T) {
	sales := &pipeline.Pipeline{
		ID: "sales", WorkspaceID: "ws", Name: "Vendas",
		ObjectType: pipeline.ObjectOpportunity, IsDefault: true,
	}
	repo := newDefaultRepo(convPipe("old", true), convPipe("new", false), sales)
	uc := NewUpdatePipelineUseCase(repo, departmentsOf{})

	if _, err := uc.Execute("ws", "new", pipeline.UpdatePipelineInput{
		IsDefault: boolPtr(true),
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if opp := repo.defaults(pipeline.ObjectOpportunity); len(opp) != 1 || opp[0] != "sales" {
		t.Fatalf("opportunity defaults = %v, want [sales] untouched", opp)
	}
}

func TestClearingTheLastDefaultIsRefused(t *testing.T) {
	repo := newDefaultRepo(convPipe("only", true), convPipe("other", false))
	uc := NewUpdatePipelineUseCase(repo, departmentsOf{})

	_, err := uc.Execute("ws", "only", pipeline.UpdatePipelineInput{
		IsDefault: boolPtr(false),
	})
	if !errors.Is(err, pipeline.ErrDefaultRequired) {
		t.Fatalf("err = %v, want ErrDefaultRequired", err)
	}
	if got := repo.defaults(pipeline.ObjectConversation); len(got) != 1 {
		t.Fatalf("defaults = %v, want the default left intact", got)
	}
}

func TestClearingTheFlagOnANonDefaultIsFine(t *testing.T) {
	repo := newDefaultRepo(convPipe("theDefault", true), convPipe("other", false))
	uc := NewUpdatePipelineUseCase(repo, departmentsOf{})

	if _, err := uc.Execute("ws", "other", pipeline.UpdatePipelineInput{
		IsDefault: boolPtr(false),
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := repo.defaults(pipeline.ObjectConversation); len(got) != 1 || got[0] != "theDefault" {
		t.Fatalf("defaults = %v, want [theDefault]", got)
	}
}

func TestRenamingDoesNotTouchTheDefaultFlag(t *testing.T) {
	repo := newDefaultRepo(convPipe("a", true), convPipe("b", false))
	uc := NewUpdatePipelineUseCase(repo, departmentsOf{})

	newName := "renamed"
	if _, err := uc.Execute("ws", "b", pipeline.UpdatePipelineInput{Name: &newName}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.promoted) != 0 {
		t.Fatalf("a rename promoted a default: %v", repo.promoted)
	}
	if got := repo.defaults(pipeline.ObjectConversation); len(got) != 1 || got[0] != "a" {
		t.Fatalf("defaults = %v, want [a]", got)
	}
}

func TestCreatingADefaultDemotesThePreviousOne(t *testing.T) {
	repo := newDefaultRepo(convPipe("old", true))
	uc := NewCreatePipelineUseCase(repo, departmentsOf{})

	created, err := uc.Execute("ws", pipeline.CreatePipelineInput{
		Name: "novo", ObjectType: string(pipeline.ObjectConversation), IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if created == nil {
		t.Fatal("no funnel returned")
	}

	got := repo.defaults(pipeline.ObjectConversation)
	if len(got) != 1 || got[0] == "old" {
		t.Fatalf("defaults after creating a default = %v, want only the new funnel", got)
	}
}

func TestCreatingANonDefaultLeavesTheDefaultAlone(t *testing.T) {
	repo := newDefaultRepo(convPipe("old", true))
	uc := NewCreatePipelineUseCase(repo, departmentsOf{})

	if _, err := uc.Execute("ws", pipeline.CreatePipelineInput{
		Name: "novo", ObjectType: string(pipeline.ObjectConversation),
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.promoted) != 0 {
		t.Fatalf("a plain creation promoted a default: %v", repo.promoted)
	}
	if got := repo.defaults(pipeline.ObjectConversation); len(got) != 1 || got[0] != "old" {
		t.Fatalf("defaults = %v, want [old]", got)
	}
}

func TestFirstFunnelBecomesTheDefault(t *testing.T) {
	repo := newDefaultRepo()
	uc := NewCreatePipelineUseCase(repo, departmentsOf{})

	if _, err := uc.Execute("ws", pipeline.CreatePipelineInput{
		Name: "primeiro", ObjectType: string(pipeline.ObjectConversation), IsDefault: true,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := repo.defaults(pipeline.ObjectConversation); len(got) != 1 {
		t.Fatalf("defaults = %v, want exactly one", got)
	}
}

type departmentsOf []string

func (d departmentsOf) Execute(string) ([]wd.Department, error) {
	out := make([]wd.Department, 0, len(d))
	for _, id := range d {
		out = append(out, wd.Department{ID: id})
	}
	return out, nil
}

func TestCreatingAFunnelRefusesAForeignDepartment(t *testing.T) {
	repo := newDefaultRepo()
	uc := NewCreatePipelineUseCase(repo, departmentsOf{"d-mine"})
	if _, err := uc.Execute("ws", pipeline.CreatePipelineInput{
		Name: "novo", ObjectType: string(pipeline.ObjectConversation), DepartmentID: "d-other",
	}); err != pipeline.ErrDepartmentUnknown {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.Execute("ws", pipeline.CreatePipelineInput{
		Name: "novo", ObjectType: string(pipeline.ObjectConversation), DepartmentID: "d-mine",
	}); err != nil {
		t.Fatalf("own department: %v", err)
	}
}

func TestMovingAFunnelToAForeignDepartmentIsRefused(t *testing.T) {
	repo := newDefaultRepo(convPipe("a", true))
	other := "d-other"
	if _, err := NewUpdatePipelineUseCase(repo, departmentsOf{"d-mine"}).Execute("ws", "a", pipeline.UpdatePipelineInput{DepartmentID: &other}); err != pipeline.ErrDepartmentUnknown {
		t.Fatalf("err = %v", err)
	}
}
