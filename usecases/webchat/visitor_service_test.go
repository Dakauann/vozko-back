package webchat

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"crypto/sha256"
	"math/bits"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
)

const siteOrigin = "https://loja.example.com"

type harness struct {
	svc           *VisitorService
	widget        *wcdomain.Widget
	visitors      *fakeVisitors
	conversations *fakeConversations
	oneShot       *fakeOneShot
	transcript    *fakeTranscript
	assignments   *fakeAssignments
	automation    *fakeAutomation
	events        *fakeEvents
	leads         *fakeLeads
	keys          wcdomain.Keys
	now           time.Time
}

func newHarness(t *testing.T, mutate func(*wcdomain.Widget)) *harness {
	t.Helper()
	keys, err := wcdomain.DeriveKeys("test-root")
	if err != nil {
		t.Fatal(err)
	}
	w := &wcdomain.Widget{ID: "widget-1", WorkspaceID: "ws-1", Name: "Loja", PublicKey: "pk-1", AllowedOrigins: []string{siteOrigin}}
	if mutate != nil {
		mutate(w)
	}
	w.Normalize()
	h := &harness{
		widget:        w,
		visitors:      &fakeVisitors{byID: map[string]*wcdomain.Visitor{}},
		conversations: &fakeConversations{byID: map[string]*wcdomain.Conversation{}},
		oneShot:       &fakeOneShot{keys: map[string]bool{}},
		transcript:    &fakeTranscript{},
		assignments:   &fakeAssignments{},
		automation:    &fakeAutomation{},
		events:        &fakeEvents{},
		leads:         &fakeLeads{},
		keys:          keys,
		now:           time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}
	h.svc = NewVisitorService(VisitorDeps{
		Widgets:       &fakeWidgets{byID: map[string]*wcdomain.Widget{w.ID: w}},
		Visitors:      h.visitors,
		Conversations: h.conversations,
		Keys:          keys,
		OneShot:       h.oneShot,
		Limits:        openLimits(),
		Leads:         h.leads,
		Transcript:    h.transcript,
		Messages:      fakeMessages{},
		Presenter:     noPresenter{},
		Media:         &fakeMedia{},
		Assignments:   h.assignments,
		Automation:    h.automation,
		Events:        h.events,
		Operators:     &fakeOperators{},
		Now:           func() time.Time { return h.now },
		Async:         func(fn func()) { fn() },
	})
	return h
}

func (h *harness) solvedChallenge(t *testing.T) (string, string) {
	t.Helper()
	c, err := h.svc.Challenge(context.Background(), h.widget.PublicKey, siteOrigin)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; ; n++ {
		nonce := strconv.Itoa(n)
		sum := sha256.Sum256([]byte(c.Token + ":" + nonce))
		if zeroBits(sum[:]) >= c.Bits {
			return c.Token, nonce
		}
	}
}

func zeroBits(digest []byte) int {
	total := 0
	for _, b := range digest {
		if b == 0 {
			total += 8
			continue
		}
		return total + bits.LeadingZeros8(b)
	}
	return total
}

