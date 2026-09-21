package conversation_event_usecase

import (
	"errors"
	"testing"

	"vozko/domain/actor"
	agentdomain "vozko/domain/agent"
	ce "vozko/domain/conversation_event"
	labeldomain "vozko/domain/label"
	"vozko/domain/shared"
	stagedomain "vozko/domain/stage"
	"vozko/domain/user"
)

type stubEventRepo struct {
	events []*ce.ConversationEvent
}

func (r *stubEventRepo) Create(*ce.ConversationEvent) error { return nil }
func (r *stubEventRepo) ListByEntry(_, _, _ string, _, _ int) ([]*ce.ConversationEvent, int64, error) {
	return r.events, int64(len(r.events)), nil
}
func (r *stubEventRepo) ListByEntryFiltered(_, _, _ string, _ ce.ListFilter) ([]*ce.ConversationEvent, int64, error) {
	return r.events, int64(len(r.events)), nil
}

type stubUserRepo struct {
	byID    map[string]string
	calls   int
	lastIDs []string
	err     error
}

func (r *stubUserRepo) FindByIDs(ids []string) ([]*user.User, error) {
	r.calls++
	r.lastIDs = append(r.lastIDs, ids...)
	if r.err != nil {
		return nil, r.err
	}
	out := []*user.User{}
	for _, id := range ids {
		if name, ok := r.byID[id]; ok {
			out = append(out, &user.User{ID: id, Username: name})
		}
	}
	return out, nil
}

func (r *stubUserRepo) WithTx(interface{}) user.UserRepository    { return r }
func (r *stubUserRepo) Create(*user.User) error                   { return nil }
func (r *stubUserRepo) Update(string, *user.User) error           { return nil }
func (r *stubUserRepo) Delete(string) error                       { return nil }
func (r *stubUserRepo) FindByID(string) (*user.User, error)       { return nil, nil }
func (r *stubUserRepo) FindByEmail(string) (*user.User, error)    { return nil, nil }
func (r *stubUserRepo) FindByDocument(string) (*user.User, error) { return nil, nil }
func (r *stubUserRepo) CountByRole(user.Role) (int64, error)      { return 0, nil }
func (r *stubUserRepo) GetUserRole(string) (string, error)        { return "", nil }
func (r *stubUserRepo) GetTokenVersion(string) (int, error)       { return 0, nil }
func (r *stubUserRepo) IncrementTokenVersion(string) (int, error) { return 0, nil }
func (r *stubUserRepo) List(user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	return nil, nil
}

type stubAgentRepo struct {
	byID  map[string]string
	calls int
}

func (r *stubAgentRepo) FindByIDs(ids []string) ([]*agentdomain.Agent, error) {
	r.calls++
	out := []*agentdomain.Agent{}
	for _, id := range ids {
		if name, ok := r.byID[id]; ok {
			out = append(out, &agentdomain.Agent{ID: id, Name: name})
		}
	}
	return out, nil
}

func (r *stubAgentRepo) Create(*agentdomain.Agent) error             { return nil }
func (r *stubAgentRepo) Update(string, *agentdomain.Agent) error     { return nil }
func (r *stubAgentRepo) Delete(string) error                         { return nil }
func (r *stubAgentRepo) FindByID(string) (*agentdomain.Agent, error) { return nil, nil }
func (r *stubAgentRepo) List(agentdomain.ListAgentsInput) (*shared.PaginatedResult[*agentdomain.AgentListItem], error) {
	return nil, nil
}

const (
	ana   = "11111111-1111-4111-8111-111111111111"
	bruno = "22222222-2222-4222-8222-222222222222"
	bot   = "33333333-3333-4333-8333-333333333333"
)

func newUC(events []*ce.ConversationEvent) (ce.ListEventsUseCase, *stubUserRepo, *stubAgentRepo) {
	users := &stubUserRepo{byID: map[string]string{ana: "ana", bruno: "bruno"}}
	agents := &stubAgentRepo{byID: map[string]string{bot: "Atendente IA"}}
	return NewListEventsUseCase(&stubEventRepo{events: events}, users, agents, nil, nil), users, agents
}

