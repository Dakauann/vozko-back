package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/user"
)

type opRecordingLedger struct {
	sent     map[conversation.SendRequest]*conversation.Message
	claimed  map[conversation.SendRequest]bool
	claimErr error
	released []conversation.SendRequest
	lookups  int
}

func newOpRecordingLedger() *opRecordingLedger {
	return &opRecordingLedger{
		sent:    map[conversation.SendRequest]*conversation.Message{},
		claimed: map[conversation.SendRequest]bool{},
	}
}

func (l *opRecordingLedger) Sent(request conversation.SendRequest) (*conversation.Message, error) {
	l.lookups++
	if message, ok := l.sent[request]; ok {
		return message, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (l *opRecordingLedger) Claim(request conversation.SendRequest) (bool, error) {
	if l.claimErr != nil {
		return false, l.claimErr
	}
	if l.claimed[request] {
		return false, nil
	}
	l.claimed[request] = true
	return true, nil
}

func (l *opRecordingLedger) Release(request conversation.SendRequest) {
	l.released = append(l.released, request)
	delete(l.claimed, request)
}

func requestedInput(requestID string) conversation.OperatorSendInput {
	in := baseInput()
	in.RequestID = requestID
	return in
}

func sentCount(f *operatorSendFixture) int {
	return len(f.sender.texts) + len(f.sender.medias) + len(f.sender.buttons)
}

func TestTheClientRequestReachesTheStoredMessage(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
	in := requestedInput("req-1")
	in.ReplyToMessageID = "msg-0"

	if _, err := f.uc.Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	want := conversation.SendOrigin{SenderUserID: "user-1", ReplyToMessageID: "msg-0", ClientRequestID: "req-1"}
	if got := f.sender.texts[0].origin; got != want {
		t.Fatalf("origin = %+v, want %+v", got, want)
	}
}

func TestARepeatedRequestAnswersWithTheOriginalMessageAndNeverSendsAgain(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
	original := &conversation.Message{ID: "msg-original", ClientRequestID: "req-1"}
	f.requests.sent[conversation.SendRequest{EntryType: "whatsapp", EntryID: "entry-1", RequestID: "req-1"}] = original

	got, err := f.uc.Execute(context.Background(), requestedInput("req-1"))
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Fatalf("answered %+v, want the original message", got)
	}
	if sentCount(f) != 0 || f.billing.calls != 0 || len(f.finalizer.calls) != 0 {
		t.Fatalf("a replay sent %d, billed %d, finalized %d; want nothing", sentCount(f), f.billing.calls, len(f.finalizer.calls))
	}
}

func TestARequestAlreadyBeingSentIsNotSentTwice(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
	f.requests.claimed[conversation.SendRequest{EntryType: "whatsapp", EntryID: "entry-1", RequestID: "req-1"}] = true

	_, err := f.uc.Execute(context.Background(), requestedInput("req-1"))
	if !errors.Is(err, conversation.ErrSendRequestInFlight) {
		t.Fatalf("err = %v, want ErrSendRequestInFlight", err)
	}
	if sentCount(f) != 0 {
		t.Fatal("a request in flight on another connection was sent again")
	}
}

func TestAFailedSendReleasesItsRequestSoTheClientCanRetry(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
	f.sender.err = errors.New("provider refused")

	if _, err := f.uc.Execute(context.Background(), requestedInput("req-1")); err == nil {
		t.Fatal("the provider failure must surface")
	}
	if len(f.requests.released) != 1 || f.requests.released[0].RequestID != "req-1" {
		t.Fatalf("released = %+v, want the failed request", f.requests.released)
	}
}

func TestARefusedSendReleasesItsRequest(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
	f.billing.allowErr = errors.New("no balance")

	if _, err := f.uc.Execute(context.Background(), requestedInput("req-1")); err == nil {
		t.Fatal("the billing refusal must surface")
	}
	if len(f.requests.released) != 1 {
		t.Fatalf("released = %+v, want the refused request", f.requests.released)
	}
}

func TestASendWithoutARequestIDIsNotDeduplicated(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)

	for i := 0; i < 2; i++ {
		if _, err := f.uc.Execute(context.Background(), requestedInput("")); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.sender.texts) != 2 || f.requests.lookups != 0 {
		t.Fatalf("sent %d with %d lookups, want two plain sends", len(f.sender.texts), f.requests.lookups)
	}
}

func TestASendIsRefusedWhenItsRequestCannotBeClaimed(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)
	f.requests.claimErr = errors.New("shared state down")

	if _, err := f.uc.Execute(context.Background(), requestedInput("req-1")); err == nil {
		t.Fatal("without a claim the send must be refused, or a replay could go out twice")
	}
	if sentCount(f) != 0 {
		t.Fatal("an unclaimed request was sent")
	}
}

func TestAnOversizedRequestIDIsRefused(t *testing.T) {
	f := newOperatorSendFixture(t, &user.User{ID: "user-1"}, nil)

	_, err := f.uc.Execute(context.Background(), requestedInput(strings.Repeat("r", conversation.MaxClientRequestIDLength+1)))
	if !errors.Is(err, conversation.ErrClientRequestIDInvalid) {
		t.Fatalf("err = %v, want ErrClientRequestIDInvalid", err)
	}
}
