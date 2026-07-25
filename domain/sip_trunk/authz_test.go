package sip_trunk

import (
	"errors"
	"testing"
)

func TestCanBeModifiedBy(t *testing.T) {
	tr := newValidTrunk() // WorkspaceID = "ws-1", non-global

	cases := []struct {
		name  string
		setup func(*SIPTrunk)
		actor Actor
		want  error
	}{
		{"admin can modify any trunk", nil, Actor{IsAdmin: true}, nil},
		{"admin can modify a global trunk", func(s *SIPTrunk) { s.IsGloballyVisible = true }, Actor{IsAdmin: true}, nil},
		{"owner can modify own non-global trunk", nil, Actor{WorkspaceID: "ws-1"}, nil},
		{"non-owner is rejected", nil, Actor{WorkspaceID: "ws-2"}, ErrTrunkForbidden},
		{"empty workspace actor is rejected", nil, Actor{}, ErrTrunkForbidden},
		{"owner cannot modify a globally-visible trunk", func(s *SIPTrunk) { s.IsGloballyVisible = true }, Actor{WorkspaceID: "ws-1"}, ErrTrunkForbidden},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr.IsGloballyVisible = false
			if c.setup != nil {
				c.setup(tr)
			}
			err := tr.CanBeModifiedBy(c.actor)
			if c.want == nil && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("expected %v, got %v", c.want, err)
			}
		})
	}
}

func TestApplyGlobalVisibility(t *testing.T) {
	t.Run("non-admin unchanged value is a no-op", func(t *testing.T) {
		tr := newValidTrunk()
		tr.IsGloballyVisible = false
		if err := tr.ApplyGlobalVisibility(false, Actor{WorkspaceID: "ws-1"}); err != nil {
			t.Fatalf("expected nil for no-op, got %v", err)
		}
		if tr.IsGloballyVisible {
			t.Fatalf("flag must remain false")
		}
	})

	t.Run("non-admin cannot publish globally", func(t *testing.T) {
		tr := newValidTrunk()
		tr.IsGloballyVisible = false
		if err := tr.ApplyGlobalVisibility(true, Actor{WorkspaceID: "ws-1"}); !errors.Is(err, ErrTrunkGlobalForbidden) {
			t.Fatalf("expected ErrTrunkGlobalForbidden, got %v", err)
		}
		if tr.IsGloballyVisible {
			t.Fatalf("flag must not flip for a non-admin")
		}
	})

	t.Run("non-admin cannot unpublish a global trunk", func(t *testing.T) {
		tr := newValidTrunk()
		tr.IsGloballyVisible = true
		if err := tr.ApplyGlobalVisibility(false, Actor{WorkspaceID: "ws-1"}); !errors.Is(err, ErrTrunkGlobalForbidden) {
			t.Fatalf("expected ErrTrunkGlobalForbidden, got %v", err)
		}
	})

	t.Run("admin can publish globally", func(t *testing.T) {
		tr := newValidTrunk()
		tr.IsGloballyVisible = false
		if err := tr.ApplyGlobalVisibility(true, Actor{IsAdmin: true}); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		if !tr.IsGloballyVisible {
			t.Fatalf("admin should be able to set the flag")
		}
	})
}
