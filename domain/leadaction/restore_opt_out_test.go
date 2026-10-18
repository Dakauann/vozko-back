package leadaction

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/workspace"
)

func restoreParams() Params {
	return Params{Note: "  o botão SAIR estava ligado à opção errada ", ConfirmCount: 120}
}

func TestRestoringOptOutsIsALeadActionRunGatedByConsent(t *testing.T) {
	if !ActionRestoreOptOut.Known() || !ActionRestoreOptOut.Runs() || ActionRestoreOptOut != "restore_opt_out" {
		t.Fatal("restore_opt_out is a known lead action run")
	}
	got := ActionRestoreOptOut.Requirements(restoreParams(), false)
	if !reflect.DeepEqual(got, []workspace.CapabilityKey{workspace.CapabilityLeadConsent}) {
		t.Fatalf("Requirements = %v", got)
	}
	if _, err := PermissionsOf(got); err != nil {
		t.Fatalf("leads.consent is not in the access catalog: %v", err)
	}
}

func TestRestoreParamsBelongToTheRestoreActionOnly(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		params Params
		want   error
	}{
		{"a restore with its note and count", ActionRestoreOptOut, restoreParams(), nil},
		{"a restore previewed before the note and count are typed", ActionRestoreOptOut, Params{}, nil},
		{"a restore with a key", ActionRestoreOptOut, Params{Note: "n", Key: "k"}, ErrParamsAmbiguous},
		{"a restore with an owner", ActionRestoreOptOut, Params{Note: "n", OwnerID: owner("u")}, ErrParamsAmbiguous},
		{"a restore with a block", ActionRestoreOptOut, Params{Note: "n", Blocked: blocked(false)}, ErrParamsAmbiguous},
		{"a restore with export columns", ActionRestoreOptOut, Params{Note: "n", Sensitive: true}, ErrParamsAmbiguous},
		{"a restore with call list parameters", ActionRestoreOptOut, Params{Note: "n", CallList: callListParams()}, ErrParamsAmbiguous},
		{"a classification carrying a note", ActionClassify, Params{Key: "k", Value: json.RawMessage(`1`), Note: "n"}, ErrParamsAmbiguous},
		{"a block carrying a typed count", ActionBlock, Params{Blocked: blocked(true), ConfirmCount: 3}, ErrParamsAmbiguous},
		{"an export carrying a note", ActionExport, Params{Note: "n"}, ErrParamsAmbiguous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.params.Validate(tc.action); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestARestoreCommitsOnlyWithItsNoteAndTheTypedCount(t *testing.T) {
	long := Params{Note: strings.Repeat("a", lead.MaxConsentExcerptLength+1), ConfirmCount: 1}
	cases := []struct {
		name   string
		params Params
		want   error
	}{
		{"with both", restoreParams(), nil},
		{"without the note", Params{ConfirmCount: 120}, ErrNoteRequired},
		{"with a blank note", Params{Note: "   ", ConfirmCount: 120}, ErrNoteRequired},
		{"with a note longer than an excerpt", long, ErrNoteTooLong},
		{"without the typed count", Params{Note: "n"}, selection.ErrCountRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.params.ValidateCommit(ActionRestoreOptOut); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateCommit = %v, want %v", err, tc.want)
			}
		})
	}
	if err := (Params{Blocked: blocked(true)}).ValidateCommit(ActionBlock); err != nil {
		t.Fatalf("other actions commit as before: %v", err)
	}
}

func TestTheTypedCountMustBeTheNumberOfSelectedLeads(t *testing.T) {
	if err := ConfirmSelected(ActionRestoreOptOut, restoreParams(), 120); err != nil {
		t.Fatalf("matching count: %v", err)
	}
	err := ConfirmSelected(ActionRestoreOptOut, restoreParams(), 121)
	var changed *selection.CountChangedError
	if !errors.As(err, &changed) || changed.Expected != 120 || changed.Matched != 121 {
		t.Fatalf("got %v, want the count changed refusal", err)
	}
	if err := ConfirmSelected(ActionBlock, Params{Blocked: blocked(true)}, 5); err != nil {
		t.Fatalf("other actions take no typed count: %v", err)
	}
}

func TestARestoreRunNeedsItsNoteAndTheTypedCount(t *testing.T) {
	req := RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionRestoreOptOut, Params: restoreParams(), IdempotencyKey: "k", Matched: 120, Selected: 120}
	run, err := NewRun(req, now)
	if err != nil || run.Params.Note != "o botão SAIR estava ligado à opção errada" {
		t.Fatalf("NewRun = %+v, %v", run, err)
	}
	req.Selected = 119
	if _, err := NewRun(req, now); !errors.Is(err, selection.ErrCountChanged) {
		t.Fatalf("NewRun with another count = %v", err)
	}
	req.Selected, req.Params.Note = 120, ""
	if _, err := NewRun(req, now); !errors.Is(err, ErrNoteRequired) {
		t.Fatalf("NewRun without the note = %v", err)
	}
}

func TestTheRestoreEditCarriesTheNoteAndWritesTheRevertEvent(t *testing.T) {
	edit, err := EditFor(ActionRestoreOptOut, restoreParams(), nil, customfield.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if edit.Kind != EditRestoreOptOut || edit.Note() != "o botão SAIR estava ligado à opção errada" || edit.EventKind != lead.EventConsentWithdrawalReverted || !edit.Recorded {
		t.Fatalf("edit = %+v", edit)
	}
	if edit.PendingFor(selection.Selection{Mode: selection.ModeFirstN, Limit: 10}) != nil {
		t.Fatal("a restore has no pending assignment to skip before a limit")
	}
}

func TestTheSkipReasonOfALeadThatCannotBeRestored(t *testing.T) {
	skipped := Tally{Unchanged: 2, Gone: 1, NotRestorable: 4}.Skipped()
	want := map[SkipReason]int{SkipUnchanged: 2, SkipGone: 1, SkipOptOutNotRestorable: 4}
	if !reflect.DeepEqual(skipped, want) || SkipOptOutNotRestorable != "lead_opt_out_not_restorable" {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestTheRestoreRefusalCodes(t *testing.T) {
	if ErrorCode(ErrNoteRequired) != "lead_action_note_required" || ErrorCode(ErrNoteTooLong) != "lead_action_note_too_long" {
		t.Fatal("codes")
	}
}
