package opportunity_usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
)

var errFakeBoom = errors.New("fake: boom")

type fakeOppRepo struct {
	store      map[string]*opportunity.Opportunity
	links      []opportunity.ConversationLink
	events     []opportunity.Event
	locks      int
	getErr     error
	createErrs int
}

func newFakeOppRepo() *fakeOppRepo { return &fakeOppRepo{store: map[string]*opportunity.Opportunity{}} }

func (r *fakeOppRepo) Create(o *opportunity.Opportunity, links []opportunity.ConversationLink, events []opportunity.Event) error {
	cp := *o
	r.store[o.ID] = &cp
	r.links = append(r.links, links...)
	r.events = append(r.events, events...)
	return nil
}

func (r *fakeOppRepo) Update(o *opportunity.Opportunity, events []opportunity.Event) error {
	if _, ok := r.store[o.ID]; !ok {
		return opportunity.ErrNotFound
	}
	cp := *o
	r.store[o.ID] = &cp
	r.events = append(r.events, events...)
	return nil
}

func (r *fakeOppRepo) Link(link opportunity.ConversationLink, events []opportunity.Event) error {
	r.links = append(r.links, link)
	r.events = append(r.events, events...)
	return nil
}

func (r *fakeOppRepo) OpenForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error) {
	for _, l := range r.links {
		o := r.store[l.OpportunityID]
		if l.EntryID == entryID && l.EntryType == entryType && o != nil &&
			o.WorkspaceID == workspaceID && o.PipelineID == pipelineID && o.Status == opportunity.StatusOpen {
			cp := *o
			return &cp, nil
		}
	}
	return nil, opportunity.ErrNotFound
}

func (r *fakeOppRepo) CurrentForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error) {
	if open, err := r.OpenForEntry(workspaceID, pipelineID, entryID, entryType); err == nil {
		return open, nil
	}
	for i := len(r.links) - 1; i >= 0; i-- {
		l := r.links[i]
		o := r.store[l.OpportunityID]
		if l.EntryID == entryID && l.EntryType == entryType && o != nil && o.WorkspaceID == workspaceID && o.PipelineID == pipelineID {
			cp := *o
			return &cp, nil
		}
	}
	return nil, opportunity.ErrNotFound
}

func (r *fakeOppRepo) WithEntryLock(_, _, _ string, fn func(opportunity.Store) error) error {
	r.locks++
	return fn(r)
}