func (h *harness) session(t *testing.T) *Session {
	t.Helper()
	token, nonce := h.solvedChallenge(t)
	view, err := h.svc.StartSession(context.Background(), StartSessionInput{
		PublicKey: h.widget.PublicKey, ChallengeToken: token, Nonce: nonce,
		Client: Client{ParentOrigin: siteOrigin, IP: "203.0.113.7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := h.svc.Authenticate(context.Background(), view.Token, "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestChallengeIsRefusedToSitesOutsideTheAllowList(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.svc.Challenge(context.Background(), h.widget.PublicKey, "https://evil.example"); !errors.Is(err, wcdomain.ErrOriginNotAllowed) {
		t.Fatalf("Challenge from a foreign site = %v", err)
	}
}

func TestPausedWidgetServesNothing(t *testing.T) {
	h := newHarness(t, func(w *wcdomain.Widget) { w.Status = wcdomain.StatusPaused })
	if _, err := h.svc.Challenge(context.Background(), h.widget.PublicKey, siteOrigin); !errors.Is(err, wcdomain.ErrWidgetPaused) {
		t.Fatalf("Challenge on a paused widget = %v", err)
	}
}

func TestAnonymousSessionNeedsASolvedChallengeUsedOnce(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	if _, err := h.svc.StartSession(ctx, StartSessionInput{PublicKey: h.widget.PublicKey, Client: Client{ParentOrigin: siteOrigin}}); !errors.Is(err, wcdomain.ErrChallengeInvalid) {
		t.Fatalf("session without proof of work = %v", err)
	}

	token, nonce := h.solvedChallenge(t)
	in := StartSessionInput{PublicKey: h.widget.PublicKey, ChallengeToken: token, Nonce: nonce, Client: Client{ParentOrigin: siteOrigin}}
	if _, err := h.svc.StartSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartSession(ctx, in); !errors.Is(err, wcdomain.ErrChallengeReused) {
		t.Fatalf("replayed challenge = %v", err)
	}
	if len(h.visitors.byID) != 1 {
		t.Fatalf("visitors created = %d, want 1", len(h.visitors.byID))
	}
}

func TestChallengeStoreFailureRefusesTheSession(t *testing.T) {
	h := newHarness(t, nil)
	token, nonce := h.solvedChallenge(t)
	h.oneShot.err = errors.New("redis down")
	_, err := h.svc.StartSession(context.Background(), StartSessionInput{PublicKey: h.widget.PublicKey, ChallengeToken: token, Nonce: nonce, Client: Client{ParentOrigin: siteOrigin}})
	if !errors.Is(err, wcdomain.ErrChallengeReused) {
		t.Fatalf("store failure = %v, want a refusal", err)
	}
}

func TestLimiterErrorsRefuseInsteadOfAllowing(t *testing.T) {
	if err := allow(fakeLimiter{allowed: true, err: errors.New("redis down")}, "k"); !errors.Is(err, wcdomain.ErrRateLimited) {
		t.Fatalf("limiter error = %v", err)
	}
	if err := allow(nil, "k"); !errors.Is(err, wcdomain.ErrRateLimited) {
		t.Fatalf("missing limiter = %v", err)
	}
	if err := allow(fakeLimiter{allowed: false}, "k"); !errors.Is(err, wcdomain.ErrRateLimited) {
		t.Fatalf("over the limit = %v", err)
	}
}

func TestResumingWithATokenSkipsTheChallengeAndKeepsTheVisitor(t *testing.T) {
	h := newHarness(t, nil)
	first := h.session(t)
	token, _ := wcdomain.NewVisitorToken(h.keys, wcdomain.VisitorGrant{WidgetID: h.widget.ID, VisitorID: first.Visitor.ID}, h.now)
	view, err := h.svc.StartSession(context.Background(), StartSessionInput{PublicKey: h.widget.PublicKey, Token: token, Client: Client{ParentOrigin: siteOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := h.svc.Authenticate(context.Background(), view.Token, "")
	if again.Visitor.ID != first.Visitor.ID {
		t.Fatal("a valid token must resume the same visitor")
	}
}

func TestRequiredIdentityRefusesAnonymousVisitors(t *testing.T) {
	h := newHarness(t, func(w *wcdomain.Widget) {
		w.IdentityMode, w.IdentitySecret = wcdomain.IdentityRequired, "secret"
	})
	token, nonce := h.solvedChallenge(t)
	_, err := h.svc.StartSession(context.Background(), StartSessionInput{PublicKey: h.widget.PublicKey, ChallengeToken: token, Nonce: nonce, Client: Client{ParentOrigin: siteOrigin}})
	if !errors.Is(err, wcdomain.ErrIdentityRequired) {
		t.Fatalf("anonymous on a verified-only widget = %v", err)
	}
}

func TestAuthenticateRevokesBlockedVisitorsAndForeignTokens(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)
	token, _ := wcdomain.NewVisitorToken(h.keys, wcdomain.VisitorGrant{WidgetID: h.widget.ID, VisitorID: s.Visitor.ID}, h.now)

	h.visitors.byID[s.Visitor.ID].Blocked = true
	if _, err := h.svc.Authenticate(context.Background(), token, ""); !errors.Is(err, wcdomain.ErrVisitorBlocked) {
		t.Fatalf("blocked visitor = %v", err)
	}

	foreign, _ := wcdomain.NewVisitorToken(h.keys, wcdomain.VisitorGrant{WidgetID: "other-widget", VisitorID: s.Visitor.ID}, h.now)
	if _, err := h.svc.Authenticate(context.Background(), foreign, ""); !errors.Is(err, wcdomain.ErrVisitorTokenInvalid) {
		t.Fatalf("token for another widget = %v", err)
	}
}

func TestRequiredIntakeBlocksTheFirstMessage(t *testing.T) {
	h := newHarness(t, func(w *wcdomain.Widget) { w.IntakeEmail = wcdomain.FieldRequired })
	s := h.session(t)
	_, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", Text: "oi"})
	if !errors.Is(err, wcdomain.ErrIntakePending) {
		t.Fatalf("message before intake = %v", err)
	}
	if len(h.transcript.records) != 0 {
		t.Fatal("nothing may be recorded before the intake")
	}
}

func TestIntakeWithAPhoneLinksTheLead(t *testing.T) {
	h := newHarness(t, func(w *wcdomain.Widget) { w.IntakePhone = wcdomain.FieldRequired })
	s := h.session(t)
	state, err := h.svc.SubmitIntake(context.Background(), s, wcdomain.IntakeAnswers{Phone: "(11) 99999-0000"})
	if err != nil {
		t.Fatal(err)
	}
	v := h.visitors.byID[s.Visitor.ID]
	if v.LeadID == nil || *v.LeadID != "lead-5511999990000" || state.IntakeRequired {
		t.Fatalf("lead link = %v, state = %+v", v.LeadID, state)
	}
}

func TestSendRecordsAssignsAndDispatchesTheWidgetAutomation(t *testing.T) {
	agentID := "agent-1"
	h := newHarness(t, func(w *wcdomain.Widget) { w.AgentID, w.EnableAgentResponses = &agentID, true })
	s := h.session(t)
	msg, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", Text: " olá "})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "olá" || len(h.transcript.records) != 1 || len(h.assignments.ensured) != 1 {
		t.Fatalf("message %+v records %d ensured %d", msg, len(h.transcript.records), len(h.assignments.ensured))
	}
	rec := h.transcript.records[0]
	if rec.EntryType != shared.EntryTypeWebchat || rec.Channel != conversation.MessageChannelWebchat || !rec.SentBy.IsContact() {
		t.Fatalf("record = %+v", rec)
	}
	if len(h.automation.inputs) != 1 || h.automation.inputs[0].Config.AgentID == nil || !h.automation.inputs[0].Config.EnableAgentResponses {
		t.Fatalf("automation = %+v", h.automation.inputs)
	}
}

func TestADoubleSubmitRunsTheAutomationOnce(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)
	for i := 0; i < 2; i++ {
		if _, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", Text: "oi"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.transcript.records) != 1 || len(h.automation.inputs) != 1 {
		t.Fatalf("records %d dispatches %d, want 1 and 1", len(h.transcript.records), len(h.automation.inputs))
	}
}

func TestAFailedRecordLetsTheVisitorRetry(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)
	h.transcript.err = errors.New("db down")
	if _, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", Text: "oi"}); err == nil {
		t.Fatal("expected the failure to surface")
	}
	h.transcript.err = nil
	if _, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", Text: "oi"}); err != nil {
		t.Fatal(err)
	}
	if len(h.transcript.records) != 1 {
		t.Fatal("the retry must be recorded")
	}
}

func TestSelectionMustBeAnOfferedOption(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)
	conv, _ := h.conversations.FindOrCreate(context.Background(), wcdomain.FindOrCreateConversationInput{WidgetID: h.widget.ID, VisitorID: s.Visitor.ID})
	conv.PendingOptions = []wcdomain.Option{{ID: "opt-sales", Title: "Vendas"}}

	if _, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", SelectionID: "opt-admin"}); !errors.Is(err, wcdomain.ErrSelectionNotOffered) {
		t.Fatalf("forged option = %v", err)
	}
	msg, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0002", SelectionID: "opt-sales"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "Vendas" || h.automation.inputs[0].Selection == nil || h.automation.inputs[0].Selection.ID != "opt-sales" {
		t.Fatalf("selection = %+v", h.automation.inputs)
	}
	if len(conv.PendingOptions) != 0 {
		t.Fatal("options must be cleared once answered")
	}
}

