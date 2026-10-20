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
	seats      map[string][]*uw.Group
	resolved   []string
	resolvedOn []string
}

func (f *fakeGroupSending) Seats(_ context.Context, workspaceID, jid string) ([]*uw.Group, error) {
	var out []*uw.Group
	for _, seat := range f.seats[jid] {
		if seat.WorkspaceID == workspaceID {
			out = append(out, seat)
		}
	}
	return out, nil
}

func (f *fakeGroupSending) Conversation(_ context.Context, instance *uw.Instance, jid, _ string) (*uwuc.Resolved, error) {
	f.resolved = append(f.resolved, jid)
	f.resolvedOn = append(f.resolvedOn, instance.ID)
	return &uwuc.Resolved{
		Conversation: &uw.Conversation{ID: "conv-group-" + instance.ID, InstanceID: instance.ID, ChatID: jid, IsGroup: true},
		Contact:      &uw.Contact{ID: "contact-" + jid, JID: jid, IsGroup: true},
	}, nil
}

func number(id string, status uw.Status) *uw.Instance {
	return &uw.Instance{
		ID: id, WorkspaceID: "ws-1", ServerID: "srv-1", Status: status, DailySendCap: 1000,
		SendDelayMinMS: 500, SendDelayMaxMS: 600,
	}
}

func seatIn(instanceID string, announce, admin bool) *uw.Group {
	return &uw.Group{WorkspaceID: "ws-1", InstanceID: instanceID, JID: groupJID, Subject: "Ofertas VIP #1", Announce: announce, WeAreAdmin: admin}
}

type groupWorld struct {
	groups *fakeGroupSending
	campID string
	entry  string
}

func (h *harness) seedGroup(t *testing.T, pool []string, seats ...*uw.Group) groupWorld {
	t.Helper()
	h.gateway.byID = map[string]*uw.Instance{
		"inst-1": number("inst-1", uw.StatusConnected),
		"inst-2": number("inst-2", uw.StatusConnected),
		"inst-3": number("inst-3", uw.StatusConnected),
	}
	groups := &fakeGroupSending{seats: map[string][]*uw.Group{groupJID: seats}}
	h.consumer.deps.Groups = groups

	h.campaigns.put(&uwc.Campaign{
		ID: "camp-g", WorkspaceID: "ws-1", SenderInstanceIDs: pool, Name: "Ofertas",
		Audience: uwc.AudienceGroups, Status: campaign.StatusRunning,
		Message:        uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oferta de hoje"}},
		SendDelayMinMS: 500, SendDelayMaxMS: 600, EnableWorkflow: true,
	})
	entry := uwc.NewGroupEntry("camp-g", "ws-1", groupJID, "Ofertas VIP #1", time.Now())
	entry.ID = "entry-g"
	h.entries.put(&entry)
	return groupWorld{groups: groups, campID: "camp-g", entry: "entry-g"}
}

func runGroup(h *harness, w groupWorld) campaignqueue.Result {
	return h.consumer.handle(campaignqueue.Message{CampaignID: w.campID, EntryID: w.entry})
}

func TestAGroupEntryIsSentByAPoolNumberThatIsInTheGroup(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1", "inst-2"}, seatIn("inst-2", false, false))

	if got := runGroup(h, w); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}
	if h.sender.count() != 1 || h.sender.last().EntryID != "conv-group-inst-2" {
		t.Fatalf("send went to %+v, want the group conversation of inst-2", h.sender.sent)
	}
	got := h.entries.get(w.entry)
	if got.Status != campaign.SendStatusSent || got.SenderInstanceID != "inst-2" || got.ConversationID != "conv-group-inst-2" {
		t.Fatalf("entry = %+v, want SENT by inst-2", got)
	}
	if len(h.metrics.recorded) != 1 || h.metrics.recorded[0].InstanceID != "inst-2" {
		t.Fatalf("metrics = %+v, want the send counted on inst-2", h.metrics.recorded)
	}
}

func TestAGroupEntryIsNeverSentThroughANumberOutsideThePool(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1"}, seatIn("inst-3", false, false))

	if got := runGroup(h, w); got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	if got := h.entries.get(w.entry); got.Status != campaign.SendStatusFailed || got.ErrorCode != errGroupSeatGone {
		t.Fatalf("entry = %+v, want failed because no campaign number is in the group", got)
	}
	if h.sender.count() != 0 {
		t.Fatalf("sent through a number the creator could not use")
	}
}

func TestAGroupEntryGoesToTheLeastLoadedNumber(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1", "inst-2"}, seatIn("inst-1", false, false), seatIn("inst-2", false, false))
	for i := 0; i < 5; i++ {
		_, _ = h.consumer.deps.Budget.TryConsumeDaily("inst-1", 1000)
	}

	runGroup(h, w)

	if got := h.entries.get(w.entry); got.SenderInstanceID != "inst-2" {
		t.Fatalf("sender = %q, want the less used inst-2", got.SenderInstanceID)
	}
}

func TestAGroupEntryUsesTheNextNumberWhileTheFirstIsPacing(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1", "inst-2"}, seatIn("inst-1", false, false), seatIn("inst-2", false, false))
	_ = h.shared.SetString(paceLeaseKey("inst-1"), "1", time.Minute)

	if got := runGroup(h, w); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done through the free number", got.Outcome)
	}
	if got := h.entries.get(w.entry); got.SenderInstanceID != "inst-2" {
		t.Fatalf("sender = %q, want inst-2 while inst-1 paces", got.SenderInstanceID)
	}
	if used := h.consumer.deps.Budget.UsedToday("inst-1"); used != 0 {
		t.Fatalf("inst-1 daily budget = %d, want the unused reservation released", used)
	}
}

