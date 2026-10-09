package campaign

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/whatsapp/template"
)

func TestCheckSelectionSizeRefusesBeforeAnythingIsWritten(t *testing.T) {
	cases := []struct {
		name     string
		selected int
		split    bool
		parts    int
		err      error
	}{
		{name: "an empty selection", selected: 0, err: ErrSelectionEmpty},
		{name: "one campaign", selected: 10, parts: 1},
		{name: "over the cap without a split", selected: MaxEntries + 1, err: ErrSelectionOverCampaignCap},
		{name: "over the cap with a split", selected: MaxEntries + 1, split: true, parts: 2},
		{name: "over every part", selected: MaxEntries*MaxSelectionParts + 1, split: true, err: ErrSelectionTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := CheckSelectionSize(tc.selected, tc.split)
			if !errors.Is(err, tc.err) || parts != tc.parts {
				t.Fatalf("CheckSelectionSize = %d, %v; want %d, %v", parts, err, tc.parts, tc.err)
			}
		})
	}
}

func TestQuoteSizeTellsTheSplitAndRefusesOnlyWhatNoSplitHolds(t *testing.T) {
	var q SendQuote
	if err := q.Size(MaxEntries+1, false); err != nil {
		t.Fatalf("Size = %v", err)
	}
	if q.Count != MaxEntries+1 || q.Parts != 2 || !q.SplitRequired || q.MaxPerCampaign != MaxEntries {
		t.Fatalf("quote = %+v", q)
	}
	q = SendQuote{}
	if err := q.Size(MaxEntries+1, true); err != nil || q.SplitRequired {
		t.Fatalf("Size = %v, quote %+v", err, q)
	}
	if err := (&SendQuote{}).Size(MaxEntries*MaxSelectionParts+1, true); !errors.Is(err, ErrSelectionTooLarge) {
		t.Fatalf("Size = %v, want ErrSelectionTooLarge", err)
	}
}

