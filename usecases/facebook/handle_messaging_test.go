package facebook

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	mm "vozko/domain/metamessaging"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/metachannel"
)

const ourAppID = "1234567890"

type recordingHistory struct {
	records []conversation.MessageHistoryRecord
}

func (r *recordingHistory) Record(_ context.Context, rec conversation.MessageHistoryRecord) error {
	r.records = append(r.records, rec)
	return nil
}

type recordingWatermarks struct{ calls []string }

func (r *recordingWatermarks) MarkOutboundStatusUpTo(entryID string, _ shared.EntryType, status conversation.DeliveryStatus, _ time.Time) (int64, error) {
	r.calls = append(r.calls, entryID+":"+string(status))
	return 1, nil
}

type recordingAssignments struct{ entries []string }

func (r *recordingAssignments) EnsureAssignment(entryID, _, _ string) string {
	r.entries = append(r.entries, entryID)
	return ""
}

type recordingEvaluator struct{ events []workflow.TriggerEvent }

func (r *recordingEvaluator) Evaluate(e workflow.TriggerEvent) { r.events = append(r.events, e) }

type recordingReplier struct{ reqs []conversation.AIReplyRequest }

func (r *recordingReplier) Reply(_ context.Context, req conversation.AIReplyRequest) (*conversation.Message, error) {
	r.reqs = append(r.reqs, req)
	return nil, nil
}

type fakeProfiles struct {
	profile *fbdomain.ProfileResult
	err     error
	calls   int
}

func (f *fakeProfiles) GetProfile(context.Context, string, string) (*fbdomain.ProfileResult, error) {
	f.calls++
	return f.profile, f.err
}

type messagingFixture struct {
	uc         *HandleMessagingUseCase
	pages      *fakePages
	contacts   *fakeContacts
	convs      *fakeConversations
	history    *recordingHistory
	watermarks *recordingWatermarks
	assigns    *recordingAssignments
	workflows  *recordingEvaluator
	agents     *recordingReplier
	profiles   *fakeProfiles
	ads        *recordingAds
}

type recordingAds struct{ recorded []*conversation.AdReferral }

func (r *recordingAds) Record(_ context.Context, _ string, _ shared.EntryType, ad *conversation.AdReferral) {
	r.recorded = append(r.recorded, ad)
}

func newMessagingFixture() *messagingFixture {
	agent := "agent-1"
	page := connectedPage("p1", "ws-1")
	page.FBPageID = "PAGE"
	page.AgentID, page.EnableAgentResponses, page.EnableWorkflow = &agent, true, true
	f := &messagingFixture{
		pages: newFakePages(page), contacts: newFakeContacts(), convs: newFakeConversations(),
		history: &recordingHistory{}, watermarks: &recordingWatermarks{}, assigns: &recordingAssignments{},
		workflows: &recordingEvaluator{}, agents: &recordingReplier{},
		profiles: &fakeProfiles{profile: &fbdomain.ProfileResult{Name: "Maria Silva"}},
		ads:      &recordingAds{},
	}
	transcript := &metachannel.Transcript{
		EntryType: shared.EntryTypeFacebook, Channel: conversation.MessageChannelFacebook, Prefix: "facebook", History: f.history, Ads: f.ads,
	}
	f.uc = NewHandleMessagingUseCase(HandleMessagingDeps{
		Pages: f.pages, Contacts: f.contacts, Conversations: f.convs, Profiles: f.profiles,
		Transcript: transcript, Watermarks: f.watermarks, Assignments: f.assigns,
		Automation: conversation_usecase.NewInboundAutomation(f.workflows, f.agents, nil, nil),
		OurAppID:   ourAppID,
	})
	return f
}

func entry(events ...*mm.MessagingEvent) *mm.EntryEnvelope {
	return &mm.EntryEnvelope{Object: "page", Entry: &mm.Entry{ID: "PAGE", Time: 1, Messaging: events}}
}

func standbyEntry(events ...*mm.MessagingEvent) *mm.EntryEnvelope {
	return &mm.EntryEnvelope{Object: "page", Entry: &mm.Entry{ID: "PAGE", Time: 1, Standby: events}}
}

func inbound(mid, text string) *mm.MessagingEvent {
	return &mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 1700000000000,
		Message: &mm.Message{MID: mid, Text: text}}
}