func TestUploadsAreRefusedUnlessTheWidgetAllowsThem(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)
	if _, err := h.svc.Upload(context.Background(), s, UploadInput{ClientMessageID: "client-0001", Data: []byte("%PDF-1.4")}); !errors.Is(err, wcdomain.ErrAttachmentsDisabled) {
		t.Fatalf("upload on a widget without attachments = %v", err)
	}
}

func TestUploadsAreSniffedNotTrusted(t *testing.T) {
	h := newHarness(t, func(w *wcdomain.Widget) { w.AllowAttachments = true })
	s := h.session(t)
	_, err := h.svc.Upload(context.Background(), s, UploadInput{ClientMessageID: "client-0001", FileName: "foto.png", Data: []byte("<html><script>alert(1)</script>")})
	if !errors.Is(err, wcdomain.ErrAttachmentTypeRefused) {
		t.Fatalf("html named .png = %v", err)
	}
}

func TestHistoryHidesInternalRows(t *testing.T) {
	h := newHarness(t, nil)
	s := h.session(t)
	conv, _ := h.conversations.FindOrCreate(context.Background(), wcdomain.FindOrCreateConversationInput{WidgetID: h.widget.ID, VisitorID: s.Visitor.ID})
	h.svc.Messages = fakeMessages{rows: []*conversation.Message{
		{ID: "3", EntryID: conv.ID, MessageType: conversation.MessageTypeToolResult, Text: "{\"lead\":\"x\"}"},
		{ID: "2", EntryID: conv.ID, MessageType: conversation.MessageTypeAIResponse, Text: "Como posso ajudar?", SentBy: conversation.SentByAI("a")},
		{ID: "1", EntryID: conv.ID, MessageType: conversation.MessageTypeUserMessage, Text: "oi", SentBy: conversation.SentByContact(s.Visitor.ID)},
	}}
	view, err := h.svc.History(context.Background(), s, HistoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Messages) != 2 || view.Messages[0].Text != "oi" || view.Messages[1].Author != wcdomain.AuthorAssistant {
		t.Fatalf("history = %+v", view.Messages)
	}
}

