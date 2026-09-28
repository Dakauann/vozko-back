package facebook

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

type fakeMessaging struct {
	sent      []fbdomain.OutboundMessage
	sendErrs  []error
	uploads   []fbdomain.UploadInput
	actions   []fbdomain.SenderAction
	reactions []string
}

func (f *fakeMessaging) Send(_ context.Context, _, _ string, msg fbdomain.OutboundMessage) (*fbdomain.SendResult, error) {
	f.sent = append(f.sent, msg)
	if len(f.sendErrs) > 0 {
		err := f.sendErrs[0]
		f.sendErrs = f.sendErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	recipient := msg.Recipient.PSID
	if msg.Recipient.CommentID != "" {
		recipient = "psid-9"
	}
	return &fbdomain.SendResult{RecipientID: recipient, MessageID: "m_" + string(rune('0'+len(f.sent)))}, nil
}
func (f *fakeMessaging) SendAction(_ context.Context, _, _, _ string, action fbdomain.SenderAction) error {
	f.actions = append(f.actions, action)
	return nil
}
func (f *fakeMessaging) React(_ context.Context, _, _, _, mid, reaction string) error {
	f.reactions = append(f.reactions, mid+":"+reaction)
	return nil
}
func (f *fakeMessaging) Unreact(_ context.Context, _, _, _, mid string) error {
	f.reactions = append(f.reactions, mid+":")
	return nil
}
func (f *fakeMessaging) Upload(_ context.Context, _, _ string, in fbdomain.UploadInput) (string, error) {
	f.uploads = append(f.uploads, in)
	return "att-1", nil
}
func (f *fakeMessaging) GetProfile(context.Context, string, string) (*fbdomain.ProfileResult, error) {
	return nil, nil
}
func (f *fakeMessaging) FetchBytes(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}

type fakeRouting struct {
	owner    string
	taken    []string
	released []string
	takeErr  error
}

func (f *fakeRouting) ThreadOwner(context.Context, string, string, string) (string, error) {
	return f.owner, nil
}
func (f *fakeRouting) TakeControl(_ context.Context, _, _, psid, metadata string) error {
	f.taken = append(f.taken, psid+"|"+metadata)
	return f.takeErr
}
func (f *fakeRouting) ReleaseControl(_ context.Context, _, _, psid string) error {
	f.released = append(f.released, psid)
	return nil
}

type adapterFixture struct {
	adapter   *ChannelAdapter
	pages     *fakePages
	contacts  *fakeContacts
	convs     *fakeConversations
	messaging *fakeMessaging
	routing   *fakeRouting
	ec        *conversation.EntryContext
}

func messagingPage() *fbdomain.Page {
	return &fbdomain.Page{
		ID: "page-1", WorkspaceID: "ws-1", FBPageID: "fb-page-1", PageToken: "page-token",
		Status:        fbdomain.StatusConnected,
		GrantedScopes: []string{fbdomain.ScopeMessaging, fbdomain.ScopeManageMetadata},
		Tasks:         []fbdomain.Task{fbdomain.TaskMessaging},
	}
}

func newAdapterFixture(t *testing.T, humanAgentApproved bool, lastInboundAgo time.Duration) *adapterFixture {
	t.Helper()
	f := &adapterFixture{
		pages:     newFakePages(messagingPage()),
		contacts:  newFakeContacts(),
		convs:     newFakeConversations(),
		messaging: &fakeMessaging{},
		routing:   &fakeRouting{},
	}
	contact, _ := f.contacts.FindOrCreate(context.Background(), "ws-1", "page-1", "psid-1")
	conv, _ := f.convs.FindOrCreate(context.Background(), "ws-1", "page-1", contact.ID)
	last := time.Now().UTC().Add(-lastInboundAgo)
	conv.LastCustomerMessageAt = &last

	f.adapter = NewChannelAdapter(ChannelAdapterDeps{
		Pages: f.pages, Contacts: f.contacts, Conversations: f.convs,
		Messaging: f.messaging, Routing: f.routing, HumanAgentApproved: humanAgentApproved,
	})
	ec, err := f.adapter.ResolveEntry(context.Background(), conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.ec = ec
	return f
}

func TestResolveEntryCarriesThePageAndTheCustomer(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	if f.ec.AccountID != "page-1" || f.ec.ContactRef != "psid-1" || f.ec.WorkspaceID != "ws-1" || f.ec.LastInboundAt == nil {
		t.Fatalf("entry context = %+v", f.ec)
	}
}

func TestOperatorTextInsideTheWindowGoesOutAsAResponse(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)

	out, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "Olá!", HumanInitiated: true, ReplyToProviderMessageID: "m_in"})
	if err != nil {
		t.Fatal(err)
	}
	msg := f.messaging.sent[0]
	if msg.Tier != fbdomain.SendStandard || msg.Recipient.PSID != "psid-1" || msg.Metadata != "vozko:operator" || msg.ReplyToMID != "m_in" {
		t.Fatalf("message = %+v", msg)
	}
	if out.ProviderMessageID == "" || len(f.convs.outbound) != 1 {
		t.Fatalf("outcome %+v, outbound %v", out, f.convs.outbound)
	}
}