func TestExecuteNamesTheSenderOfAHumanReply(t *testing.T) {
	uc, _, _ := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "whatsapp", ce.EventReplied).
			WithActorHuman(ana).
			WithDetails(map[string]string{"message_id": "m1"}).
			Build(),
	})

	got, _, err := uc.Execute("ws", "entry", "whatsapp", 50, 0)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got[0].ActorName != "ana" {
		t.Fatalf("ActorName = %q, want ana", got[0].ActorName)
	}
}

func TestExecuteNamesBothSidesOfAHandoff(t *testing.T) {
	uc, _, _ := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "whatsapp", ce.EventAssigned).
			WithActorHuman(ana).
			WithDetails(map[string]string{
				"from_user_id": bruno,
				"to_user_id":   ana,
				"trigger":      "manual",
			}).
			Build(),
	})

	got, _, err := uc.Execute("ws", "entry", "whatsapp", 50, 0)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	ev := got[0]
	if ev.ActorName != "ana" || ev.FromName != "bruno" || ev.ToName != "ana" {
		t.Fatalf("actor=%q from=%q to=%q, want ana/bruno/ana", ev.ActorName, ev.FromName, ev.ToName)
	}
}

func TestExecuteNamesTheAIAgent(t *testing.T) {
	uc, _, _ := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "whatsapp", ce.EventAIReplied).WithActorAI(bot).Build(),
	})

	got, _, _ := uc.Execute("ws", "entry", "whatsapp", 50, 0)
	if got[0].ActorName != "Atendente IA" {
		t.Fatalf("ActorName = %q, want Atendente IA", got[0].ActorName)
	}
}

func TestExecuteBatchesLookupsAcrossThePage(t *testing.T) {
	events := []*ce.ConversationEvent{}
	for i := 0; i < 10; i++ {
		events = append(events,
			ce.New("ws", "entry", "whatsapp", ce.EventReplied).WithActorHuman(ana).Build(),
			ce.New("ws", "entry", "whatsapp", ce.EventAIReplied).WithActorAI(bot).Build(),
		)
	}
	uc, users, agents := newUC(events)

	if _, _, err := uc.Execute("ws", "entry", "whatsapp", 50, 0); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if users.calls != 1 || agents.calls != 1 {
		t.Fatalf("users=%d agents=%d lookups, want 1 each", users.calls, agents.calls)
	}
}

func TestExecuteDegradesWithoutNames(t *testing.T) {
	sys := ce.New("ws", "entry", "whatsapp", ce.EventFinished).WithActorSystem().Build()
	if sys.ActorID != actor.SystemID {
		t.Fatalf("system actor id = %q", sys.ActorID)
	}

	uc, users, _ := newUC([]*ce.ConversationEvent{
		sys,
		ce.New("ws", "entry", "whatsapp", ce.EventReplied).WithActorHuman(ana).Build(),
	})
	users.err = errors.New("db down")

	got, _, err := uc.Execute("ws", "entry", "whatsapp", 50, 0)
	if err != nil {
		t.Fatalf("Execute must survive a name lookup failure: %v", err)
	}
	if got[0].ActorName != "" || got[1].ActorName != "" {
		t.Fatalf("names = %q/%q, want empty", got[0].ActorName, got[1].ActorName)
	}
}

func TestExecuteLeavesUnknownIDsUnnamed(t *testing.T) {
	uc, _, _ := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "whatsapp", ce.EventAssigned).
			WithActorHuman("99999999-9999-4999-8999-999999999999").
			WithDetails(map[string]string{"to_user_id": "88888888-8888-4888-8888-888888888888"}).
			Build(),
	})

	got, _, _ := uc.Execute("ws", "entry", "whatsapp", 50, 0)
	if got[0].ActorName != "" || got[0].ToName != "" {
		t.Fatalf("actor=%q to=%q, want empty", got[0].ActorName, got[0].ToName)
	}
}

