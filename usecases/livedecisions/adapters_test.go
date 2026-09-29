package livedecisions_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

type fakeMessages struct {
	messages []*conversation.Message
	input    conversation.ListMessagesInput
	err      error
}

func (f *fakeMessages) ListByEntryPaginated(input conversation.ListMessagesInput) ([]*conversation.Message, error) {
	f.input = input
	return f.messages, f.err
}

type fakeStages struct {
	entry     *stage.EntryStage
	stages    map[string]*stage.Stage
	pipeline  []*stage.Stage
	campaign  []*stage.Stage
	assigned  []stage.AssignEntryStageInput
	assignErr error
	entryErr  error
}

func (f *fakeStages) GetEntryStage(string, string, string) (*stage.EntryStage, error) {
	return f.entry, f.entryErr
}

type fakeCampaigns map[string]string

func (f fakeCampaigns) GetEntryCampaignID(entryID, _ string) (string, error) {
	return f[entryID], nil
}
func (f *fakeStages) FindByID(id string) (*stage.Stage, error)              { return f.stages[id], nil }
func (f *fakeStages) ListByPipeline(string, string) ([]*stage.Stage, error) { return f.pipeline, nil }
func (f *fakeStages) ListByCampaign(string, string, string) ([]*stage.Stage, error) {
	return f.campaign, nil
}
func (f *fakeStages) Execute(_ string, input stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	if f.assignErr != nil {
		return nil, f.assignErr
	}
	f.assigned = append(f.assigned, input)
	return &stage.EntryStage{StageID: input.StageID}, nil
}

func message(text string, customer bool, at time.Time) *conversation.Message {
	direction := conversation.MessageDirectionOutbound
	if customer {
		direction = conversation.MessageDirectionInbound
	}
	return &conversation.Message{Text: text, MessageType: conversation.MessageTypeUserMessage, Direction: direction, CreatedAt: at}
}

func TestASnapshotHoldsTheRecentConversationOldestFirst(t *testing.T) {
	messages := &fakeMessages{messages: []*conversation.Message{
		message("fechado", true, t0.Add(2*time.Minute)),
		{Text: "", MessageType: conversation.MessageTypeSystem, CreatedAt: t0.Add(time.Minute)},
		message("R$ 1.200", false, t0),
	}}
	reader := ConversationSnapshots{Messages: messages, Stages: &fakeStages{}}
	snapshot, err := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e", EntryType: shared.EntryTypeWhatsApp}, false)
	if err != nil {
		t.Fatal(err)
	}
	if messages.input.Limit != ld.MaxTurns || messages.input.EntryID != "e" {
		t.Fatalf("listed %+v", messages.input)
	}
	if len(snapshot.Turns) != 2 || snapshot.Turns[0].Text != "R$ 1.200" || snapshot.Turns[0].Role != ld.RoleAgent ||
		snapshot.Turns[1].Role != ld.RoleCustomer || snapshot.WorkspaceID != "ws" || snapshot.EntryType != "whatsapp" {
		t.Fatalf("turns = %+v", snapshot.Turns)
	}
}

func TestStagesComeFromTheEntrysPipeline(t *testing.T) {
	stages := &fakeStages{
		entry:    &stage.EntryStage{StageID: "s1"},
		stages:   map[string]*stage.Stage{"s1": {ID: "s1", WorkspaceID: "ws", PipelineID: "p1"}},
		pipeline: []*stage.Stage{{ID: "s1", Name: "Novo"}, {ID: "s2", Name: "Ganho", Description: "fechou"}},
	}
	reader := ConversationSnapshots{Messages: &fakeMessages{}, Stages: stages}
	snapshot, err := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e", EntryType: shared.EntryTypeWhatsApp}, true)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentStageID != "s1" || len(snapshot.Stages) != 2 || snapshot.Stages[1].Description != "fechou" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	withoutStages, _ := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e"}, false)
	if len(withoutStages.Stages) != 0 {
		t.Fatal("stages are only read when staging is asked")
	}
}

func TestACampaignStageWithoutPipelineUsesTheCampaignStages(t *testing.T) {
	stages := &fakeStages{
		entry:    &stage.EntryStage{StageID: "s1"},
		stages:   map[string]*stage.Stage{"s1": {ID: "s1", WorkspaceID: "ws", CampaignID: "c1", CampaignType: "whatsapp"}},
		campaign: []*stage.Stage{{ID: "s1", Name: "A"}, {ID: "s9", Name: "B"}},
	}
	reader := ConversationSnapshots{Messages: &fakeMessages{}, Stages: stages}
	snapshot, _ := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e"}, true)
	if len(snapshot.Stages) != 2 || snapshot.Stages[1].ID != "s9" {
		t.Fatalf("stages = %+v", snapshot.Stages)
	}
}

