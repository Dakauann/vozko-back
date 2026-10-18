package leadaction_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/leadaction"
	"vozko/domain/selection"
)

const restoreNote = "o botão SAIR estava ligado à opção errada"

func restore(confirmCount int, s selection.Selection) Request {
	return Request{
		Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionRestoreOptOut,
		Params: leadaction.Params{Note: restoreNote, ConfirmCount: confirmCount}, Selection: s, IdempotencyKey: "key-restore",
	}
}

func consentHarness(n int) *harness {
	h := newHarness(n)
	h.perms[manager]["leads:consent"] = true
	return h
}

func TestUndoingOptOutsNeedsTheConsentCapability(t *testing.T) {
	h := newHarness(3)
	if _, err := h.svc.Start(context.Background(), restore(3, matchingAll(3))); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if _, err := h.svc.Preview(context.Background(), restore(0, matchingAll(0))); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("preview got %v, want ErrForbidden", err)
	}
}

func TestUndoingOptOutsWithoutTheNoteOrTheTypedCountFreezesNothing(t *testing.T) {
	h := consentHarness(3)
	noNote := restore(3, matchingAll(3))
	noNote.Params.Note = "  "
	if _, err := h.svc.Start(context.Background(), noNote); !errors.Is(err, leadaction.ErrNoteRequired) {
		t.Fatalf("got %v, want ErrNoteRequired", err)
	}
	if _, err := h.svc.Start(context.Background(), restore(0, matchingAll(3))); !errors.Is(err, selection.ErrCountRequired) {
		t.Fatalf("got %v, want ErrCountRequired", err)
	}
	if h.selections.freezes != 0 || h.runs.create != 0 {
		t.Fatalf("a refused request froze %d selections and created %d runs", h.selections.freezes, h.runs.create)
	}
}

func TestATypedCountOtherThanTheSelectedLeadsIsRefusedAndReleasesTheSelection(t *testing.T) {
	h := consentHarness(3)
	_, err := h.svc.Start(context.Background(), restore(4, matchingAll(3)))
	var changed *selection.CountChangedError
	if !errors.As(err, &changed) || changed.Expected != 4 || changed.Matched != 3 {
		t.Fatalf("got %v, want the count changed refusal", err)
	}
	if h.runs.create != 0 || len(h.selections.snapshots) != 0 || len(h.selections.dropped) != 1 {
		t.Fatalf("runs %d, snapshots %d, dropped %v", h.runs.create, len(h.selections.snapshots), h.selections.dropped)
	}
}

func TestUndoingOptOutsRunsInBatchesForWithdrawalsChosenBeforeTheRequest(t *testing.T) {
	h := consentHarness(3)
	out, err := h.svc.Start(context.Background(), restore(3, matchingAll(3)))
	if err != nil {
		t.Fatal(err)
	}
	if out.Run == nil || out.Run.Params.Note != restoreNote || out.Run.Params.ConfirmCount != 3 {
		t.Fatalf("run = %+v", out.Run)
	}
	h.clock.at = h.clock.at.Add(time.Minute)
	h.drain()
	if len(h.writer.applied) == 0 {
		t.Fatal("no batch was applied")
	}
	edit := h.writer.applied[0]
	if edit.Kind != leadaction.EditRestoreOptOut || edit.Note() != restoreNote || !edit.ChosenAt.Equal(out.Run.CreatedAt) {
		t.Fatalf("edit = %+v, run created at %v", edit, out.Run.CreatedAt)
	}
	if h.selections.pendings[0] != nil {
		t.Fatal("a restore has no pending assignment")
	}
}

func TestThePreviewCountsWithdrawalsChosenNow(t *testing.T) {
	h := consentHarness(3)
	p, err := h.svc.Preview(context.Background(), restore(0, matchingAll(0)))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != leadaction.PreviewDone || len(h.writer.tallied) == 0 {
		t.Fatalf("preview = %+v, tallied %d", p, len(h.writer.tallied))
	}
	if edit := h.writer.tallied[0]; edit.Kind != leadaction.EditRestoreOptOut || !edit.ChosenAt.Equal(start) {
		t.Fatalf("edit = %+v", edit)
	}
}