func TestExecuteSkipsNonUUIDTransferTargets(t *testing.T) {
	uc, users, _ := newUC([]*ce.ConversationEvent{
		ce.New("ws", "entry", "voice", ce.EventTransferCompleted).
			WithActorHuman(ana).
			WithDetails(map[string]string{"target": "Support queue"}).
			Build(),
	})

	got, _, err := uc.Execute("ws", "entry", "voice", 50, 0)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got[0].ToName != "" {
		t.Fatalf("ToName = %q, want empty (the UI shows the raw target)", got[0].ToName)
	}
	if got[0].ActorName != "ana" {
		t.Fatalf("ActorName = %q, the bad target must not cost the page its names", got[0].ActorName)
	}
	for _, id := range users.lastIDs {
		if id == "Support queue" {
			t.Fatal("non-uuid target reached the user lookup")
		}
	}
}

type stubStageRepo struct {
	stagedomain.Repository
	byID  map[string]string
	calls int
}

func (r *stubStageRepo) ListByWorkspace(string) ([]*stagedomain.Stage, error) {
	r.calls++
	out := []*stagedomain.Stage{}
	for id, name := range r.byID {
		out = append(out, &stagedomain.Stage{ID: id, Name: name})
	}
	return out, nil
}

type stubLabelRepo struct {
	labeldomain.Repository
	byID  map[string]string
	calls int
}

func (r *stubLabelRepo) ListByWorkspace(string) ([]*labeldomain.Label, error) {
	r.calls++
	out := []*labeldomain.Label{}
	for id, name := range r.byID {
		out = append(out, &labeldomain.Label{ID: id, Name: name})
	}
	return out, nil
}

func newSubjectUC(events []*ce.ConversationEvent) (ce.ListEventsUseCase, *stubStageRepo, *stubLabelRepo) {
	stages := &stubStageRepo{byID: map[string]string{"s-old": "recebido", "s-new": "em atendimento"}}
	labels := &stubLabelRepo{byID: map[string]string{"l1": "urgente"}}
	uc := NewListEventsUseCase(&stubEventRepo{events: events}, nil, nil, stages, labels)
	return uc, stages, labels
}

func TestExecuteBackfillsStageAndLabelNames(t *testing.T) {
	uc, _, _ := newSubjectUC([]*ce.ConversationEvent{
		ce.New("ws", "e1", "whatsapp", ce.EventStageChanged).
			WithDetails(map[string]string{"to_stage_id": "s-new", "from_stage_id": "s-old"}).
			Build(),
		ce.New("ws", "e1", "whatsapp", ce.EventLabelAdded).
			WithDetails(map[string]string{"label_id": "l1"}).
			Build(),
	})

	got, _, err := uc.Execute("ws", "e1", "whatsapp", 50, 0)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stageDetails := got[0].DetailsMap()
	if stageDetails["stage_name"] != "em atendimento" || stageDetails["from_stage_name"] != "recebido" {
		t.Fatalf("stage details = %v, want the move named both ways", stageDetails)
	}
	if got := got[1].DetailsMap()["label_name"]; got != "urgente" {
		t.Fatalf("label_name = %q, want urgente", got)
	}
}

func TestExecuteKeepsNamesTheEventAlreadyCarries(t *testing.T) {
	uc, _, _ := newSubjectUC([]*ce.ConversationEvent{
		ce.New("ws", "e1", "whatsapp", ce.EventStageChanged).
			WithDetails(map[string]string{"to_stage_id": "s-new", "stage_name": "nome antigo"}).
			Build(),
	})

	got, _, _ := uc.Execute("ws", "e1", "whatsapp", 50, 0)
	if name := got[0].DetailsMap()["stage_name"]; name != "nome antigo" {
		t.Fatalf("stage_name = %q, want the stored name kept", name)
	}
}

func TestExecuteReadsStagesAndLabelsAtMostOncePerPage(t *testing.T) {
	events := []*ce.ConversationEvent{}
	for i := 0; i < 10; i++ {
		events = append(events, ce.New("ws", "e1", "whatsapp", ce.EventStageChanged).
			WithDetails(map[string]string{"to_stage_id": "s-new"}).
			Build())
	}
	uc, stages, labels := newSubjectUC(events)

	if _, _, err := uc.Execute("ws", "e1", "whatsapp", 50, 0); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stages.calls != 1 {
		t.Fatalf("stage reads = %d, want 1", stages.calls)
	}
	if labels.calls != 0 {
		t.Fatalf("label reads = %d, want 0 — no label event on this page", labels.calls)
	}
}