func TestAutomationNeverReachesTheHumanAgentTier(t *testing.T) {
	f := newAdapterFixture(t, true, 48*time.Hour)

	_, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("got %v", err)
	}
	if len(f.messaging.sent) != 0 {
		t.Fatal("automation sent outside the standard window")
	}
}

func TestApprovedOperatorAfterADayUsesTheHumanAgentTag(t *testing.T) {
	f := newAdapterFixture(t, true, 48*time.Hour)

	if _, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true}); err != nil {
		t.Fatal(err)
	}
	if f.messaging.sent[0].Tier != fbdomain.SendHumanAgent {
		t.Fatalf("tier = %q", f.messaging.sent[0].Tier)
	}
}

func TestUnapprovedHumanAgentKeepsTheWindowClosed(t *testing.T) {
	f := newAdapterFixture(t, false, 48*time.Hour)

	_, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestWindowStateShowsTheOperatorTier(t *testing.T) {
	f := newAdapterFixture(t, true, 48*time.Hour)

	state, err := f.adapter.WindowState(context.Background(), f.ec)
	if err != nil || !state.Open || state.Tier != channel.WindowTierHuman {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestTextOverTheRuneLimitIsRefusedBeforeSending(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)

	_, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: strings.Repeat("ç", fbdomain.MaxTextRunes+1), HumanInitiated: true})
	if !errors.Is(err, fbdomain.ErrTextTooLong) || len(f.messaging.sent) != 0 {
		t.Fatalf("got %v with %d sends", err, len(f.messaging.sent))
	}
}

func TestOperatorTakesTheThreadBackAndRetriesOnce(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.convs.byID[f.ec.EntryID].ThreadOwnerAppID = fbdomain.PageInboxAppID
	f.messaging.sendErrs = []error{&meta.Error{Code: meta.CodeThreadOwned}, nil}

	if _, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.routing.taken) != 1 || f.routing.taken[0] != "psid-1|vozko:operator" {
		t.Fatalf("take control = %v", f.routing.taken)
	}
	if len(f.messaging.sent) != 2 {
		t.Fatalf("sends = %d, want the original and one retry", len(f.messaging.sent))
	}
	if owner := f.convs.byID[f.ec.EntryID].ThreadOwnerAppID; owner != "" {
		t.Fatalf("thread owner still %q after taking control", owner)
	}
}

func TestOperatorRetryHappensOnlyOnce(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.messaging.sendErrs = []error{&meta.Error{Code: meta.CodeThreadOwned}, &meta.Error{Code: meta.CodeThreadOwned}}

	_, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true})
	if !errors.Is(err, fbdomain.ErrThreadOwnedElsewhere) || len(f.messaging.sent) != 2 {
		t.Fatalf("got %v after %d sends", err, len(f.messaging.sent))
	}
}

