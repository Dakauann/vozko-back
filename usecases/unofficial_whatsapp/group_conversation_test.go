package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

// recordingHistory captures what the webhook path persisted, so a test can
// assert who a message was attributed to.
type recordingHistory struct {
	mu         sync.Mutex
	records    []conversation.MessageHistoryRecord
	directions []conversation.MessageHistoryDirection
}

func (h *recordingHistory) Record(_ context.Context, d conversation.MessageHistoryDirection, r conversation.MessageHistoryRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	h.directions = append(h.directions, d)
	return nil
}

// directionOf returns the direction recorded alongside record i.
func (h *recordingHistory) directionOf(i int) conversation.MessageHistoryDirection {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i < 0 || i >= len(h.directions) {
		return conversation.MessageDirectionUnknown
	}
	return h.directions[i]
}

func (h *recordingHistory) all() []conversation.MessageHistoryRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]conversation.MessageHistoryRecord(nil), h.records...)
}

// groupHarness assembles the webhook usecase with every port a group message
// touches.
type groupHarness struct {
	uc        *HandleWebhookUseCase
	instance  *uw.Instance
	contacts  *fakeContactRepo
	convs     *fakeConversationRepo
	groups    *fakeGroupRepo
	groupAPI  *fakeGroupAPI
	messaging *fakeMessaging
	assets    *fakeAssets
	storage   *fakeStorage
	history   *recordingHistory
}

func newGroupHarness(t *testing.T, handleGroups bool) *groupHarness {
	t.Helper()

	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: uw.StatusConnected, HandleGroups: handleGroups,
		PhoneNumber: "5599999999999",
	}
	h := &groupHarness{
		instance:  instance,
		contacts:  newFakeContactRepo(),
		convs:     newFakeConversationRepo(),
		groups:    newFakeGroupRepo(),
		groupAPI:  &fakeGroupAPI{},
		messaging: &fakeMessaging{},
		assets:    &fakeAssets{},
		storage:   newFakeStorage(),
		history:   &recordingHistory{},
	}
	h.uc = NewHandleWebhookUseCase(HandleWebhookDeps{
		Instances:     newFakeInstanceRepo(instance),
		Servers:       newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		Contacts:      h.contacts,
		Conversations: h.convs,
		Groups:        h.groups,
		Messaging:     h.messaging,
		GroupAPI:      h.groupAPI,
		Assets:        h.assets,
		FileStorage:   h.storage,
		History:       h.history,
	})
	return h
}

// deliver feeds one provider message through the whole ingest path.
func (h *groupHarness) deliver(t *testing.T, msg map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1", "data": msg,
	})
	if err != nil {
		t.Fatalf("encoding the provider message: %v", err)
	}
	if err := h.uc.Execute(context.Background(), &QueuedEvent{
		InstanceID: "inst-1", Body: body,
	}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
}

func groupMessage(from, text string) map[string]any {
	return map[string]any{
		"messageid": "msg-" + from + "-" + text,
		"chatid":    "120363012345678901@g.us",
		"sender":    from + "@s.whatsapp.net",
		"sender_pn": from + "@s.whatsapp.net",
		"senderName": map[string]string{
			"5511111111111": "Ana",
			"5522222222222": "Bruno",
		}[from],
		"isGroup":          true,
		"messageType":      "text",
		"text":             text,
		"messageTimestamp": time.Now().UnixMilli(),
	}
}

