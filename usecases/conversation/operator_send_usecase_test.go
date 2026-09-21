package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/user"
)

// What an operator send MEANS used to live inside the WebSocket hub's frame
// handler: which signature the channel renders, how media differs from text,
// and what the conversation is owed afterwards. These pin it here, where every
// send surface reaches it.

type opSentText struct{ entryID, entryType, text, userID, replyTo string }
type opSentMedia struct{ entryID, entryType, mediaID, mediaType, userID, replyTo, caption string }
type opSentButtons struct {
	entryID, entryType, userID, replyTo string
	input                               conversation.SendButtonInput
}

type opRecordingSender struct {
	texts   []opSentText
	medias  []opSentMedia
	buttons []opSentButtons
	err     error
}

func (r *opRecordingSender) SendTextMessage(entryID, entryType, text, userID, replyTo string) (*conversation.Message, error) {
	r.texts = append(r.texts, opSentText{entryID, entryType, text, userID, replyTo})
	if r.err != nil {
		return nil, r.err
	}
	return &conversation.Message{ID: "msg-1", EntryID: entryID, EntryType: shared.EntryType(entryType)}, nil
}

func (r *opRecordingSender) SendMediaMessage(entryID, entryType, mediaID, mediaType, userID, replyTo, caption string) (*conversation.Message, error) {
	r.medias = append(r.medias, opSentMedia{entryID, entryType, mediaID, mediaType, userID, replyTo, caption})
	if r.err != nil {
		return nil, r.err
	}
	return &conversation.Message{ID: "msg-2", EntryID: entryID, EntryType: shared.EntryType(entryType)}, nil
}

func (r *opRecordingSender) SendButtonMessage(entryID, entryType, userID, replyTo string, in conversation.SendButtonInput) (*conversation.Message, error) {
	r.buttons = append(r.buttons, opSentButtons{entryID, entryType, userID, replyTo, in})
	if r.err != nil {
		return nil, r.err
	}
	return &conversation.Message{ID: "msg-3", EntryID: entryID, EntryType: shared.EntryType(entryType)}, nil
}

type opStubUserRepo struct {
	user.UserRepository
	u   *user.User
	err error
}

func (r opStubUserRepo) FindByID(string) (*user.User, error) { return r.u, r.err }

type opRecordingFinalizer struct {
	calls []conversation.FinalizeOperatorSendInput
	err   error
}

func (f *opRecordingFinalizer) FinalizeOperatorSend(_ context.Context, in conversation.FinalizeOperatorSendInput) error {
	f.calls = append(f.calls, in)
	return f.err
}

type operatorSendFixture struct {
	sender    *opRecordingSender
	billing   *opStubBilling
	finalizer *opRecordingFinalizer
	uc        conversation.OperatorSendUseCase
}

func newOperatorSendFixture(t *testing.T, u *user.User, userErr error) *operatorSendFixture {
	t.Helper()
	f := &operatorSendFixture{
		sender:    &opRecordingSender{},
		billing:   &opStubBilling{},
		finalizer: &opRecordingFinalizer{},
	}
	uc, err := NewOperatorSendUseCase(f.sender, opStubUserRepo{u: u, err: userErr}, f.finalizer, f.billing)
	if err != nil {
		t.Fatalf("NewOperatorSendUseCase: %v", err)
	}
	f.uc = uc
	return f
}

func baseInput() conversation.OperatorSendInput {
	return conversation.OperatorSendInput{
		EntryID:      "entry-1",
		EntryType:    string(shared.EntryTypeWhatsApp),
		WorkspaceID:  "ws-1",
		SenderUserID: "user-1",
		Text:         "oi",
	}
}

func TestOperatorSendDeliversTextAndFinalizes(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)

	msg, err := f.uc.Execute(context.Background(), baseInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if msg == nil || msg.ID != "msg-1" {
		t.Fatalf("message = %+v", msg)
	}
	if len(f.sender.texts) != 1 || f.sender.texts[0].text != "oi" {
		t.Fatalf("texts = %+v, want one unsigned 'oi'", f.sender.texts)
	}
	if len(f.finalizer.calls) != 1 {
		t.Fatalf("finalizer calls = %d, want 1", len(f.finalizer.calls))
	}
	if got := f.finalizer.calls[0]; got.ActorUserID != "user-1" || got.Message == nil {
		t.Errorf("finalize input = %+v", got)
	}
}

