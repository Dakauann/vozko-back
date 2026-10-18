package unofficial_whatsapp_campaign

import (
	"context"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignqueue"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

const groupJID = "120363000000000001@g.us"

type fakeGroupSending struct {
	seats    map[string]*uw.Group
	resolved []string
}

func (f *fakeGroupSending) Seat(_ context.Context, instanceID, jid string) (*uw.Group, error) {
	if g, ok := f.seats[instanceID+"|"+jid]; ok {
		return g, nil
	}
	return nil, uw.ErrGroupNotFound
}

func (f *fakeGroupSending) Conversation(_ context.Context, instance *uw.Instance, jid, _ string) (*uwuc.Resolved, error) {
	f.resolved = append(f.resolved, jid)
	return &uwuc.Resolved{
		Conversation: &uw.Conversation{ID: "conv-group-" + jid, InstanceID: instance.ID, ChatID: jid, IsGroup: true},
		Contact:      &uw.Contact{ID: "contact-" + jid, JID: jid, IsGroup: true},
	}, nil
}

func (h *harness) seedGroup(t *testing.T, seat *uw.Group) (*fakeGroupSending, string, string) {
	t.Helper()
	groups := &fakeGroupSending{seats: map[string]*uw.Group{}}
	if seat != nil {
		groups.seats["inst-1|"+groupJID] = seat
	}
	h.consumer.deps.Groups = groups

	h.campaigns.put(&uwc.Campaign{
		ID: "camp-g", WorkspaceID: "ws-1", InstanceID: "inst-1", Name: "Ofertas",
		Audience: uwc.AudienceGroups, Status: campaign.StatusRunning,
		Message:        uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oferta de hoje"}},
		SendDelayMinMS: 500, SendDelayMaxMS: 600, EnableWorkflow: true,
	})
	entry := uwc.NewGroupEntry("camp-g", "ws-1", groupJID, "Ofertas VIP #1", time.Now())
	entry.ID = "entry-g"
	h.entries.put(&entry)
	return groups, "camp-g", "entry-g"
}

func runGroup(h *harness, campID, entryID string) campaignqueue.Result {
	return h.consumer.handle(campaignqueue.Message{CampaignID: campID, EntryID: entryID})
}

func TestAGroupEntryIsSentIntoTheGroupConversation(t *testing.T) {
	h := newHarness(t)
	groups, campID, entryID := h.seedGroup(t, &uw.Group{InstanceID: "inst-1", JID: groupJID, Subject: "Ofertas VIP #1"})

	if got := runGroup(h, campID, entryID); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}
	if h.sender.count() != 1 || h.sender.last().EntryID != "conv-group-"+groupJID {
		t.Fatalf("send went to %+v, want the group conversation", h.sender.sent)
	}
	if got := h.entries.get(entryID); got.Status != campaign.SendStatusSent || got.ConversationID != "conv-group-"+groupJID {
		t.Fatalf("entry = %+v, want SENT in the group conversation", got)
	}
	if len(groups.resolved) != 1 {
		t.Fatalf("group conversation resolved %d times", len(groups.resolved))
	}
	if len(h.metrics.recorded) != 1 {
		t.Fatalf("metrics = %d, want the send counted like any other", len(h.metrics.recorded))
	}
}

func TestAGroupEntrySkipsThePersonRules(t *testing.T) {
	h := newHarness(t)
	_, campID, entryID := h.seedGroup(t, &uw.Group{InstanceID: "inst-1", JID: groupJID})

	runGroup(h, campID, entryID)

	if h.spam.checks != 0 || h.spam.claims != 0 || len(h.spam.recorded) != 0 {
		t.Fatalf("lead eligibility ran for a group: checks %d claims %d recorded %v", h.spam.checks, h.spam.claims, h.spam.recorded)
	}
	if h.gateway.checkCalls != 0 || len(h.gateway.resolved) != 0 {
		t.Fatalf("a group was checked or resolved as a phone number")
	}
	if len(h.workflows.fired) != 0 || len(h.assigner.calls) != 0 {
		t.Fatalf("a group send fired person automations: workflows %d assignments %d", len(h.workflows.fired), len(h.assigner.calls))
	}
}

func TestAGroupWhereTheNumberCannotPostFailsWithItsReason(t *testing.T) {
	h := newHarness(t)
	_, campID, entryID := h.seedGroup(t, &uw.Group{InstanceID: "inst-1", JID: groupJID, Announce: true})

	if got := runGroup(h, campID, entryID); got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	got := h.entries.get(entryID)
	if got.Status != campaign.SendStatusFailed || got.ErrorCode != errGroupCannotPost {
		t.Fatalf("entry = %+v, want failed with the cannot-post code", got)
	}
	if h.sender.count() != 0 {
		t.Fatalf("sent into a group the number cannot post in")
	}
}

func TestAGroupTheNumberLeftFailsWithItsReason(t *testing.T) {
	h := newHarness(t)
	_, campID, entryID := h.seedGroup(t, nil)

	runGroup(h, campID, entryID)

	if got := h.entries.get(entryID); got.Status != campaign.SendStatusFailed || got.ErrorCode != errGroupSeatGone {
		t.Fatalf("entry = %+v, want failed because the number left the group", got)
	}
	if h.sender.count() != 0 {
		t.Fatalf("sent into a group the number left")
	}
}

func TestAGroupEntryWaitsWhenGroupSendingIsNotWired(t *testing.T) {
	h := newHarness(t)
	_, campID, entryID := h.seedGroup(t, &uw.Group{InstanceID: "inst-1", JID: groupJID})
	h.consumer.deps.Groups = nil

	got := runGroup(h, campID, entryID)

	if got.Outcome == campaignqueue.OutcomeDone || h.sender.count() != 0 {
		t.Fatalf("outcome = %v sends %d, want a held entry and no send", got.Outcome, h.sender.count())
	}
	if status := h.entries.get(entryID).Status; status != campaign.SendStatusPending {
		t.Fatalf("status = %q, want the entry kept pending", status)
	}
}