func TestInboundMessageRecordsAssignsEnrichesAndAutomates(t *testing.T) {
	f := newMessagingFixture()
	if err := f.uc.Execute(context.Background(), entry(inbound("m1", "oi"))); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 1 || f.history.records[0].SentBy.Direction() != conversation.MessageDirectionInbound ||
		f.history.records[0].EntryType != shared.EntryTypeFacebook || f.history.records[0].ProviderMessageID != "m1" {
		t.Fatalf("records = %+v", f.history.records)
	}
	if len(f.convs.inbound) != 1 || len(f.assigns.entries) != 1 {
		t.Fatalf("inbound=%v assigns=%v", f.convs.inbound, f.assigns.entries)
	}
	if f.profiles.calls != 1 || f.contacts.byID["contact-PSID"].Name != "Maria Silva" {
		t.Fatalf("profile not enriched: %+v", f.contacts.byID["contact-PSID"])
	}
	if len(f.agents.reqs) != 1 || f.agents.reqs[0].EntryType != shared.EntryTypeFacebook || len(f.workflows.events) == 0 {
		t.Fatalf("automation agents=%d workflows=%d", len(f.agents.reqs), len(f.workflows.events))
	}
	if f.history.records[0].SenderName != "Facebook user PSID" {
		t.Fatalf("sender name recorded before enrichment = %q", f.history.records[0].SenderName)
	}
}

func TestStandbyMessageIsRecordedWithoutAutomation(t *testing.T) {
	f := newMessagingFixture()
	if err := f.uc.Execute(context.Background(), standbyEntry(inbound("m1", "oi"))); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 1 || len(f.agents.reqs) != 0 || len(f.workflows.events) != 0 {
		t.Fatalf("records=%d agents=%d workflows=%d", len(f.history.records), len(f.agents.reqs), len(f.workflows.events))
	}
	if f.convs.byID["conv-contact-PSID"].ThreadOwnerAppID != fbdomain.OtherAppOwner {
		t.Fatalf("owner = %q", f.convs.byID["conv-contact-PSID"].ThreadOwnerAppID)
	}
}

func TestThreadOwnedByBusinessSuiteSilencesAutomation(t *testing.T) {
	f := newMessagingFixture()
	_ = f.uc.Execute(context.Background(), entry(&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 1,
		TakeThreadControl: &mm.ThreadControl{NewOwnerAppID: fbdomain.PageInboxAppID}}))
	if err := f.uc.Execute(context.Background(), standbyEntry(inbound("m2", "oi"))); err != nil {
		t.Fatal(err)
	}
	if len(f.agents.reqs) != 0 || len(f.workflows.events) != 0 {
		t.Fatalf("automation ran on a thread Business Suite owns: agents=%d workflows=%d", len(f.agents.reqs), len(f.workflows.events))
	}
	if len(f.history.records) != 1 {
		t.Fatal("the message must still reach the transcript")
	}
}

func TestPassingControlBackToUsReenablesAutomation(t *testing.T) {
	f := newMessagingFixture()
	ctx := context.Background()
	_ = f.uc.Execute(ctx, entry(&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Timestamp: 1, TakeThreadControl: &mm.ThreadControl{NewOwnerAppID: fbdomain.PageInboxAppID}}))
	_ = f.uc.Execute(ctx, entry(&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Timestamp: 2, PassThreadControl: &mm.ThreadControl{NewOwnerAppID: ourAppID}}))
	_ = f.uc.Execute(ctx, entry(inbound("m3", "oi")))
	if len(f.agents.reqs) != 1 {
		t.Fatalf("agents = %d", len(f.agents.reqs))
	}
}

func TestAMessageDeliveredToUsMeansWeOwnTheThreadAgain(t *testing.T) {
	f := newMessagingFixture()
	ctx := context.Background()
	_ = f.uc.Execute(ctx, standbyEntry(inbound("m1", "oi")))
	if err := f.uc.Execute(ctx, entry(inbound("m2", "voltei"))); err != nil {
		t.Fatal(err)
	}
	if owner := f.convs.byID["conv-contact-PSID"].ThreadOwnerAppID; owner != "" {
		t.Fatalf("owner = %q after a message reached us as the owner", owner)
	}
	if len(f.agents.reqs) != 1 {
		t.Fatalf("agents = %d, want automation back on the thread", len(f.agents.reqs))
	}
}