func TestBaseOfKey(t *testing.T) {
	for key, want := range map[string]string{"base:1/2": "base", "a:b:2/2": "a:b", "nokey": "", ":1/1": ""} {
		if got := BaseOfKey(key); got != want {
			t.Fatalf("BaseOfKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestOneSendAcceptsOnlyTheCompletePartsOfOneSend(t *testing.T) {
	part := func(id, key, templateID string) SendPartRef {
		return SendPartRef{CampaignID: id, Key: key, TemplateID: templateID, BusinessPhoneID: "bp-1"}
	}
	cases := []struct {
		name  string
		parts []SendPartRef
		err   error
	}{
		{name: "both parts", parts: []SendPartRef{part("a", "base:1/2", "t"), part("b", "base:2/2", "t")}},
		{name: "half a send", parts: []SendPartRef{part("a", "base:1/2", "t")}, err: ErrSendIncomplete},
		{name: "parts of two templates", parts: []SendPartRef{part("a", "base:1/2", "t"), part("b", "base:2/2", "u")}, err: ErrSendIncomplete},
		{name: "a campaign without a key", parts: []SendPartRef{part("a", "", "t")}, err: ErrSendIncomplete},
		{name: "no part", err: ErrSendIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := OneSend(tc.parts); !errors.Is(err, tc.err) {
				t.Fatalf("OneSend = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestKeepFirstSpreadsTheAllowedCountInPartOrder(t *testing.T) {
	cases := []struct {
		name     string
		eligible []int
		allowed  int
		want     []int
	}{
		{name: "everything", eligible: []int{10, 5}, allowed: 15, want: []int{10, 5}},
		{name: "inside the first part", eligible: []int{10, 5}, allowed: 4, want: []int{4, 0}},
		{name: "into the second part", eligible: []int{10, 5}, allowed: 12, want: []int{10, 2}},
		{name: "nothing", eligible: []int{10, 5}, allowed: 0, want: []int{0, 0}},
		{name: "a negative count", eligible: []int{3}, allowed: -2, want: []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeepFirst(tc.eligible, tc.allowed); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("KeepFirst = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFirstSkipLetsTheLeadsOwnRefusalWin(t *testing.T) {
	if got := FirstSkip(SkipBlocked, SkipMissingVariable); got != SkipBlocked {
		t.Fatalf("FirstSkip = %q, want blocked", got)
	}
	if got := FirstSkip("", SkipMissingVariable); got != SkipMissingVariable {
		t.Fatalf("FirstSkip = %q, want missing_variable", got)
	}
	if got := FirstSkip(); got != "" {
		t.Fatalf("FirstSkip() = %q, want none", got)
	}
}

func TestOnlyTheStartOfASelectionSendIsGated(t *testing.T) {
	cases := []struct {
		source string
		action Action
		want   bool
	}{
		{source: SourceLeadSelection, action: ActionStart, want: true},
		{source: SourceLeadSelection, action: ActionPause},
		{source: SourceLeadSelection, action: ActionStop},
		{source: "", action: ActionStart},
	}
	for _, tc := range cases {
		if got := SelectionStartGated(tc.source, tc.action); got != tc.want {
			t.Fatalf("SelectionStartGated(%q, %q) = %v, want %v", tc.source, tc.action, got, tc.want)
		}
	}
}

func TestWhatASelectionSendDeliversCannotBeChangedLikeACampaign(t *testing.T) {
	if err := RefuseSelectionChange(SourceLeadSelection, true); !errors.Is(err, ErrSelectionSendLocked) {
		t.Fatalf("RefuseSelectionChange = %v, want ErrSelectionSendLocked", err)
	}
	if err := RefuseSelectionChange(SourceLeadSelection, false); err != nil {
		t.Fatalf("archiving or renaming a selection send was refused: %v", err)
	}
	if err := RefuseSelectionChange("", true); err != nil {
		t.Fatalf("a plain campaign was refused: %v", err)
	}
}

func TestTheNewSendRefusalsHaveCodes(t *testing.T) {
	for err, want := range map[error]string{
		ErrSelectionSendLocked:         "send_selection_locked",
		ErrSendPreparing:               "send_preparing",
		template.ErrPricingUnavailable: "send_pricing_unavailable",
	} {
		if got := ErrorCode(err); got != want {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestASelectionSendStartsOnlyFromItsReviewOrAResume(t *testing.T) {
	cases := []struct {
		name    string
		attempt StartAttempt
		err     error
	}{
		{name: "a generic start of a stopped send", attempt: StartAttempt{Source: SourceLeadSelection, Action: ActionStart, From: StatusStopped}, err: ErrSelectionStartNeedsReview},
		{name: "a generic start of a completed send", attempt: StartAttempt{Source: SourceLeadSelection, Action: ActionStart, From: StatusCompleted}, err: ErrSelectionStartNeedsReview},
		{name: "the reviewed start", attempt: StartAttempt{Source: SourceLeadSelection, Action: ActionStart, From: StatusStopped, Reviewed: true}},
		{name: "a resume of a paused send", attempt: StartAttempt{Source: SourceLeadSelection, Action: ActionStart, From: StatusPaused}},
		{name: "a pause", attempt: StartAttempt{Source: SourceLeadSelection, Action: ActionPause, From: StatusRunning}},
		{name: "a plain campaign", attempt: StartAttempt{Action: ActionStart, From: StatusStopped}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.attempt.RefuseUnreviewed(); !errors.Is(err, tc.err) {
				t.Fatalf("RefuseUnreviewed = %v, want %v", err, tc.err)
			}
		})
	}
	if !(StartAttempt{Source: SourceLeadSelection, Action: ActionStart}).Gated() || (StartAttempt{Action: ActionStart}).Gated() {
		t.Fatalf("only the start of a selection send is gated")
	}
	if got := ErrorCode(ErrSelectionStartNeedsReview); got != "send_start_from_leads" {
		t.Fatalf("ErrorCode = %q, want send_start_from_leads", got)
	}
}
