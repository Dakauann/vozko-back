package launchcheck

import (
	"errors"
	"testing"
)

func TestAdmitLetsAHealthyCampaignStart(t *testing.T) {
	if err := Check(healthy()).Admit(""); err != nil {
		t.Fatalf("Admit = %v", err)
	}
}

func TestAdmitRefusesABlockEvenWhenSomethingWasConfirmed(t *testing.T) {
	s := healthy()
	s.Pending = 0
	s.Template.Quality = "RED"
	r := Check(s)

	err := r.Admit(r.Fingerprint())

	var refused *RefusedError
	if !errors.Is(err, ErrBlocked) || !errors.As(err, &refused) || len(refused.Report.Findings) == 0 {
		t.Fatalf("Admit = %v, want a blocked refusal carrying the report", err)
	}
}

func TestAdmitAsksForTheCurrentConfirmation(t *testing.T) {
	s := healthy()
	s.Template.Quality = "RED"
	r := Check(s)

	if err := r.Admit(""); !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("Admit(\"\") = %v, want unconfirmed", err)
	}
	if err := r.Admit("stale"); !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("Admit(stale) = %v, want unconfirmed", err)
	}
	if err := r.Admit(r.Fingerprint()); err != nil {
		t.Fatalf("Admit(current) = %v", err)
	}
}
