package customerportfolio

import (
	"errors"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func TestNewEntryNeedsWorkspaceHolderAndValidSubject(t *testing.T) {
	if _, err := New("", "u1", "u1", LeadSubject("l1"), at); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("missing workspace = %v", err)
	}
	if _, err := New("ws", " ", "u1", LeadSubject("l1"), at); !errors.Is(err, ErrHolderRequired) {
		t.Fatalf("missing holder = %v", err)
	}
	if _, err := New("ws", "u1", "u1", Subject{}, at); !errors.Is(err, ErrSubjectInvalid) {
		t.Fatalf("missing subject = %v", err)
	}
	e, err := New("ws", " u1 ", "u2", Subject{LeadID: " l1 "}, at)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if e.HolderID != "u1" || e.AddedBy != "u2" || e.Subject.LeadID != "l1" || !e.AddedAt.Equal(at) || !e.UpdatedAt.Equal(at) {
		t.Fatalf("entry = %+v", e)
	}
}

func TestClaimRefusesACustomerHeldByAColleague(t *testing.T) {
	wanted := Entry{HolderID: "u1", Subject: LeadSubject("l1")}

	got, created, err := Claim(nil, wanted)
	if err != nil || !created || got.HolderID != "u1" {
		t.Fatalf("free customer = %+v %v %v", got, created, err)
	}

	same := &Entry{ID: "e1", HolderID: "u1", Subject: LeadSubject("l1")}
	got, created, err = Claim(same, wanted)
	if err != nil || created || got.ID != "e1" {
		t.Fatalf("already mine must be a no-op = %+v %v %v", got, created, err)
	}

	other := &Entry{ID: "e2", HolderID: "u2", Subject: LeadSubject("l1")}
	_, _, err = Claim(other, wanted)
	if !errors.Is(err, ErrHeldByOther) {
		t.Fatalf("colleague's customer = %v, want ErrHeldByOther", err)
	}
	var held *HeldByOther
	if !errors.As(err, &held) || held.Entry.ID != "e2" || held.Entry.HolderID != "u2" {
		t.Fatalf("the refusal must name the current holder, got %#v", err)
	}
}

func TestMoveChangesTheHolderAndRefusesNoOps(t *testing.T) {
	e := Entry{ID: "e1", HolderID: "u1", UpdatedAt: at}
	later := at.Add(time.Hour)

	moved, changed, err := e.MoveTo(" u2 ", later)
	if err != nil || !changed || moved.HolderID != "u2" || !moved.UpdatedAt.Equal(later) {
		t.Fatalf("move = %+v %v %v", moved, changed, err)
	}
	if _, changed, err := e.MoveTo("u1", later); err != nil || changed {
		t.Fatalf("same holder must be a no-op = %v %v", changed, err)
	}
	if _, _, err := e.MoveTo("", later); !errors.Is(err, ErrHolderRequired) {
		t.Fatalf("blank holder = %v", err)
	}
}