func TestAutomationNeverTakesTheThread(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.messaging.sendErrs = []error{&meta.Error{Code: meta.CodeThreadOwned}}

	_, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, fbdomain.ErrThreadOwnedElsewhere) {
		t.Fatalf("got %v", err)
	}
	if len(f.routing.taken) != 0 || len(f.messaging.sent) != 1 {
		t.Fatalf("automation took control %v or retried (%d sends)", f.routing.taken, len(f.messaging.sent))
	}
}

func TestRejectedTokenMovesThePageToReconnect(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.messaging.sendErrs = []error{&meta.Error{Code: meta.CodeAccessTokenError, Subcode: 463}}

	if _, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true}); err == nil {
		t.Fatal("a rejected token must fail the send")
	}
	if f.pages.statuses["page-1"] != fbdomain.StatusTokenRevoked {
		t.Fatalf("status = %q", f.pages.statuses["page-1"])
	}
}

func TestUnreachableCustomerIsMarked(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.messaging.sendErrs = []error{&meta.Error{Code: meta.CodePermission, Subcode: meta.SubcodeCannotReceive}}

	if _, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true}); err == nil {
		t.Fatal("an unreachable customer must fail the send")
	}
	if !f.contacts.byID[f.ec.ContactID].Unreachable {
		t.Fatal("contact not marked unreachable")
	}
}

func TestPageWithoutMessagingCannotSend(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.pages.byID["page-1"].Tasks = []fbdomain.Task{fbdomain.TaskAnalyze}

	_, err := f.adapter.SendText(context.Background(), f.ec, conversation.SendTextRequest{Body: "oi", HumanInitiated: true})
	if !errors.Is(err, fbdomain.ErrCapabilityDenied) || len(f.messaging.sent) != 0 {
		t.Fatalf("got %v with %d sends", err, len(f.messaging.sent))
	}
}

func TestMediaBytesAreUploadedThenSentByAttachmentID(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)

	_, err := f.adapter.SendMedia(context.Background(), f.ec, conversation.SendMediaRequest{
		Kind: "image", Bytes: []byte("png"), MIMEType: "image/png", FileName: "a.png", HumanInitiated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.messaging.uploads) != 1 || f.messaging.sent[0].AttachmentID != "att-1" || f.messaging.sent[0].AttachmentKind != "image" {
		t.Fatalf("uploads %v, sent %+v", f.messaging.uploads, f.messaging.sent)
	}
}

func TestMediaByURLIsSentDirectly(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)

	if _, err := f.adapter.SendMedia(context.Background(), f.ec, conversation.SendMediaRequest{Kind: "document", URL: "https://x/doc.pdf"}); err != nil {
		t.Fatal(err)
	}
	if f.messaging.sent[0].AttachmentURL != "https://x/doc.pdf" || len(f.messaging.uploads) != 0 {
		t.Fatalf("sent %+v", f.messaging.sent[0])
	}
}

func TestMediaOutsideTheLimitsIsUnsupported(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)

	for name, req := range map[string]conversation.SendMediaRequest{
		"kind":  {Kind: "sticker", URL: "https://x/s"},
		"mime":  {Kind: "video", URL: "https://x/v.mov", MIMEType: "video/quicktime"},
		"size":  {Kind: "image", Bytes: make([]byte, 9<<20), MIMEType: "image/png"},
		"empty": {Kind: "image"},
	} {
		if _, err := f.adapter.SendMedia(context.Background(), f.ec, req); !errors.Is(err, conversation.ErrCapabilityUnsupported) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if len(f.messaging.sent) != 0 {
		t.Fatal("unsupported media reached Meta")
	}
}