// The signature is applied at SEND time, in the form the channel renders.
// Asserted against SignOutbound rather than a literal, so a format change can
// never make the composer and the scheduled dispatcher disagree.
func TestOperatorSendAppliesTheChannelSignature(t *testing.T) {
	for _, entryType := range []shared.EntryType{shared.EntryTypeWhatsApp, shared.EntryTypeInstagram} {
		t.Run(string(entryType), func(t *testing.T) {
			f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)

			in := baseInput()
			in.EntryType = string(entryType)
			in.Signed = true

			if _, err := f.uc.Execute(context.Background(), in); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			want := conversation.SignOutbound(entryType, "Ana", "oi")
			if got := f.sender.texts[0].text; got != want {
				t.Errorf("sent %q, want %q", got, want)
			}
		})
	}
}

// Resolve-and-continue: a dead user repository costs the signature prefix and
// nothing else. Before this lived in one place, the hub read Username off a nil
// user and took the process down.
func TestOperatorSendSurvivesAUserLookupFailure(t *testing.T) {
	f := newOperatorSendFixture(t, nil, errors.New("user lookup failed"))

	in := baseInput()
	in.Signed = true

	if _, err := f.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(f.sender.texts) != 1 {
		t.Fatalf("the send never happened: %+v", f.sender.texts)
	}
	if got := f.sender.texts[0].text; got != "oi" {
		t.Errorf("sent %q, want the bare text with no stray signature markup", got)
	}
	if strings.HasPrefix(f.sender.texts[0].text, "*") {
		t.Errorf("an unresolved user produced signature markup: %q", f.sender.texts[0].text)
	}
}

// Media carries the text as its caption, and the caption is signed exactly like
// a text body would be.
func TestOperatorSendRoutesMediaWithASignedCaption(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)

	in := baseInput()
	in.MediaID, in.MediaType, in.Signed = "med-1", "image", true

	if _, err := f.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(f.sender.texts) != 0 {
		t.Errorf("a media send also went out as text: %+v", f.sender.texts)
	}
	if len(f.sender.medias) != 1 {
		t.Fatalf("medias = %+v", f.sender.medias)
	}
	want := conversation.SignOutbound(shared.EntryTypeWhatsApp, "Ana", "oi")
	if got := f.sender.medias[0].caption; got != want {
		t.Errorf("caption = %q, want %q", got, want)
	}
}

// An interactive prompt is never signed: the prefix would land in the body above
// the buttons and read as part of the question.
func TestOperatorSendDoesNotSignAnInteractivePrompt(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)

	in := baseInput()
	in.Signed = true
	in.Buttons = &conversation.SendButtonInput{
		BodyText: "escolha",
		Buttons:  []conversation.ButtonPayload{{ID: "b1", Title: "Sim"}},
	}

	if _, err := f.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(f.sender.buttons) != 1 {
		t.Fatalf("buttons = %+v", f.sender.buttons)
	}
	if got := f.sender.buttons[0].input.BodyText; got != "escolha" {
		t.Errorf("body = %q, want it unsigned", got)
	}
	if len(f.finalizer.calls) != 1 {
		t.Errorf("an interactive send skipped the post-send side effects")
	}
}

// A failed send has nothing to finalize, and the error reaches the caller.
func TestOperatorSendDoesNotFinalizeAFailedSend(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)
	f.sender.err = errors.New("provider refused")

	if _, err := f.uc.Execute(context.Background(), baseInput()); err == nil {
		t.Fatal("a failing send must surface its error")
	}
	if len(f.finalizer.calls) != 0 {
		t.Errorf("a failed send was finalized: %+v", f.finalizer.calls)
	}
}

// The message is already with the customer by the time the finalizer runs, so a
// failing side effect must never be reported as a failed send.
func TestOperatorSendSucceedsWhenFinalizationFails(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)
	f.finalizer.err = errors.New("telemetry down")

	msg, err := f.uc.Execute(context.Background(), baseInput())
	if err != nil {
		t.Fatalf("a failing finalizer turned a delivered message into an error: %v", err)
	}
	if msg == nil {
		t.Fatal("message was dropped")
	}
}

