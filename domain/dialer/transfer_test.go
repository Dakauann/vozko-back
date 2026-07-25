package dialer

import (
	"errors"
	"testing"
)

func TestTransferKind_Valid(t *testing.T) {
	cases := []struct {
		kind TransferKind
		want bool
	}{
		{TransferKindBlind, true},
		{TransferKindAttended, true},
		{TransferKind(""), false},
		{TransferKind("warm"), false},
	}
	for _, c := range cases {
		if got := c.kind.Valid(); got != c.want {
			t.Errorf("TransferKind(%q).Valid() = %v, want %v", c.kind, got, c.want)
		}
	}
}

func TestTransferStage_Terminal(t *testing.T) {
	terminal := []TransferStage{
		TransferStageCompleted,
		TransferStageDeclined,
		TransferStageTimedOut,
		TransferStageCancelled,
		TransferStageRecalled,
		TransferStageFailed,
	}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Errorf("Stage %q expected terminal", s)
		}
	}
	nonTerminal := []TransferStage{
		TransferStagePendingOffer,
		TransferStageConsulting,
		TransferStageCompleting,
		TransferStageRecalling,
	}
	for _, s := range nonTerminal {
		if s.Terminal() {
			t.Errorf("Stage %q must not be terminal", s)
		}
	}
}

func TestValidateStageTransition_AllowedPaths(t *testing.T) {
	allowed := []struct {
		from, to TransferStage
	}{

		{TransferStagePendingOffer, TransferStageConsulting},
		{TransferStagePendingOffer, TransferStageCompleting},
		{TransferStagePendingOffer, TransferStageRecalling},
		{TransferStagePendingOffer, TransferStageDeclined},
		{TransferStagePendingOffer, TransferStageTimedOut},
		{TransferStagePendingOffer, TransferStageCancelled},
		{TransferStagePendingOffer, TransferStageFailed},

		{TransferStageConsulting, TransferStageCompleting},
		{TransferStageConsulting, TransferStageCancelled},
		{TransferStageConsulting, TransferStageFailed},

		{TransferStageCompleting, TransferStageCompleted},
		{TransferStageCompleting, TransferStageRecalled},
		{TransferStageCompleting, TransferStageRecalling},
		{TransferStageCompleting, TransferStageFailed},

		{TransferStageRecalling, TransferStageCompleting},
		{TransferStageRecalling, TransferStageCancelled},
		{TransferStageRecalling, TransferStageFailed},
	}
	for _, c := range allowed {
		if err := ValidateStageTransition(c.from, c.to); err != nil {
			t.Errorf("%q → %q: want nil, got %v", c.from, c.to, err)
		}
	}
}

func TestValidateStageTransition_Rejected(t *testing.T) {
	rejected := []struct {
		from, to TransferStage
	}{

		{TransferStagePendingOffer, TransferStagePendingOffer},

		// Terminals are one-way; nothing leaves a terminal stage.
		{TransferStageCompleted, TransferStageConsulting},
		{TransferStageCancelled, TransferStageCompleted},
		{TransferStageDeclined, TransferStagePendingOffer},
		{TransferStageTimedOut, TransferStageCompleted},
		{TransferStageRecalled, TransferStageCompleting},
		{TransferStageFailed, TransferStageRecalling},

		// completed is only reachable THROUGH completing (the media-swap window),
		// so observers can never see a completed transfer whose swap failed.
		{TransferStagePendingOffer, TransferStageCompleted},
		{TransferStageConsulting, TransferStageCompleted},
		{TransferStageRecalling, TransferStageCompleted},
		{TransferStageRecalling, TransferStageRecalled},

		{TransferStageConsulting, TransferStagePendingOffer},
		{TransferStageConsulting, TransferStageDeclined},
		{TransferStageConsulting, TransferStageTimedOut},
		{TransferStageConsulting, TransferStageRecalling},

		{TransferStageCompleting, TransferStagePendingOffer},
		{TransferStageCompleting, TransferStageConsulting},
	}
	for _, c := range rejected {
		if err := ValidateStageTransition(c.from, c.to); !errors.Is(err, ErrTransferInvalidStage) {
			t.Errorf("%q → %q: want ErrTransferInvalidStage, got %v", c.from, c.to, err)
		}
	}
}

func TestTransferRequest_Validate(t *testing.T) {
	base := TransferRequest{
		WorkspaceID:  "ws-1",
		InitiatorID:  "user-a",
		TargetUserID: "user-b",
		CallID:       "call-1",
		Kind:         TransferKindBlind,
	}

	if err := base.Validate(); err != nil {
		t.Fatalf("baseline valid request must pass: %v", err)
	}

	type mutFn func(r *TransferRequest)
	cases := []struct {
		name string
		mut  mutFn
		want error
	}{
		{"missing workspace", func(r *TransferRequest) { r.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"missing initiator", func(r *TransferRequest) { r.InitiatorID = "" }, ErrOwnerRequired},
		{"missing target", func(r *TransferRequest) { r.TargetUserID = "" }, ErrTransferTargetRequired},
		{"self transfer", func(r *TransferRequest) { r.TargetUserID = r.InitiatorID }, ErrTransferSelfTransfer},
		{"missing call", func(r *TransferRequest) { r.CallID = "" }, ErrTransferCallNotFound},
		{"bad kind", func(r *TransferRequest) { r.Kind = TransferKind("warm") }, ErrTransferInvalidKind},
		{"empty kind", func(r *TransferRequest) { r.Kind = "" }, ErrTransferInvalidKind},
	}
	for _, c := range cases {
		req := base
		c.mut(&req)
		if err := req.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

func TestTransferError_UnwrapAndIs(t *testing.T) {
	te := &TransferError{Err: ErrTransferTargetBusy, Reason: "user-b on call-9"}
	if !errors.Is(te, ErrTransferTargetBusy) {
		t.Fatalf("errors.Is must match underlying sentinel")
	}
	if te.Error() == ErrTransferTargetBusy.Error() {
		t.Fatalf("Error() must include reason when present")
	}
}

func TestTransferError_NilSafe(t *testing.T) {
	var te *TransferError
	if got := te.Error(); got == "" {
		t.Fatalf("nil receiver Error() must not panic and must return a stable string")
	}
	if got := te.Unwrap(); got != nil {
		t.Fatalf("nil receiver Unwrap() = %v, want nil", got)
	}
}
