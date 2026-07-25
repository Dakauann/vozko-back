package opportunity

import (
	"errors"
	"testing"
)

func baseOpp() *Opportunity {
	return &Opportunity{
		WorkspaceID: "ws1",
		LeadID:      "lead1",
		PipelineID:  "pipe1",
		StageID:     "stage1",
		OwnerID:     "u1",
		Title:       "Cauan - Plano Pro",
		ValueCents:  490000,
		Currency:    "BRL",
		Status:      StatusOpen,
	}
}

func TestValidate_OK(t *testing.T) {
	if err := baseOpp().Validate(); err != nil {
		t.Fatalf("expected valid opportunity, got %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Opportunity)
		want   error
	}{
		{"no workspace", func(o *Opportunity) { o.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"no pipeline", func(o *Opportunity) { o.PipelineID = "" }, ErrPipelineRequired},
		{"no stage", func(o *Opportunity) { o.StageID = "" }, ErrStageRequired},
		{"no title or lead", func(o *Opportunity) { o.Title = ""; o.LeadID = "" }, ErrTitleOrLead},
		{"bad status", func(o *Opportunity) { o.Status = "pending" }, ErrInvalidStatus},
		{"negative value", func(o *Opportunity) { o.ValueCents = -1 }, ErrNegativeValue},
		{"lost needs reason", func(o *Opportunity) { o.Status = StatusLost }, ErrLostReasonMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := baseOpp()
			tc.mutate(o)
			err := o.Validate()
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidate_LostWithReason(t *testing.T) {
	o := baseOpp()
	o.Status = StatusLost
	o.LostReasonID = "reason-price"
	if err := o.Validate(); err != nil {
		t.Fatalf("lost with a reason should be valid, got %v", err)
	}
}

func TestTitleOrLeadEitherSatisfies(t *testing.T) {
	o := baseOpp()
	o.Title = ""
	if err := o.Validate(); err != nil {
		t.Fatalf("a lead alone should satisfy title-or-lead, got %v", err)
	}
	o.LeadID = ""
	o.Title = "Standalone deal"
	if err := o.Validate(); err != nil {
		t.Fatalf("a title alone should satisfy title-or-lead, got %v", err)
	}
}

func TestNormalizeDefaults(t *testing.T) {
	o := &Opportunity{WorkspaceID: "ws1", LeadID: "l1", PipelineID: "p1", StageID: "s1", Title: "  Deal  "}
	o.Normalize()
	if o.Title != "Deal" {
		t.Fatalf("title should be trimmed, got %q", o.Title)
	}
	if o.Currency != DefaultCurrency {
		t.Fatalf("currency should default to %s, got %q", DefaultCurrency, o.Currency)
	}
	if o.Status != StatusOpen {
		t.Fatalf("status should default to open, got %q", o.Status)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("normalized opportunity should validate, got %v", err)
	}
}

func TestIsClosed(t *testing.T) {
	o := baseOpp()
	if o.IsClosed() {
		t.Fatal("open deal should not be closed")
	}
	o.Status = StatusWon
	if !o.IsClosed() {
		t.Fatal("won deal should be closed")
	}
}
