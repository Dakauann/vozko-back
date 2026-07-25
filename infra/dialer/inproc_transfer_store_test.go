package dialer

import (
	"errors"
	"testing"
	"time"

	dialer_domain "vozko/domain/dialer"
)

func makeHandle(id, callID string, stage dialer_domain.TransferStage) *dialer_domain.TransferHandle {
	now := time.Now().UTC()
	return &dialer_domain.TransferHandle{
		ID:           id,
		CallID:       callID,
		WorkspaceID:  "ws-1",
		InitiatorID:  "user-a",
		TargetUserID: "user-b",
		Kind:         dialer_domain.TransferKindBlind,
		Stage:        stage,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestInProcTransferStore_PutGetDelete(t *testing.T) {
	s := NewInProcTransferStore()
	h := makeHandle("t1", "call-1", dialer_domain.TransferStagePendingOffer)
	if err := s.Put(h); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := s.Get("t1")
	if !ok || got.ID != "t1" {
		t.Fatalf("Get: %+v ok=%v", got, ok)
	}
	s.Delete("t1")
	if _, ok := s.Get("t1"); ok {
		t.Fatal("Delete failed")
	}
}

func TestInProcTransferStore_FindByCall_OnlyNonTerminal(t *testing.T) {
	s := NewInProcTransferStore()
	_ = s.Put(makeHandle("t1", "call-1", dialer_domain.TransferStagePendingOffer))

	got, ok := s.FindByCall("ws-1", "call-1")
	if !ok || got.ID != "t1" {
		t.Fatalf("FindByCall: %+v ok=%v", got, ok)
	}

	_, err := s.Update("t1", func(h *dialer_domain.TransferHandle) error {
		h.Stage = dialer_domain.TransferStageCompleted
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, ok := s.FindByCall("ws-1", "call-1"); ok {
		t.Fatal("terminal transfer must not be findable by call")
	}
}

func TestInProcTransferStore_Put_RejectsConcurrentInFlight(t *testing.T) {
	s := NewInProcTransferStore()
	_ = s.Put(makeHandle("t1", "call-1", dialer_domain.TransferStagePendingOffer))

	err := s.Put(makeHandle("t2", "call-1", dialer_domain.TransferStagePendingOffer))
	if !errors.Is(err, dialer_domain.ErrTransferAlreadyInFlight) {
		t.Fatalf("want ErrTransferAlreadyInFlight, got %v", err)
	}
}

func TestInProcTransferStore_Put_AllowsAfterTerminal(t *testing.T) {
	s := NewInProcTransferStore()
	_ = s.Put(makeHandle("t1", "call-1", dialer_domain.TransferStagePendingOffer))
	_, _ = s.Update("t1", func(h *dialer_domain.TransferHandle) error {
		h.Stage = dialer_domain.TransferStageDeclined
		return nil
	})

	if err := s.Put(makeHandle("t2", "call-1", dialer_domain.TransferStagePendingOffer)); err != nil {
		t.Fatalf("second transfer after terminal must be allowed: %v", err)
	}
}

func TestInProcTransferStore_Update_PropagatesError(t *testing.T) {
	s := NewInProcTransferStore()
	_ = s.Put(makeHandle("t1", "call-1", dialer_domain.TransferStagePendingOffer))
	boom := errors.New("boom")
	_, err := s.Update("t1", func(*dialer_domain.TransferHandle) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Update must surface mutation error: got %v", err)
	}
}

func TestInProcTransferStore_Update_NotFound(t *testing.T) {
	s := NewInProcTransferStore()
	_, err := s.Update("missing", func(*dialer_domain.TransferHandle) error { return nil })
	if !errors.Is(err, dialer_domain.ErrTransferNotFound) {
		t.Fatalf("missing: want ErrTransferNotFound, got %v", err)
	}
}

func TestInProcTransferStore_ExpireBefore(t *testing.T) {
	s := NewInProcTransferStore()
	old := makeHandle("t-old", "call-1", dialer_domain.TransferStagePendingOffer)
	old.UpdatedAt = time.Now().UTC().Add(-time.Minute)
	_ = s.Put(old)

	fresh := makeHandle("t-fresh", "call-2", dialer_domain.TransferStagePendingOffer)
	_ = s.Put(fresh)

	expired := s.ExpireBefore(time.Now().UTC().Add(-30 * time.Second))
	if len(expired) != 1 || expired[0].ID != "t-old" {
		t.Fatalf("ExpireBefore: %+v", expired)
	}
	if expired[0].Stage != dialer_domain.TransferStageTimedOut {
		t.Fatalf("expected stage TimedOut, got %s", expired[0].Stage)
	}
	if _, ok := s.FindByCall("ws-1", "call-1"); ok {
		t.Fatal("expired transfer must not be findable by call")
	}
	if _, ok := s.FindByCall("ws-1", "call-2"); !ok {
		t.Fatal("fresh transfer must still be findable")
	}
}