func TestEchoAttribution(t *testing.T) {
	cases := []struct {
		name      string
		appID     string
		wantOwner string
	}{
		{"our app", ourAppID, ""},
		{"page inbox", fbdomain.PageInboxAppID, fbdomain.PageInboxAppID},
		{"legacy page inbox id", fbdomain.PageInboxAppIDLegacy, fbdomain.PageInboxAppID},
		{"another app", "555", "555"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMessagingFixture()
			yes := true
			echo := &mm.MessagingEvent{Sender: mm.Participant{ID: "PAGE"}, Recipient: mm.Participant{ID: "PSID"}, Timestamp: 5,
				Message: &mm.Message{MID: "e1", Text: "hello", IsEcho: &yes, AppID: jsonNumber(tc.appID)}}
			if err := f.uc.Execute(context.Background(), entry(echo)); err != nil {
				t.Fatal(err)
			}
			if len(f.history.records) != 1 || f.history.records[0].SentBy.Direction() != conversation.MessageDirectionOutbound ||
				f.history.records[0].MessageType != conversation.MessageTypeOperator {
				t.Fatalf("records = %+v", f.history.records)
			}
			if got := f.convs.byID["conv-contact-PSID"].ThreadOwnerAppID; got != tc.wantOwner {
				t.Fatalf("owner = %q, want %q", got, tc.wantOwner)
			}
			if len(f.convs.outbound) != 1 {
				t.Fatal("outbound clock not advanced")
			}
		})
	}
}

func TestOurCaptionEchoIsAlreadyPartOfTheMediaMessage(t *testing.T) {
	f := newMessagingFixture()
	yes := true
	echo := &mm.MessagingEvent{Sender: mm.Participant{ID: "PAGE"}, Recipient: mm.Participant{ID: "PSID"}, Timestamp: 5,
		Message: &mm.Message{MID: "e1", Text: "Segue a foto", IsEcho: &yes, AppID: jsonNumber(ourAppID), Metadata: fbdomain.CaptionMetadata}}
	if err := f.uc.Execute(context.Background(), entry(echo)); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 0 {
		t.Fatalf("caption recorded twice: %+v", f.history.records)
	}
	if len(f.convs.outbound) != 1 {
		t.Fatal("outbound clock not advanced")
	}
}

func TestAnotherAppsCaptionMetadataIsStillRecorded(t *testing.T) {
	f := newMessagingFixture()
	yes := true
	echo := &mm.MessagingEvent{Sender: mm.Participant{ID: "PAGE"}, Recipient: mm.Participant{ID: "PSID"}, Timestamp: 5,
		Message: &mm.Message{MID: "e1", Text: "oi", IsEcho: &yes, AppID: jsonNumber("555"), Metadata: fbdomain.CaptionMetadata}}
	if err := f.uc.Execute(context.Background(), entry(echo)); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 1 {
		t.Fatalf("records = %+v", f.history.records)
	}
}

func TestDeliveryAndReadAdvanceWatermarks(t *testing.T) {
	f := newMessagingFixture()
	ctx := context.Background()
	_ = f.uc.Execute(ctx, entry(inbound("m1", "oi")))
	err := f.uc.Execute(ctx, entry(
		&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 10, Delivery: &mm.Delivery{Watermark: 1700000000500}},
		&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 11, Read: &mm.Read{Watermark: 1700000000600}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.watermarks.calls) != 2 || f.watermarks.calls[0] != "conv-contact-PSID:delivered" || f.watermarks.calls[1] != "conv-contact-PSID:read" {
		t.Fatalf("watermark calls = %v", f.watermarks.calls)
	}
}

func TestWatermarkForUnknownContactIsIgnored(t *testing.T) {
	f := newMessagingFixture()
	err := f.uc.Execute(context.Background(), entry(&mm.MessagingEvent{Sender: mm.Participant{ID: "NEW"}, Timestamp: 10, Read: &mm.Read{Watermark: 5}}))
	if err != nil || len(f.watermarks.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, f.watermarks.calls)
	}
	if len(f.convs.byID) != 0 {
		t.Fatal("a read receipt must not create a conversation")
	}
}