func TestAConversationWithoutAStageIsOfferedItsCampaignFunnel(t *testing.T) {
	stages := &fakeStages{campaign: []*stage.Stage{{ID: "r1", Name: "recebido"}, {ID: "a1", Name: "agendamento"}}}
	reader := ConversationSnapshots{Messages: &fakeMessages{}, Stages: stages, Campaigns: fakeCampaigns{"e": "camp-1"}}
	snapshot, _ := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e", EntryType: shared.EntryTypeUnofficialWhatsApp}, true)
	if snapshot.CurrentStageID != "" || len(snapshot.Stages) != 2 || snapshot.Stages[1].ID != "a1" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestAnUnreadableStageAsksNoStageQuestion(t *testing.T) {
	stages := &fakeStages{entryErr: errors.New("down"), campaign: []*stage.Stage{{ID: "r1"}, {ID: "a1"}}}
	reader := ConversationSnapshots{Messages: &fakeMessages{}, Stages: stages, Campaigns: fakeCampaigns{}}
	snapshot, err := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e"}, true)
	if err != nil || len(snapshot.Stages) != 0 {
		t.Fatalf("a stage that cannot be read must not be replaced by another funnel: %+v, %v", snapshot.Stages, err)
	}
}

func TestAnUnreadableConversationIsAnError(t *testing.T) {
	reader := ConversationSnapshots{Messages: &fakeMessages{err: errors.New("down")}, Stages: &fakeStages{}}
	if _, err := reader.Snapshot(context.Background(), Trigger{WorkspaceID: "ws", EntryID: "e"}, false); err == nil {
		t.Fatal("deciding on an empty conversation would be wrong")
	}
}

type stageHub struct{ updates chan string }

func (h stageHub) BroadcastStageUpdate(_, entryID, _ string) { h.updates <- entryID }

func TestAStageMoveIsRecordedAsThePlatformAIWithItsCertaintyAndAnnounced(t *testing.T) {
	stages := &fakeStages{}
	hub := stageHub{updates: make(chan string, 1)}
	mover := StageEffects{Assign: stages, Hub: hub}
	if err := mover.MoveStage(context.Background(), "ws", "e", shared.EntryTypeWhatsApp, "s2", 0.93); err != nil {
		t.Fatal(err)
	}
	if len(stages.assigned) != 1 || stages.assigned[0].ActorID != actor.PlatformAI || stages.assigned[0].Confidence != 0.93 || stages.assigned[0].EntryType != "whatsapp" {
		t.Fatalf("assigned = %+v", stages.assigned)
	}
	select {
	case id := <-hub.updates:
		if id != "e" {
			t.Fatalf("announced %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("the move was not announced")
	}
	stages.assignErr = errors.New("gone")
	if err := mover.MoveStage(context.Background(), "ws", "e", shared.EntryTypeWhatsApp, "s2", 0.93); err == nil {
		t.Fatal("a failed move is reported")
	}
}

type counterState struct {
	memoryState
	counts map[string]int64
	err    error
}

func (c *counterState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	if c.err != nil {
		return 0, c.err
	}
	c.counts[key]++
	return c.counts[key], nil
}

func TestTheHourlyLimiterStopsARunawayConversationAndFailsClosed(t *testing.T) {
	state := &counterState{counts: map[string]int64{}}
	limiter := HourlyLimiter{State: state, Max: 2}
	if !limiter.AllowDecision("ws", "e") || !limiter.AllowDecision("ws", "e") || limiter.AllowDecision("ws", "e") {
		t.Fatal("the third decision in the hour is refused")
	}
	if !limiter.AllowDecision("ws", "other") {
		t.Fatal("each conversation has its own budget")
	}
	state.err = errors.New("redis down")
	if limiter.AllowDecision("ws", "fresh") {
		t.Fatal("an unreadable counter refuses rather than spending blind")
	}
}

type readsByRef struct {
	reads map[ld.EntryRef]ld.LiveRead
	ws    string
}

func (r *readsByRef) Save(context.Context, ld.LiveRead) (bool, error) { return true, nil }
func (r *readsByRef) Get(context.Context, string, string, string) (*ld.LiveRead, error) {
	return nil, nil
}
func (r *readsByRef) ForEntries(_ context.Context, ws string, refs []ld.EntryRef) (map[ld.EntryRef]ld.LiveRead, error) {
	r.ws = ws
	out := map[ld.EntryRef]ld.LiveRead{}
	for _, ref := range refs {
		if read, ok := r.reads[ref]; ok {
			out[ref] = read
		}
	}
	return out, nil
}

func TestTheInboxGetsViewsOnlyForReadsWithLabels(t *testing.T) {
	store := &readsByRef{reads: map[ld.EntryRef]ld.LiveRead{
		{EntryID: "a", EntryType: "whatsapp"}: {Qualification: "hot_lead"},
		{EntryID: "b", EntryType: "whatsapp"}: {StageSettled: true},
	}}
	views, err := (InboxReads{Reads: store}).LiveReadViews("ws", []string{"a", "b", "c"}, "whatsapp")
	if err != nil || store.ws != "ws" || len(views) != 1 || views["a"].Qualification != "hot_lead" {
		t.Fatalf("views = %+v, err = %v", views, err)
	}
}
