package whatsapp_outreach

import (
	"context"
	"errors"
	"testing"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	wo "vozko/domain/whatsapp_outreach"
)

func TestStartingAConversationClaimsItForTheSender(t *testing.T) {
	uc := newUC(t)

	started, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}

	want := claimCall{started.EntryID, string(shared.EntryTypeWhatsApp), "bp-1", "ws-1", "user-1", ia.TriggerOutreachSent}
	if len(uc.claims.calls) != 1 || uc.claims.calls[0] != want {
		t.Fatalf("claims = %+v, want %+v", uc.claims.calls, want)
	}
}

func TestATemplateIntoAConversationClaimsItForTheSender(t *testing.T) {
	f := newConversationUC(t)

	if _, err := f.uc.Send(context.Background(), conversationInput()); err != nil {
		t.Fatal(err)
	}

	want := claimCall{"entry-1", string(shared.EntryTypeWhatsApp), "bp-1", "ws-1", "user-1", ia.TriggerOutreachSent}
	if len(f.claims.calls) != 1 || f.claims.calls[0] != want {
		t.Fatalf("claims = %+v, want %+v", f.claims.calls, want)
	}
}

func TestAFailedSendDoesNotClaim(t *testing.T) {
	uc := newUC(t)
	uc.sender.err = errors.New("meta refused")

	if _, err := uc.uc.Execute(context.Background(), input()); err == nil {
		t.Fatal("expected the send error")
	}
	if len(uc.claims.calls) != 0 {
		t.Fatalf("claims = %+v, a send that never went out must not take the conversation", uc.claims.calls)
	}
}

func TestAReplayedSendDoesNotClaimAgain(t *testing.T) {
	uc := newUC(t)
	uc.sender.result.Replayed = true

	if _, err := uc.uc.Execute(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if len(uc.claims.calls) != 0 {
		t.Fatalf("claims = %+v, a replay settles nothing", uc.claims.calls)
	}
}

func TestAnOpenWindowDoesNotClaim(t *testing.T) {
	uc := newUC(t, func(d *Deps) { d.Windows = &fakeWindows{open: true} })

	if _, err := uc.uc.Execute(context.Background(), input()); !errors.Is(err, wo.ErrWindowAlreadyOpen) {
		t.Fatalf("err = %v, want ErrWindowAlreadyOpen", err)
	}
	if len(uc.claims.calls) != 0 {
		t.Fatalf("claims = %+v, no template goes out while the window is open", uc.claims.calls)
	}
}

func TestASendWithoutAUserDoesNotClaim(t *testing.T) {
	uc := newUC(t)
	in := input()
	in.UserID = ""

	if _, err := uc.uc.Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(uc.claims.calls) != 0 {
		t.Fatalf("claims = %+v, there is no sender to give the conversation to", uc.claims.calls)
	}
}

func TestAFailedClaimStillReportsTheSend(t *testing.T) {
	uc := newUC(t)
	uc.claims.err = errors.New("db down")

	started, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatalf("err = %v, the message already went out", err)
	}
	if started.MessageID != "wamid.1" {
		t.Fatalf("message id = %q, want the delivered message", started.MessageID)
	}
}