func TestButtonsBecomeATemplateAndListsBecomeQuickReplies(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	three := []conversation.InteractiveOption{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}}
	five := append(append([]conversation.InteractiveOption{}, three...), conversation.InteractiveOption{ID: "d", Title: "D"}, conversation.InteractiveOption{ID: "e", Title: "E"})

	if _, err := f.adapter.SendInteractive(context.Background(), f.ec, conversation.SendInteractiveRequest{Body: "Escolha", Options: three, Style: channel.InteractiveStyleButtons}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.SendInteractive(context.Background(), f.ec, conversation.SendInteractiveRequest{Body: "Escolha", Options: five, Style: channel.InteractiveStyleButtons}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.SendInteractive(context.Background(), f.ec, conversation.SendInteractiveRequest{Body: "Escolha", Options: three, Style: channel.InteractiveStyleList}); err != nil {
		t.Fatal(err)
	}

	if got := f.messaging.sent[0]; len(got.Buttons) != 3 || len(got.QuickReplies) != 0 || got.Metadata != "vozko:automation" {
		t.Errorf("buttons = %+v", got)
	}
	if got := f.messaging.sent[1]; len(got.QuickReplies) != 5 || len(got.Buttons) != 0 {
		t.Errorf("more options than buttons = %+v", got)
	}
	if got := f.messaging.sent[2]; len(got.QuickReplies) != 3 || got.QuickReplies[0].Payload != "a" {
		t.Errorf("list = %+v", got)
	}
}

func TestInteractiveWithoutRenderableOptionsIsUnsupported(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	_, err := f.adapter.SendInteractive(context.Background(), f.ec, conversation.SendInteractiveRequest{Body: "Escolha", Options: []conversation.InteractiveOption{{Title: "Sem id"}}})
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestPresenceAndReactionsUseSenderActions(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	ctx := context.Background()

	if err := f.adapter.SendTyping(ctx, f.ec, true); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.SendTyping(ctx, f.ec, false); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.MarkSeen(ctx, f.ec, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.SendReaction(ctx, f.ec, "m_1", "❤️"); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.RemoveReaction(ctx, f.ec, "m_1"); err != nil {
		t.Fatal(err)
	}

	want := []fbdomain.SenderAction{fbdomain.ActionTypingOn, fbdomain.ActionTypingOff, fbdomain.ActionMarkSeen}
	for i, a := range want {
		if f.messaging.actions[i] != a {
			t.Errorf("action %d = %q, want %q", i, f.messaging.actions[i], a)
		}
	}
	if len(f.messaging.reactions) != 2 || f.messaging.reactions[1] != "m_1:" {
		t.Errorf("reactions = %v", f.messaging.reactions)
	}
}

func TestAdapterDeclaresNoEditingOrRetraction(t *testing.T) {
	var a any = NewChannelAdapter(ChannelAdapterDeps{})
	if _, ok := a.(conversation.EditingAdapter); ok {
		t.Error("messenger cannot edit a sent message")
	}
	if _, ok := a.(conversation.RetractingAdapter); ok {
		t.Error("messenger cannot unsend a message")
	}
}

func TestCaptionGoesOutAsATextBeforeTheAttachment(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)

	out, err := f.adapter.SendMedia(context.Background(), f.ec, conversation.SendMediaRequest{
		Kind: "image", URL: "https://x/p.png", Caption: "Segue a foto", HumanInitiated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.messaging.sent) != 2 {
		t.Fatalf("sends = %d", len(f.messaging.sent))
	}
	caption, media := f.messaging.sent[0], f.messaging.sent[1]
	if caption.Text != "Segue a foto" || caption.Metadata != fbdomain.CaptionMetadata {
		t.Errorf("caption = %+v", caption)
	}
	if media.AttachmentURL != "https://x/p.png" || media.Metadata != "vozko:operator" {
		t.Errorf("media = %+v", media)
	}
	if out.ProviderMessageID != "m_2" {
		t.Errorf("outcome = %q, want the attachment's id", out.ProviderMessageID)
	}
}

func TestAFailedCaptionSendsNoAttachment(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.messaging.sendErrs = []error{&meta.Error{Code: meta.CodeInvalidParam}}

	if _, err := f.adapter.SendMedia(context.Background(), f.ec, conversation.SendMediaRequest{Kind: "image", URL: "https://x/p.png", Caption: "Legenda"}); err == nil {
		t.Fatal("expected the caption failure")
	}
	if len(f.messaging.sent) != 1 {
		t.Fatalf("sends = %d, want only the failed caption", len(f.messaging.sent))
	}
}