func TestAGroupEntryWaitsWhileEveryEligibleNumberIsPacing(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1", "inst-2"}, seatIn("inst-1", false, false), seatIn("inst-2", false, false))
	_ = h.shared.SetString(paceLeaseKey("inst-1"), "1", time.Minute)
	_ = h.shared.SetString(paceLeaseKey("inst-2"), "1", time.Minute)

	got := runGroup(h, w)

	if got.Outcome != campaignqueue.OutcomeRetryLater || got.Delay <= 0 {
		t.Fatalf("outcome = %+v, want a retry after the pacing wait", got)
	}
	if h.sender.count() != 0 || h.entries.get(w.entry).Status != campaign.SendStatusPending {
		t.Fatalf("a paced group entry was sent or lost its pending status")
	}
	if h.consumer.deps.Budget.UsedToday("inst-1")+h.consumer.deps.Budget.UsedToday("inst-2") != 0 {
		t.Fatalf("daily budget kept for a send that never happened")
	}
}

func TestAGroupWhereNoPoolNumberMayPostFailsWithItsReason(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1"}, seatIn("inst-1", true, false))

	if got := runGroup(h, w); got.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %v, want Drop", got.Outcome)
	}
	if got := h.entries.get(w.entry); got.Status != campaign.SendStatusFailed || got.ErrorCode != errGroupCannotPost {
		t.Fatalf("entry = %+v, want failed with the cannot-post code", got)
	}
	if h.sender.count() != 0 {
		t.Fatalf("sent into a group no campaign number may post in")
	}
}

func TestAnAdminNumberPostsWhereOnlyAdminsPost(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1", "inst-2"}, seatIn("inst-1", true, false), seatIn("inst-2", true, true))

	runGroup(h, w)

	if got := h.entries.get(w.entry); got.Status != campaign.SendStatusSent || got.SenderInstanceID != "inst-2" {
		t.Fatalf("entry = %+v, want SENT by the admin number", got)
	}
}

func TestAGroupWhoseNumbersAreOfflineWaitsForThem(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1"}, seatIn("inst-1", false, false))
	h.gateway.byID["inst-1"] = number("inst-1", uw.StatusDisconnected)

	got := runGroup(h, w)

	if got.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("outcome = %v, want a retry while the number is offline", got.Outcome)
	}
	if status := h.entries.get(w.entry).Status; status != campaign.SendStatusPending || h.sender.count() != 0 {
		t.Fatalf("status = %q sends %d, want the entry kept pending and unsent", status, h.sender.count())
	}
}

func TestAGroupEntrySkipsThePersonRules(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1"}, seatIn("inst-1", false, false))

	runGroup(h, w)

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

func TestAnOlderGroupCampaignSendsThroughItsOwnNumber(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, nil, seatIn("inst-1", false, false), seatIn("inst-2", false, false))
	stored, _ := h.campaigns.FindByID(w.campID)
	stored.InstanceID = "inst-1"
	h.campaigns.put(stored)
	for i := 0; i < 5; i++ {
		_, _ = h.consumer.deps.Budget.TryConsumeDaily("inst-1", 1000)
	}

	runGroup(h, w)

	if got := h.entries.get(w.entry); got.SenderInstanceID != "inst-1" {
		t.Fatalf("sender = %q, want the number the campaign was created with", got.SenderInstanceID)
	}
}

func TestAPoolNumberOfAnotherWorkspaceIsIgnored(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1"}, seatIn("inst-1", false, false))
	other := number("inst-1", uw.StatusConnected)
	other.WorkspaceID = "ws-2"
	h.gateway.byID["inst-1"] = other

	runGroup(h, w)

	if h.sender.count() != 0 || h.entries.get(w.entry).Status == campaign.SendStatusSent {
		t.Fatalf("sent through a number of another workspace")
	}
}

func TestAGroupEntryWaitsWhenGroupSendingIsNotWired(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-1"}, seatIn("inst-1", false, false))
	h.consumer.deps.Groups = nil

	got := runGroup(h, w)

	if got.Outcome == campaignqueue.OutcomeDone || h.sender.count() != 0 {
		t.Fatalf("outcome = %v sends %d, want a held entry and no send", got.Outcome, h.sender.count())
	}
	if status := h.entries.get(w.entry).Status; status != campaign.SendStatusPending {
		t.Fatalf("status = %q, want the entry kept pending", status)
	}
}

func TestAGroupCampaignWithoutAUsableNumberDoesNotStart(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-9"}, seatIn("inst-1", false, false))

	if err := h.consumer.precheck(w.campID); err == nil {
		t.Fatalf("precheck passed for a campaign none of whose numbers exists")
	}
	if got, _ := h.campaigns.FindByID(w.campID); got.Status != campaign.StatusStopped || h.campaigns.reason(w.campID) == "" {
		t.Fatalf("campaign = %+v reason %q, want it stopped with a reason", got, h.campaigns.reason(w.campID))
	}
}

func TestAGroupCampaignWithAPoolNumberPassesThePrecheck(t *testing.T) {
	h := newHarness(t)
	w := h.seedGroup(t, []string{"inst-9", "inst-2"}, seatIn("inst-2", false, false))

	if err := h.consumer.precheck(w.campID); err != nil {
		t.Fatalf("precheck = %v, want a campaign with one usable number to run", err)
	}
}