func (r *fakeOppRepo) ListEvents(workspaceID, opportunityID string) ([]opportunity.Event, error) {
	var out []opportunity.Event
	for _, e := range r.events {
		if e.WorkspaceID == workspaceID && e.OpportunityID == opportunityID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *fakeOppRepo) Delete(workspaceID, id string) error {
	if _, ok := r.store[id]; !ok {
		return opportunity.ErrNotFound
	}
	delete(r.store, id)
	return nil
}

func (r *fakeOppRepo) GetByID(workspaceID, id string) (*opportunity.Opportunity, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	o, ok := r.store[id]
	if !ok || o.WorkspaceID != workspaceID {
		return nil, opportunity.ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (r *fakeOppRepo) ListByPipeline(workspaceID, pipelineID string) ([]*opportunity.Opportunity, error) {
	return nil, nil
}

func (r *fakeOppRepo) ListByPipelineScoped(workspaceID, pipelineID string, _ []string, _ bool, _ string) ([]*opportunity.Opportunity, error) {
	return nil, nil
}

func (r *fakeOppRepo) SearchByFilter(opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error) {
	return nil, 0, nil
}

func (r *fakeOppRepo) SumValueByFilter(opportunity.SearchByFilterInput) (int64, error) {
	return 0, nil
}

func (r *fakeOppRepo) eventTypes(opportunityID string) []opportunity.EventType {
	var out []opportunity.EventType
	for _, e := range r.events {
		if e.OpportunityID == opportunityID {
			out = append(out, e.Type)
		}
	}
	return out
}

type fakeLinks struct {
	byEntry []opportunity.ConversationLink
	err     error
}

func (l *fakeLinks) Unlink(string, string, string) error { return nil }
func (l *fakeLinks) ListByOpportunity(string, string) ([]opportunity.ConversationLink, error) {
	return nil, nil
}
func (l *fakeLinks) ListByEntry(string, string, string) ([]opportunity.ConversationLink, error) {
	return l.byEntry, l.err
}

type fakeFieldRepo struct {
	defs []*customfield.Definition
}

func (r *fakeFieldRepo) Create(*customfield.Definition) error { return nil }
func (r *fakeFieldRepo) Update(*customfield.Definition) error { return nil }
func (r *fakeFieldRepo) Delete(string, string) error          { return nil }
func (r *fakeFieldRepo) GetByID(string, string) (*customfield.Definition, error) {
	return nil, errFakeBoom
}
func (r *fakeFieldRepo) ListByObject(string, string) ([]*customfield.Definition, error) {
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

type fakeStages struct {
	byID map[string]*stage.Stage
}

func salesStages() *fakeStages {
	stages := []*stage.Stage{
		{ID: "stage-new", WorkspaceID: "ws1", PipelineID: "pipe1", IsInitial: true, Position: 0},
		{ID: "stage1", WorkspaceID: "ws1", PipelineID: "pipe1", Position: 1},
		{ID: "stage-won", WorkspaceID: "ws1", PipelineID: "pipe1", IsWon: true, Position: 4},
		{ID: "stage-lost", WorkspaceID: "ws1", PipelineID: "pipe1", IsLost: true, Position: 5},
		{ID: "stage-other", WorkspaceID: "ws1", PipelineID: "pipe-conv", Position: 0},
		{ID: "stage-foreign", WorkspaceID: "ws2", PipelineID: "pipe1", Position: 1},
	}
	out := &fakeStages{byID: map[string]*stage.Stage{}}
	for _, s := range stages {
		out.byID[s.ID] = s
	}
	return out
}

func (f *fakeStages) FindByID(id string) (*stage.Stage, error) {
	s, ok := f.byID[id]
	if !ok {
		return nil, stage.ErrTagNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeStages) ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	var out []*stage.Stage
	for _, s := range f.byID {
		if s.WorkspaceID == workspaceID && s.PipelineID == pipelineID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

type fakePipelines struct{}

func (fakePipelines) GetByID(workspaceID, id string) (*pipeline.Pipeline, error) {
	switch {
	case workspaceID == "ws1" && id == "pipe1":
		return &pipeline.Pipeline{ID: id, WorkspaceID: workspaceID, ObjectType: pipeline.ObjectOpportunity}, nil
	case workspaceID == "ws1" && id == "pipe-conv":
		return &pipeline.Pipeline{ID: id, WorkspaceID: workspaceID, ObjectType: pipeline.ObjectConversation}, nil
	}
	return nil, pipeline.ErrNotFound
}

type fakeOwners struct {
	outsiders map[string]bool
}

func (f fakeOwners) Belongs(_ string, actorID string) (bool, error) {
	return !f.outsiders[actorID], nil
}

var fixedNow = time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)

func newService(repo *fakeOppRepo) *Service {
	return serviceWithFields(repo, fieldsWith())
}

func newAutomationService(repo *fakeOppRepo) *Service {
	return serviceWithFields(repo, &fakeFieldRepo{})
}

func serviceWithFields(repo *fakeOppRepo, fields *fakeFieldRepo) *Service {
	return NewService(Deps{
		Repo:      repo,
		Links:     &fakeLinks{},
		Fields:    fields,
		Stages:    salesStages(),
		Pipelines: fakePipelines{},
		Owners:    fakeOwners{outsiders: map[string]bool{"stranger": true}},
		Leads:     leadsIn{"ws1": true},
		Entries:   entriesIn("ws1"),
		Clock:     func() time.Time { return fixedNow },
	})
}

type leadsIn map[string]bool

func (l leadsIn) Get(workspaceID, id string) (*lead.Lead, error) {
	if !l[workspaceID] {
		return nil, lead.ErrLeadNotFound
	}
	return &lead.Lead{ID: id, WorkspaceID: workspaceID}, nil
}

type entriesIn string

func (e entriesIn) GetEntryWorkspaceID(string, string) (string, error) { return string(e), nil }

func foreignService(repo *fakeOppRepo) *Service {
	return NewService(Deps{
		Repo:      repo,
		Links:     &fakeLinks{},
		Fields:    fieldsWith(),
		Stages:    salesStages(),
		Pipelines: fakePipelines{},
		Owners:    fakeOwners{},
		Leads:     leadsIn{"ws2": true},
		Entries:   entriesIn("ws2"),
		Clock:     func() time.Time { return fixedNow },
	})
}

func TestCreateRefusesALeadOfAnotherWorkspace(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.LeadID = "lead-9"
	if _, err := foreignService(repo).Create("ws1", in); !errors.Is(err, ErrLeadOutsideWorkspace) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateRefusesToLinkAConversationOfAnotherWorkspace(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.LinkEntryID, in.LinkEntryType = "entry-9", "whatsapp"
	if _, err := foreignService(repo).Create("ws1", in); !errors.Is(err, ErrEntryOutsideWorkspace) {
		t.Fatalf("err = %v", err)
	}
	if len(repo.links) != 0 {
		t.Fatal("linked a conversation of another workspace")
	}
}

func TestLinkRefusesAConversationOfAnotherWorkspace(t *testing.T) {
	repo := newFakeOppRepo()
	o, err := newService(repo).Create("ws1", baseCreate())
	if err != nil {
		t.Fatal(err)
	}
	if err := foreignService(repo).LinkConversation("ws1", o.ID, "entry-9", "whatsapp", "u1"); !errors.Is(err, ErrEntryOutsideWorkspace) {
		t.Fatalf("err = %v", err)
	}
}

func baseCreate() CreateInput {
	return CreateInput{
		PipelineID:   "pipe1",
		StageID:      "stage1",
		Title:        "Deal",
		ValueCents:   490000,
		CustomFields: map[string]any{"segmento": "enterprise", "score": float64(87)},
		Actor:        "u1",
	}
}

func TestCreate_CustomFieldValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*CreateInput)
		wantErr error
	}{
		{"valid", func(in *CreateInput) {}, nil},
		{"unknown key", func(in *CreateInput) { in.CustomFields["foo"] = "bar" }, ErrUnknownCustomField},
		{"bad select option", func(in *CreateInput) { in.CustomFields["segmento"] = "startup" }, customfield.ErrValueNotInOptions},
		{"wrong type", func(in *CreateInput) { in.CustomFields["score"] = "not-a-number-because-abc" }, customfield.ErrValueType},
		{"missing required", func(in *CreateInput) { delete(in.CustomFields, "segmento") }, customfield.ErrValueRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseCreate()
			tc.mutate(&in)
			_, err := newService(newFakeOppRepo()).Create("ws1", in)
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

func TestCreateMakesTheCreatorTheOwnerAndRecordsIt(t *testing.T) {
	for _, creator := range []string{"u1", "ai:agent-1", "workflow:wf-1"} {
		repo := newFakeOppRepo()
		in := baseCreate()
		in.Actor = creator
		o, err := newService(repo).Create("ws1", in)
		if err != nil {
			t.Fatalf("Create(by %s) error = %v", creator, err)
		}
		if o.OwnerID != creator || o.CreatedBy != creator {
			t.Fatalf("Create(by %s) = owner %q created by %q", creator, o.OwnerID, o.CreatedBy)
		}
		if got := repo.eventTypes(o.ID); len(got) != 1 || got[0] != opportunity.EventCreated {
			t.Fatalf("Create(by %s) events = %v, want [created]", creator, got)
		}
		if repo.events[0].ActorID != creator {
			t.Fatalf("created event actor = %q, want %q", repo.events[0].ActorID, creator)
		}
	}
}

func TestCreateKeepsAnExplicitOwnerFromTheWorkspace(t *testing.T) {
	in := baseCreate()
	in.OwnerID = "u2"
	o, err := newService(newFakeOppRepo()).Create("ws1", in)
	if err != nil || o.OwnerID != "u2" || o.CreatedBy != "u1" {
		t.Fatalf("Create() = %+v, %v, want owner u2 created by u1", o, err)
	}
}

func TestCreateRefusesAnOwnerFromOutsideTheWorkspace(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.OwnerID = "stranger"
	if _, err := newService(repo).Create("ws1", in); !errors.Is(err, ErrOwnerOutsideWorkspace) {
		t.Fatalf("Create() error = %v, want ErrOwnerOutsideWorkspace", err)
	}
	if len(repo.store) != 0 {
		t.Fatalf("a refused deal was saved")
	}
}

func TestCreateNeedsSomeoneToAct(t *testing.T) {
	in := baseCreate()
	in.Actor = ""
	if _, err := newService(newFakeOppRepo()).Create("ws1", in); !errors.Is(err, ErrActorRequired) {
		t.Fatalf("Create() error = %v, want ErrActorRequired", err)
	}
}

func TestCreateOnlyPlacesDealsOnTheirOwnDealFunnel(t *testing.T) {
	cases := map[string]struct {
		pipelineID, stageID string
		want                error
	}{
		"stage from another funnel":    {pipelineID: "pipe1", stageID: "stage-other", want: opportunity.ErrStageOutsidePipeline},
		"stage from another workspace": {pipelineID: "pipe1", stageID: "stage-foreign", want: ErrStageNotFound},
		"conversation funnel":          {pipelineID: "pipe-conv", stageID: "stage-other", want: ErrNotOpportunityPipeline},
		"unknown funnel":               {pipelineID: "nope", stageID: "stage1", want: ErrPipelineNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := baseCreate()
			in.PipelineID, in.StageID = tc.pipelineID, tc.stageID
			if _, err := newService(newFakeOppRepo()).Create("ws1", in); !errors.Is(err, tc.want) {
				t.Fatalf("Create() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCreateOnAWonStageIsAWinThatNeedsAValue(t *testing.T) {
	in := baseCreate()
	in.StageID = "stage-won"
	o, err := newService(newFakeOppRepo()).Create("ws1", in)
	if err != nil || o.Status != opportunity.StatusWon || o.CloseDate == nil || o.ClosedBy != "u1" {
		t.Fatalf("Create(won) = %+v, %v, want a won deal closed now by u1", o, err)
	}

	in.ValueCents = 0
	if _, err := newService(newFakeOppRepo()).Create("ws1", in); !errors.Is(err, opportunity.ErrWonWithoutValue) {
		t.Fatalf("Create(won, no value) error = %v, want ErrWonWithoutValue", err)
	}
}

func TestCreateKeepsAnImportedCloseDate(t *testing.T) {
	historical := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	in := baseCreate()
	in.StageID = "stage-won"
	in.CloseDate = &historical
	o, err := newService(newFakeOppRepo()).Create("ws1", in)
	if err != nil || !o.CloseDate.Equal(historical) {
		t.Fatalf("Create(imported) close date = %v, %v, want %v", o.CloseDate, err, historical)
	}
}

func TestCreateFromAConversationLinksItAtomically(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.LinkEntryID, in.LinkEntryType = "entry-1", "whatsapp"
	o, err := newService(repo).Create("ws1", in)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(repo.links) != 1 || repo.links[0].EntryID != "entry-1" || repo.links[0].OpportunityID != o.ID {
		t.Fatalf("links = %+v, want the conversation", repo.links)
	}
	got := repo.eventTypes(o.ID)
	if len(got) != 2 || got[0] != opportunity.EventCreated || got[1] != opportunity.EventLinked {
		t.Fatalf("events = %v, want [created linked]", got)
	}
}

func TestUpdateToAWonStageStampsWhoAndWhen(t *testing.T) {
	repo := newFakeOppRepo()
	svc := newService(repo)
	created, _ := svc.Create("ws1", baseCreate())
	won := "stage-won"

	o, err := svc.Update("ws1", created.ID, UpdateInput{StageID: &won}, "u7")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if o.Status != opportunity.StatusWon || o.ClosedBy != "u7" || o.CloseDate == nil || !o.CloseDate.Equal(fixedNow) {
		t.Fatalf("Update(won) = %+v", o)
	}
	types := repo.eventTypes(created.ID)
	if types[len(types)-1] != opportunity.EventWon {
		t.Fatalf("events = %v, want the last to be won", types)
	}
}

func TestUpdateRefusesAnOwnerFromOutsideTheWorkspace(t *testing.T) {
	svc := newService(newFakeOppRepo())
	created, _ := svc.Create("ws1", baseCreate())
	stranger := "stranger"
	if _, err := svc.Update("ws1", created.ID, UpdateInput{OwnerID: &stranger}, "u1"); !errors.Is(err, ErrOwnerOutsideWorkspace) {
		t.Fatalf("Update() error = %v, want ErrOwnerOutsideWorkspace", err)
	}
}

func TestMoveStage_LostReasonEnforced(t *testing.T) {
	svc := newService(newFakeOppRepo())
	created, _ := svc.Create("ws1", baseCreate())

	if _, err := svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage-lost"}, "u1"); !errors.Is(err, opportunity.ErrLostReasonMissing) {
		t.Fatalf("expected ErrLostReasonMissing, got %v", err)
	}
	moved, err := svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage-lost", LostReasonID: "reason-price"}, "u1")
	if err != nil {
		t.Fatalf("MoveStage: %v", err)
	}
	if moved.Status != opportunity.StatusLost || moved.LostReasonID != "reason-price" || moved.CloseDate == nil {
		t.Fatalf("unexpected moved state: %#v", moved)
	}
}

func TestMoveStageBackToAnOpenStageReopens(t *testing.T) {
	svc := newService(newFakeOppRepo())
	created, _ := svc.Create("ws1", baseCreate())
	_, _ = svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage-won"}, "u1")
	reopened, err := svc.MoveStage("ws1", created.ID, MoveStageInput{StageID: "stage1"}, "u1")
	if err != nil || reopened.Status != opportunity.StatusOpen || reopened.CloseDate != nil || reopened.ClosedBy != "" {
		t.Fatalf("reopened = %+v, %v", reopened, err)
	}
}

func TestListForEntryFailsInsteadOfHidingADeal(t *testing.T) {
	repo := newFakeOppRepo()
	repo.getErr = errFakeBoom
	svc := NewService(Deps{
		Repo:  repo,
		Links: &fakeLinks{byEntry: []opportunity.ConversationLink{{OpportunityID: "opp1", EntryID: "e1", EntryType: "whatsapp"}}},
	})
	if _, err := svc.ListOpportunitiesForEntry("ws1", "e1", "whatsapp"); !errors.Is(err, errFakeBoom) {
		t.Fatalf("ListOpportunitiesForEntry() error = %v, want the read error", err)
	}
}

func entryCommand(action EntryAction) EntryCommand {
	value := int64(7900)
	return EntryCommand{
		EntryID:    "entry-1",
		EntryType:  "whatsapp",
		PipelineID: "pipe1",
		Actor:      "ai:agent-1",
		Action:     action,
		Title:      "Plano Pro",
		ValueCents: &value,
	}
}

func TestAutomationCreatesTheConversationsDealOnItsFirstStage(t *testing.T) {
	repo := newFakeOppRepo()
	result, err := newAutomationService(repo).ManageForEntry("ws1", entryCommand(EntryCreate))
	if err != nil {
		t.Fatalf("ManageForEntry() error = %v", err)
	}
	o := result.Opportunity
	if !result.Created || o.StageID != "stage-new" || o.OwnerID != "ai:agent-1" || o.CreatedBy != "ai:agent-1" || o.ValueCents != 7900 {
		t.Fatalf("created = %+v (created %v)", o, result.Created)
	}
	if len(repo.links) != 1 || repo.links[0].EntryID != "entry-1" {
		t.Fatalf("the deal is not linked to its conversation: %+v", repo.links)
	}
	if repo.locks != 1 {
		t.Fatalf("ManageForEntry() took %d locks, want 1", repo.locks)
	}
}

func TestAutomationNeverOpensASecondDealForTheSameConversation(t *testing.T) {
	repo := newFakeOppRepo()
	svc := newAutomationService(repo)
	first, _ := svc.ManageForEntry("ws1", entryCommand(EntryCreate))

	again := entryCommand(EntryCreate)
	bigger := int64(15000)
	again.ValueCents = &bigger
	second, err := svc.ManageForEntry("ws1", again)
	if err != nil {
		t.Fatalf("ManageForEntry() error = %v", err)
	}
	if second.Created || second.Opportunity.ID != first.Opportunity.ID || second.Opportunity.ValueCents != 15000 {
		t.Fatalf("second = %+v (created %v), want the first deal with the new value", second.Opportunity, second.Created)
	}
	if len(repo.store) != 1 {
		t.Fatalf("%d deals for one conversation", len(repo.store))
	}
}

func TestAutomationWinCreatesAndClosesTheDeal(t *testing.T) {
	result, err := newAutomationService(newFakeOppRepo()).ManageForEntry("ws1", entryCommand(EntryWin))
	if err != nil {
		t.Fatalf("ManageForEntry(win) error = %v", err)
	}
	o := result.Opportunity
	if o.Status != opportunity.StatusWon || o.StageID != "stage-won" || o.ClosedBy != "ai:agent-1" {
		t.Fatalf("won = %+v", o)
	}
}

func TestAutomationCannotWinWithoutAValue(t *testing.T) {
	cmd := entryCommand(EntryWin)
	cmd.ValueCents = nil
	repo := newFakeOppRepo()
	if _, err := newAutomationService(repo).ManageForEntry("ws1", cmd); !errors.Is(err, opportunity.ErrWonWithoutValue) {
		t.Fatalf("ManageForEntry(win, no value) error = %v, want ErrWonWithoutValue", err)
	}
	if len(repo.store) != 0 {
		t.Fatalf("a refused win left a deal behind")
	}
}

func TestAutomationCannotLoseADealThatDoesNotExist(t *testing.T) {
	cmd := entryCommand(EntryLose)
	cmd.LostReasonID = "price"
	if _, err := newAutomationService(newFakeOppRepo()).ManageForEntry("ws1", cmd); !errors.Is(err, ErrNoOpenDeal) {
		t.Fatalf("ManageForEntry(lose, no deal) error = %v, want ErrNoOpenDeal", err)
	}
}

func TestAutomationLosesWithAReason(t *testing.T) {
	svc := newAutomationService(newFakeOppRepo())
	_, _ = svc.ManageForEntry("ws1", entryCommand(EntryCreate))

	if _, err := svc.ManageForEntry("ws1", entryCommand(EntryLose)); !errors.Is(err, opportunity.ErrLostReasonMissing) {
		t.Fatalf("ManageForEntry(lose, no reason) error = %v, want ErrLostReasonMissing", err)
	}
	cmd := entryCommand(EntryLose)
	cmd.LostReasonID = "price"
	result, err := svc.ManageForEntry("ws1", cmd)
	if err != nil || result.Opportunity.Status != opportunity.StatusLost || result.Opportunity.StageID != "stage-lost" {
		t.Fatalf("lost = %+v, %v", result, err)
	}
}

func TestAutomationMovesOnlyWithinItsFunnel(t *testing.T) {
	svc := newAutomationService(newFakeOppRepo())
	cmd := entryCommand(EntryMove)
	cmd.StageID = "stage-other"
	if _, err := svc.ManageForEntry("ws1", cmd); !errors.Is(err, opportunity.ErrStageOutsidePipeline) {
		t.Fatalf("ManageForEntry(move elsewhere) error = %v, want ErrStageOutsidePipeline", err)
	}
	cmd.StageID = ""
	if _, err := svc.ManageForEntry("ws1", cmd); !errors.Is(err, opportunity.ErrStageRequired) {
		t.Fatalf("ManageForEntry(move nowhere) error = %v, want ErrStageRequired", err)
	}
}

func TestAutomationNeedsItsConversationAndActor(t *testing.T) {
	for name, mutate := range map[string]func(*EntryCommand){
		"no conversation": func(c *EntryCommand) { c.EntryID = "" },
		"no actor":        func(c *EntryCommand) { c.Actor = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cmd := entryCommand(EntryCreate)
			mutate(&cmd)
			if _, err := newAutomationService(newFakeOppRepo()).ManageForEntry("ws1", cmd); err == nil {
				t.Fatalf("ManageForEntry(%s) succeeded", name)
			}
		})
	}
}

func TestAutomationCannotSkipTheWorkspacesRequiredFields(t *testing.T) {
	repo := newFakeOppRepo()
	if _, err := newService(repo).ManageForEntry("ws1", entryCommand(EntryCreate)); !errors.Is(err, customfield.ErrValueRequired) {
		t.Fatalf("ManageForEntry() without a required field error = %v, want ErrValueRequired", err)
	}
	if len(repo.store) != 0 {
		t.Fatalf("a deal missing a required field was saved")
	}
}

func TestPipelineStagesAreThoseOfADealFunnelInOrder(t *testing.T) {
	svc := newAutomationService(newFakeOppRepo())
	stages, err := svc.PipelineStages("ws1", "pipe1")
	if err != nil {
		t.Fatalf("PipelineStages() error = %v", err)
	}
	got := make([]string, 0, len(stages))
	for _, st := range stages {
		got = append(got, st.ID)
	}
	want := []string{"stage-new", "stage1", "stage-won", "stage-lost"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("PipelineStages() = %v, want %v", got, want)
	}
	if _, err := svc.PipelineStages("ws1", "pipe-conv"); !errors.Is(err, ErrNotOpportunityPipeline) {
		t.Fatalf("PipelineStages(conversation funnel) error = %v, want ErrNotOpportunityPipeline", err)
	}
}

func TestCurrentDealForEntryReadsTheConversationsDeal(t *testing.T) {
	svc := newAutomationService(newFakeOppRepo())
	if _, err := svc.CurrentDealForEntry("ws1", "pipe1", "entry-1", "whatsapp"); !errors.Is(err, opportunity.ErrNotFound) {
		t.Fatalf("CurrentDealForEntry() before any deal error = %v, want ErrNotFound", err)
	}
	won, _ := svc.ManageForEntry("ws1", entryCommand(EntryWin))
	got, err := svc.CurrentDealForEntry("ws1", "pipe1", "entry-1", "whatsapp")
	if err != nil || got.ID != won.Opportunity.ID || got.Status != opportunity.StatusWon {
		t.Fatalf("CurrentDealForEntry() = %+v, %v, want the won deal", got, err)
	}
	if _, err := svc.CurrentDealForEntry("ws1", "pipe-conv", "entry-1", "whatsapp"); !errors.Is(err, ErrNotOpportunityPipeline) {
		t.Fatalf("CurrentDealForEntry(conversation funnel) error = %v, want ErrNotOpportunityPipeline", err)
	}
}

func (fakePipelines) ListByWorkspace(workspaceID, objectType string) ([]*pipeline.Pipeline, error) {
	if workspaceID != "ws1" || objectType != string(pipeline.ObjectOpportunity) {
		return nil, nil
	}
	return []*pipeline.Pipeline{{ID: "pipe1", WorkspaceID: workspaceID, ObjectType: pipeline.ObjectOpportunity}}, nil
}

func TestDealPipelinesAreOnlyTheWorkspacesDealFunnels(t *testing.T) {
	got, err := newAutomationService(newFakeOppRepo()).DealPipelines("ws1")
	if err != nil || len(got) != 1 || got[0].ID != "pipe1" {
		t.Fatalf("DealPipelines() = %+v, %v, want pipe1", got, err)
	}
}
