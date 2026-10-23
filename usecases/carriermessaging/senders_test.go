package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

type fakeDocuments struct{ valid bool }

func (f fakeDocuments) ValidateCPFOrCNPJ(string) bool { return f.valid }

func brandInput() SenderInput {
	return SenderInput{
		BrandName: "Loja do João", LogoURL: "https://media.example.com/logo.png", BannerURL: "https://media.example.com/banner.png",
		Color: "#1a73e8", UseCase: rcs.UseCasePromotional, Sector: rcs.SectorRetail, ContactName: "João",
		ContactEmail: "mkt@lojadojoao.com.br", ContactPhone: "+5511999990000", WebsiteURL: "https://lojadojoao.com.br",
		PrivacyURL: "https://lojadojoao.com.br/p", TermsURL: "https://lojadojoao.com.br/t", CNPJ: "11.222.333/0001-81",
		BillingCategory: rcs.BillingNonConversational,
	}
}

func TestCreateSenderStartsADraftWithTheISVDescription(t *testing.T) {
	h := newHarness(t)
	s, err := h.svc.CreateSender(context.Background(), "ws-1", brandInput())
	if err != nil {
		t.Fatalf("CreateSender = %v", err)
	}
	if s.Status != rcs.SenderDraft || s.Description != "Vozko envia promoções e novidades em nome de Loja do João." || s.CNPJ != "11222333000181" || s.Color != "#1A73E8" {
		t.Fatalf("sender = %+v", s)
	}
}

func TestDraftsMayBeIncomplete(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.CreateSender(context.Background(), "ws-1", SenderInput{BrandName: "Loja"}); err != nil {
		t.Fatalf("CreateSender = %v", err)
	}
	if _, err := h.svc.CreateSender(context.Background(), "ws-1", SenderInput{}); !errors.Is(err, rcs.ErrSenderIncomplete) {
		t.Fatalf("a draft needs at least the brand name: %v", err)
	}
}

func TestSubmitSenderRegistersWithTheProvider(t *testing.T) {
	h := newHarness(t)
	draft, _ := h.svc.CreateSender(context.Background(), "ws-1", brandInput())

	s, err := h.svc.SubmitSender(context.Background(), "ws-1", draft.ID)
	if err != nil {
		t.Fatalf("SubmitSender = %v", err)
	}
	if s.Status != rcs.SenderSubmitted || s.ProviderProfileID != "profile-1" || s.ProviderRequestID != "req-1" || s.Provider != "fake" {
		t.Fatalf("sender = %+v", s)
	}
	if len(h.provider.submits) != 1 || len(h.notices.states) != 1 {
		t.Fatalf("submits = %d, notices = %d", len(h.provider.submits), len(h.notices.states))
	}
}

func TestSubmitRefusesAnInvalidCNPJBeforeTheProvider(t *testing.T) {
	h := newHarness(t)
	h.svc.Documents = fakeDocuments{valid: false}
	draft, _ := h.svc.CreateSender(context.Background(), "ws-1", brandInput())
	if _, err := h.svc.SubmitSender(context.Background(), "ws-1", draft.ID); !errors.Is(err, ErrCNPJInvalid) {
		t.Fatalf("SubmitSender = %v", err)
	}
	if len(h.provider.submits) != 0 {
		t.Fatal("the provider must not see an invalid brand")
	}
}

func TestProviderFailureKeepsTheDraft(t *testing.T) {
	h := newHarness(t)
	draft, _ := h.svc.CreateSender(context.Background(), "ws-1", brandInput())
	h.provider.err = carriermessaging.ErrProviderUnavailable
	if _, err := h.svc.SubmitSender(context.Background(), "ws-1", draft.ID); !errors.Is(err, carriermessaging.ErrProviderUnavailable) {
		t.Fatalf("SubmitSender = %v", err)
	}
	stored, _ := h.senders.FindByID(context.Background(), "ws-1", draft.ID)
	if stored.Status != rcs.SenderDraft {
		t.Fatalf("status = %s, a failed submission stays a draft", stored.Status)
	}
}

func TestUpdateOnlyWhileEditable(t *testing.T) {
	h := newHarness(t)
	in := brandInput()
	in.BrandName = "Outra"
	if _, err := h.svc.UpdateSender(context.Background(), "ws-1", "sender-1", in); !errors.Is(err, rcs.ErrSenderTransition) {
		t.Fatalf("UpdateSender on a launched sender = %v", err)
	}
}

func TestWorkspacePauseAndResume(t *testing.T) {
	h := newHarness(t)
	s, err := h.svc.PauseSender(context.Background(), "ws-1", "sender-1")
	if err != nil || s.Status != rcs.SenderPaused || s.PauseReason != rcs.PauseManual {
		t.Fatalf("PauseSender = %+v, %v", s, err)
	}
	if s, err = h.svc.ResumeSender(context.Background(), "ws-1", "sender-1"); err != nil || !s.CanSend() {
		t.Fatalf("ResumeSender = %+v, %v", s, err)
	}
}

func TestWorkspaceCannotLiftAPlatformPause(t *testing.T) {
	h := newHarness(t)
	sender := h.senders.rows["sender-1"]
	paused, _ := sender.Pause(rcs.PausedByPlatform, rcs.PauseOptOutRate, t0)
	*sender = paused
	if _, err := h.svc.ResumeSender(context.Background(), "ws-1", "sender-1"); !errors.Is(err, ErrPauseNotYours) {
		t.Fatalf("ResumeSender = %v", err)
	}
	if _, err := h.svc.AdminResumeSender(context.Background(), "sender-1"); err != nil {
		t.Fatalf("AdminResumeSender = %v", err)
	}
}

func TestSyncSendersAppliesProviderStates(t *testing.T) {
	h := newHarness(t)
	draft, _ := h.svc.CreateSender(context.Background(), "ws-1", brandInput())
	_, _ = h.svc.SubmitSender(context.Background(), "ws-1", draft.ID)
	h.notices.states = nil

	h.provider.status = carriermessaging.SenderStatusReport{State: rcs.ProviderLaunched}
	changed, err := h.svc.SyncSenders(context.Background(), 50)
	if err != nil || changed < 1 {
		t.Fatalf("SyncSenders = %d, %v", changed, err)
	}
	stored, _ := h.senders.FindByID(context.Background(), "ws-1", draft.ID)
	if !stored.CanSend() || len(h.notices.states) == 0 {
		t.Fatalf("stored = %+v, notices = %v", stored, h.notices.states)
	}
	changed, _ = h.svc.SyncSenders(context.Background(), 50)
	if changed != 0 {
		t.Fatalf("a second sync with no news changed %d senders", changed)
	}
}

func TestDeleteOnlyDraftsAndRejected(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.DeleteSender(context.Background(), "ws-1", "sender-1"); !errors.Is(err, rcs.ErrSenderTransition) {
		t.Fatalf("DeleteSender launched = %v", err)
	}
	draft, _ := h.svc.CreateSender(context.Background(), "ws-1", SenderInput{BrandName: "X"})
	if err := h.svc.DeleteSender(context.Background(), "ws-1", draft.ID); err != nil {
		t.Fatalf("DeleteSender draft = %v", err)
	}
}