func TestPostbackRecordsAndDispatchesTheSelection(t *testing.T) {
	f := newMessagingFixture()
	err := f.uc.Execute(context.Background(), entry(&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 1,
		Postback: &mm.Postback{MID: "pb1", Title: "Começar", Payload: "GET_STARTED"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 1 || f.history.records[0].Text != "Começar" {
		t.Fatalf("records = %+v", f.history.records)
	}
	if f.workflows.events[0].Data[workflow.DataKeySelectedOptionID] != "GET_STARTED" || len(f.convs.inbound) != 1 {
		t.Fatalf("workflow data = %v", f.workflows.events[0].Data)
	}
}

func TestAMessageFromAnAdCarriesTheAd(t *testing.T) {
	f := newMessagingFixture()
	msg := inbound("m1", "oi")
	msg.Message.Referral = &mm.Referral{Ref: "promo", Source: "ADS", AdID: "42", AdsContextData: &mm.AdsContextData{AdTitle: "Promoção", PhotoURL: "https://scontent/ad.jpg"}}
	if err := f.uc.Execute(context.Background(), entry(msg)); err != nil {
		t.Fatal(err)
	}
	ad := f.history.records[0].AdReferral
	if ad == nil || ad.AdID != "42" || ad.Title != "Promoção" || ad.ImageURL != "https://scontent/ad.jpg" || ad.Platform != conversation.AdPlatformFacebook {
		t.Fatalf("ad = %+v", ad)
	}
}

func TestAnAdClickInAnExistingThreadIsRecordedToo(t *testing.T) {
	f := newMessagingFixture()
	err := f.uc.Execute(context.Background(), entry(&mm.MessagingEvent{Sender: mm.Participant{ID: "PSID"}, Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 1,
		Referral: &mm.Referral{Source: "ADS", AdID: "77", AdsContextData: &mm.AdsContextData{AdTitle: "Black Friday"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ads.recorded) != 1 || f.ads.recorded[0].AdID != "77" {
		t.Fatalf("recorded = %+v", f.ads.recorded)
	}
}

func TestAShortLinkReferralIsNotAnAd(t *testing.T) {
	f := newMessagingFixture()
	msg := inbound("m1", "oi")
	msg.Message.Referral = &mm.Referral{Ref: "bio", Source: "SHORTLINK"}
	if err := f.uc.Execute(context.Background(), entry(msg)); err != nil {
		t.Fatal(err)
	}
	if f.history.records[0].AdReferral != nil {
		t.Fatalf("ad = %+v", f.history.records[0].AdReferral)
	}
}

func TestPolicyBlockRestrictsThePage(t *testing.T) {
	f := newMessagingFixture()
	err := f.uc.Execute(context.Background(), entry(&mm.MessagingEvent{Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 1,
		PolicyEnforcement: &mm.PolicyEnforcement{Action: "block", Reason: "spam"}}))
	if err != nil {
		t.Fatal(err)
	}
	page := f.pages.byID["p1"]
	if page.Status != fbdomain.StatusRestricted || page.PolicyAction != "block" {
		t.Fatalf("page = %+v", page)
	}
	_ = f.uc.Execute(context.Background(), entry(&mm.MessagingEvent{Recipient: mm.Participant{ID: "PAGE"}, Timestamp: 2,
		PolicyEnforcement: &mm.PolicyEnforcement{Action: "unblock"}}))
	if f.pages.byID["p1"].Status != fbdomain.StatusConnected {
		t.Fatal("unblock must restore the page")
	}
}

func TestUnknownPageIsDropped(t *testing.T) {
	f := newMessagingFixture()
	env := &mm.EntryEnvelope{Object: "page", Entry: &mm.Entry{ID: "OTHER", Messaging: []*mm.MessagingEvent{inbound("m1", "x")}}}
	if err := f.uc.Execute(context.Background(), env); !errors.Is(err, ErrUnknownPage) {
		t.Fatalf("got %v", err)
	}
}

func TestBlockedContactIsDropped(t *testing.T) {
	f := newMessagingFixture()
	c, _ := f.contacts.FindOrCreate(context.Background(), "ws-1", "p1", "PSID")
	c.Blocked = true
	if err := f.uc.Execute(context.Background(), entry(inbound("m1", "oi"))); err != nil {
		t.Fatal(err)
	}
	if len(f.history.records) != 0 {
		t.Fatal("blocked contact reached the transcript")
	}
}

func TestProfileFailureStatesAreSticky(t *testing.T) {
	f := newMessagingFixture()
	f.profiles.profile, f.profiles.err = nil, codedErr{code: 2018218}
	if err := f.uc.Execute(context.Background(), entry(inbound("m1", "oi"))); err != nil {
		t.Fatal(err)
	}
	if f.contacts.byID["contact-PSID"].ProfileStatus != fbdomain.ProfileNone {
		t.Fatalf("status = %s", f.contacts.byID["contact-PSID"].ProfileStatus)
	}
	_ = f.uc.Execute(context.Background(), entry(inbound("m2", "oi")))
	if f.profiles.calls != 1 {
		t.Fatalf("profile re-fetched %d times", f.profiles.calls)
	}
}

type codedErr struct{ code int }

func (c codedErr) Error() string                  { return "coded" }
func (c codedErr) ErrorCode() (code, subcode int) { return c.code, 0 }

func jsonNumber(s string) json.Number { return json.Number(s) }