// ONE group is ONE conversation, however many members write in it.
//
// This is the bug that made groups unusable in production. Conversations were
// keyed by (instance, contact) and a group message's contact was resolved from
// its SENDER, so every member who spoke created another conversation for the
// same chat — each labelled with that member's name and number, with the lookup
// that resolves a chat for delivery receipts picking one of them arbitrarily.
func TestOneGroupIsOneConversation(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliver(t, groupMessage("5511111111111", "oi"))
	h.deliver(t, groupMessage("5522222222222", "bom dia"))
	h.deliver(t, groupMessage("5511111111111", "tudo bem?"))

	if got := len(h.convs.created); got != 1 {
		t.Fatalf("three messages from two members created %d conversations, want 1", got)
	}

	conv := h.convs.created[0]
	if !conv.IsGroup {
		t.Error("the conversation is not flagged as a group")
	}
	if conv.ChatID != "120363012345678901@g.us" {
		t.Errorf("chat id = %q, want the group jid", conv.ChatID)
	}

	// The subject is the GROUP, not whoever spoke first.
	subject, err := h.contacts.FindByID(context.Background(), conv.ContactID)
	if err != nil {
		t.Fatalf("resolving the conversation subject: %v", err)
	}
	if !subject.IsGroup {
		t.Fatalf("the subject is %q, a participant rather than the group", subject.JID)
	}
	if subject.PhoneNumber != "" {
		t.Errorf("the group subject carries a phone number (%q); it is not dialable",
			subject.PhoneNumber)
	}
	if subject.LeadID != nil {
		t.Error("a group must never be bridged to a CRM lead")
	}
}

// Each message names the MEMBER who wrote it, not the group.
//
// The conversation's subject is the group, so labelling bubbles with the subject
// would attribute the whole thread to itself and make a group transcript
// unreadable — every line from "Time Comercial".
func TestGroupMessagesAreAttributedToTheirAuthor(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliver(t, groupMessage("5511111111111", "oi"))
	h.deliver(t, groupMessage("5522222222222", "bom dia"))

	records := h.history.all()
	if len(records) != 2 {
		t.Fatalf("persisted %d messages, want 2", len(records))
	}
	if records[0].SenderName != "Ana" {
		t.Errorf("first message attributed to %q, want the participant", records[0].SenderName)
	}
	if records[1].SenderName != "Bruno" {
		t.Errorf("second message attributed to %q, want the participant", records[1].SenderName)
	}
	if records[0].From != "+5511111111111" {
		t.Errorf("from = %q, want the participant's number", records[0].From)
	}
}

// A group runs no automation unless its instance opted in — and the decision is
// the INSTANCE's, which is the whole point.
//
// The gate used to live on the event, where it ran before HandleGroups was ever
// read and made that setting unreachable: turning it on changed nothing.
func TestGroupAttendanceFollowsTheInstanceSetting(t *testing.T) {
	for _, tc := range []struct {
		name         string
		handleGroups bool
		wantAssigned bool
	}{
		{"off by default", false, false},
		{"on when the instance opts in", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newGroupHarness(t, tc.handleGroups).withFreshGate()
			assigned := &recordingAssignments{}
			h.uc.assignments = assigned

			h.deliver(t, groupMessage("5511111111111", "oi"))

			if got := assigned.count() > 0; got != tc.wantAssigned {
				t.Errorf("assigned = %v, want %v (HandleGroups=%v)",
					got, tc.wantAssigned, tc.handleGroups)
			}
		})
	}
}

// A private chat is never affected by the group gate.
func TestPrivateChatsAreAlwaysInScope(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	assigned := &recordingAssignments{}
	h.uc.assignments = assigned

	body, _ := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1",
		"data": map[string]any{
			"messageid": "msg-private", "chatid": "5511999999999@s.whatsapp.net",
			"sender": "5511999999999@s.whatsapp.net", "senderName": "Carla",
			"messageType": "text", "text": "oi",
			"messageTimestamp": time.Now().UnixMilli(),
		},
	})
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if assigned.count() == 0 {
		t.Error("a private chat was not assigned; the group gate leaked onto it")
	}
}

// recordingAssignments counts round-robin assignments.
type recordingAssignments struct {
	mu sync.Mutex
	n  int
}

func (a *recordingAssignments) EnsureAssignment(_, _, _ string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return "user-1"
}

func (a *recordingAssignments) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}