func TestOperatorSendValidatesItsInput(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*conversation.OperatorSendInput)
		wantErr error
	}{
		{"no entry", func(in *conversation.OperatorSendInput) { in.EntryID = " " }, conversation.ErrEntryIDRequired},
		{"unknown channel", func(in *conversation.OperatorSendInput) { in.EntryType = "carrier-pigeon" }, conversation.ErrEntryTypeInvalid},
		{"no content", func(in *conversation.OperatorSendInput) { in.Text = "   " }, conversation.ErrMessageContentRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
			in := baseInput()
			tc.mutate(&in)

			_, err := f.uc.Execute(context.Background(), in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(f.sender.texts)+len(f.sender.medias)+len(f.sender.buttons) != 0 {
				t.Errorf("an invalid input still reached the provider")
			}
		})
	}
}

// A missing dependency must stop the boot, not silently cost every reply its
// side effects.
func TestNewOperatorSendUseCaseRefusesMissingDependencies(t *testing.T) {
	if _, err := NewOperatorSendUseCase(nil, opStubUserRepo{}, &opRecordingFinalizer{}, &opStubBilling{}); err == nil {
		t.Error("a nil message sender was accepted")
	}
	if _, err := NewOperatorSendUseCase(&opRecordingSender{}, nil, &opRecordingFinalizer{}, &opStubBilling{}); err == nil {
		t.Error("a nil user repository was accepted")
	}
	if _, err := NewOperatorSendUseCase(&opRecordingSender{}, opStubUserRepo{}, nil, &opStubBilling{}); err == nil {
		t.Error("a nil finalizer was accepted")
	}
}

// opStubBilling stands in for the Meta service message billing. Its zero value
// allows, which is the unpriced plan every existing test assumes.
type opStubBilling struct {
	allowErr error
	calls    int
}

func (s *opStubBilling) AllowSend(string) error {
	s.calls++
	return s.allowErr
}
func (s *opStubBilling) ShouldCharge(conversation.DeliveryReceipt) bool { return false }
func (s *opStubBilling) ChargeDelivered(string, conversation.DeliveryReceipt, string) error {
	return nil
}

// A plan that prices service messages and a workspace that cannot pay: the
// reply is refused BEFORE it is sent, because after sending there is nothing to
// refuse. Meta charges on delivery and the message is already gone.
func TestOperatorSendRefusesWhatTheWorkspaceCannotPayFor(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)
	f.billing.allowErr = balance.ErrInsufficientBalance

	_, err := f.uc.Execute(context.Background(), conversation.OperatorSendInput{
		EntryID:     "e-1",
		EntryType:   string(shared.EntryTypeWhatsApp),
		WorkspaceID: "ws-1",
		Text:        "olá",
	})

	if !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("Execute() = %v, want ErrInsufficientBalance", err)
	}
	if len(f.sender.texts) != 0 {
		t.Error("the message was sent anyway; the gate has to run before the send")
	}
}

// The unpriced plan, which is every plan today: the gate is asked and says
// nothing, and the reply goes out at any balance.
func TestOperatorSendProceedsWhenServiceMessagesAreFree(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)

	if _, err := f.uc.Execute(context.Background(), conversation.OperatorSendInput{
		EntryID:     "e-1",
		EntryType:   string(shared.EntryTypeWhatsApp),
		WorkspaceID: "ws-1",
		Text:        "olá",
	}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if f.billing.calls != 1 {
		t.Errorf("the billing was asked %d times, want 1", f.billing.calls)
	}
	if len(f.sender.texts) != 1 {
		t.Errorf("the message was sent %d times, want 1", len(f.sender.texts))
	}
}

// Without a workspace there is nothing to charge and nothing to check. Refusing
// an operator's reply because a hint was missing would be a worse failure than
// not charging for it.
func TestOperatorSendSkipsTheGateWithoutAWorkspace(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1", Username: "Ana"}, nil)
	f.billing.allowErr = balance.ErrInsufficientBalance

	if _, err := f.uc.Execute(context.Background(), conversation.OperatorSendInput{
		EntryID:   "e-1",
		EntryType: string(shared.EntryTypeWhatsApp),
		Text:      "olá",
	}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if f.billing.calls != 0 {
		t.Errorf("the billing was asked %d times with no workspace, want 0", f.billing.calls)
	}
}