func TestRequestHumanFollowsTheWidgetSettingAndDepartment(t *testing.T) {
	dept := "dept-1"
	h := newHarness(t, nil)
	s := h.session(t)
	if err := h.svc.RequestHuman(context.Background(), s); !errors.Is(err, wcdomain.ErrHumanRequestDisabled) {
		t.Fatalf("hand-off on a widget without it = %v", err)
	}

	h = newHarness(t, func(w *wcdomain.Widget) { w.AllowHumanRequest, w.DepartmentID = true, &dept })
	s = h.session(t)
	if _, err := h.svc.Send(context.Background(), s, SendInput{ClientMessageID: "client-0001", Text: "oi"}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RequestHuman(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if len(h.assignments.handoffs) != 1 || h.assignments.handoffs[0].DepartmentID != dept {
		t.Fatalf("handoffs = %+v", h.assignments.handoffs)
	}
}

func TestAcceptedUploadIsStoredUnderTheSniffedType(t *testing.T) {
	h := newHarness(t, func(w *wcdomain.Widget) { w.AllowAttachments = true })
	media := &fakeMedia{}
	h.svc.Media = media
	s := h.session(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	msg, err := h.svc.Upload(context.Background(), s, UploadInput{ClientMessageID: "client-0001", FileName: "../../etc/passwd.exe", Data: png})
	if err != nil {
		t.Fatal(err)
	}
	if len(media.stored) != 1 || media.stored[0].MimeType != "image/png" || !strings.HasSuffix(media.stored[0].Key, ".png") {
		t.Fatalf("stored = %+v", media.stored)
	}
	if strings.Contains(media.stored[0].OriginalFilename, "/") || msg.Media == nil || msg.Media.Kind != "image" {
		t.Fatalf("file name %q, message %+v", media.stored[0].OriginalFilename, msg)
	}
}
