package campaign

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/whatsapp/template"
)

func TestSplitSelection(t *testing.T) {
	cases := []struct {
		name     string
		selected int
		split    bool
		want     []int
		err      error
	}{
		{name: "an empty selection", selected: 0, err: ErrSelectionEmpty},
		{name: "one campaign", selected: 1204, want: []int{1204}},
		{name: "exactly the cap", selected: MaxEntries, want: []int{MaxEntries}},
		{name: "over the cap without asking to split", selected: MaxEntries + 1, err: ErrSelectionOverCampaignCap},
		{name: "over the cap split evenly", selected: 187831, split: true, want: []int{93916, 93915}},
		{name: "three parts", selected: 2*MaxEntries + 2, split: true, want: []int{100001, 100001, 100000}},
		{name: "split asked on a small selection keeps one part", selected: 10, split: true, want: []int{10}},
		{name: "more parts than allowed", selected: MaxEntries*MaxSelectionParts + 1, split: true, err: ErrSelectionTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SplitSelection(tc.selected, tc.split)
			if !errors.Is(err, tc.err) {
				t.Fatalf("SplitSelection error = %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SplitSelection = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPartKeysAreStableAndCarryTheCount(t *testing.T) {
	keys := PartKeys("base", 2)
	if !reflect.DeepEqual(keys, []string{"base:1/2", "base:2/2"}) {
		t.Fatalf("PartKeys = %v", keys)
	}
	parts, ok := PartsOfKey("base:2/3")
	if !ok || parts != 3 {
		t.Fatalf("PartsOfKey = %d %v", parts, ok)
	}
	for _, bad := range []string{"", "base", "base:0/2", "base:3/2", "base:a/b"} {
		if _, ok := PartsOfKey(bad); ok {
			t.Fatalf("PartsOfKey(%q) accepted", bad)
		}
	}
}

func capOf(n int64) *int64 { return &n }

func TestSendBudgetDecidesHowManyGoOut(t *testing.T) {
	cases := []struct {
		name   string
		budget SendBudget
		firstN int
		want   int
		err    error
		fits   int
	}{
		{name: "everything fits", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 1000}, want: 100},
		{name: "everything fits under the monthly cap", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 1000, CapRemaining: capOf(100)}, want: 100},
		{name: "the balance covers part", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 555}, err: ErrUnaffordable, fits: 55},
		{name: "the cap covers part", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 5000, CapRemaining: capOf(40)}, err: ErrOverMonthlyCap, fits: 40},
		{name: "the tighter limit names the refusal", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 300, CapRemaining: capOf(40)}, err: ErrUnaffordable, fits: 30},
		{name: "the first N that fit", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 555}, firstN: 55, want: 55},
		{name: "the first N above what fits", budget: SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 555}, firstN: 56, err: ErrUnaffordable, fits: 55},
		{name: "the first N above the eligible", budget: SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: 5000}, firstN: 11, err: ErrFirstNInvalid},
		{name: "a negative first N", budget: SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: 5000}, firstN: -1, err: ErrFirstNInvalid},
		{name: "a negative balance", budget: SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: -5}, err: ErrUnaffordable, fits: 0},
		{name: "a used up cap", budget: SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: 5000, CapRemaining: capOf(0)}, err: ErrOverMonthlyCap, fits: 0},
		{name: "a free send only meets the cap", budget: SendBudget{Eligible: 10, CapRemaining: capOf(4)}, err: ErrOverMonthlyCap, fits: 4},
		{name: "a free send without cap", budget: SendBudget{Eligible: 10}, want: 10},
		{name: "a priced send without a price", budget: SendBudget{Eligible: 10, Priced: true, BalanceMicros: 5000}, err: template.ErrPricingUnavailable},
		{name: "a priced send with a negative price", budget: SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: -1, BalanceMicros: 5000}, err: template.ErrPricingUnavailable},
		{name: "nothing eligible", budget: SendBudget{Eligible: 0, Priced: true, UnitPriceMicros: 10}, err: ErrNothingEligible},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.budget.Allow(tc.firstN)
			if !errors.Is(err, tc.err) {
				t.Fatalf("Allow error = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("Allow = %d, want %d", got, tc.want)
			}
			var refusal *BudgetRefusal
			if errors.As(err, &refusal) && refusal.Fits != tc.fits {
				t.Fatalf("refusal fits = %d, want %d", refusal.Fits, tc.fits)
			}
			if tc.budget.Fits() < 0 {
				t.Fatalf("Fits went negative")
			}
		})
	}
}

func TestEstimatedDays(t *testing.T) {
	cases := []struct {
		eligible, cap, want int
	}{
		{0, 200, 0}, {1, 200, 1}, {200, 200, 1}, {201, 200, 2}, {1122, 250, 5}, {10, 0, 1},
	}
	for _, tc := range cases {
		if got := EstimatedDays(tc.eligible, tc.cap); got != tc.want {
			t.Fatalf("EstimatedDays(%d, %d) = %d, want %d", tc.eligible, tc.cap, got, tc.want)
		}
	}
}

func TestSelectionTemplateShape(t *testing.T) {
	if err := RefuseSelectionTemplate(0, false); err != nil {
		t.Fatalf("a body-only positional template was refused: %v", err)
	}
	if err := RefuseSelectionTemplate(1, false); !errors.Is(err, ErrHeaderVariableUnsupported) {
		t.Fatalf("header variable error = %v", err)
	}
	if err := RefuseSelectionTemplate(0, true); !errors.Is(err, ErrNamedParametersUnsupported) {
		t.Fatalf("named parameters error = %v", err)
	}
}

func TestSendErrorCodes(t *testing.T) {
	cases := map[error]string{
		ErrSelectionOverCampaignCap:   "send_selection_over_campaign_cap",
		ErrSelectionTooLarge:          "send_selection_too_large",
		ErrHeaderVariableUnsupported:  "send_header_variable_unsupported",
		ErrNamedParametersUnsupported: "send_named_parameters_unsupported",
		ErrUnaffordable:               "unaffordable",
		ErrOverMonthlyCap:             "over_cap",
		ErrFirstNInvalid:              "send_first_n_invalid",
		ErrNothingEligible:            "send_nothing_eligible",
		ErrCreationScopeMissing:       "send_creation_scope_missing",
		ErrDepartmentRequired:         "send_department_required",
		ErrNotFromSelection:           "send_not_from_selection",
		ErrAlreadyStarted:             "send_already_started",
		ErrSendIncomplete:             "send_incomplete",
		ErrBindingsMismatch:           "send_bindings_mismatch",
		ErrBindingUnknown:             "send_binding_unknown",
		ErrBindingLiteralEmpty:        "send_binding_literal_empty",
		ErrBindingFieldUnknown:        "send_binding_field_unknown",
		ErrBindingSensitive:           "send_binding_sensitive",
		ErrWorkflowNotFound:           "campaign_workflow_not_found",
		ErrWorkflowForbidden:          "campaign_workflow_forbidden",
		ErrWorkflowVarsMissing:        "campaign_workflow_vars_missing",
		ErrAgentNotFound:              "campaign_agent_not_found",
		ErrAgentVarsMissing:           "AGENT_REQUIRED_VARIABLE_MISSING",
		ErrAutomationUnavailable:      "campaign_automation_unavailable",
		ErrIdempotencyUnavailable:     "campaign_idempotency_unavailable",
		ErrLeadTargetsUnavailable:     "campaign_lead_targets_unavailable",
	}
	for err, want := range cases {
		if got := ErrorCode(errors.Join(errors.New("context"), err)); got != want {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
	refusal := &BudgetRefusal{Reason: ErrOverMonthlyCap, Fits: 3}
	if got := ErrorCode(refusal); got != "over_cap" {
		t.Fatalf("ErrorCode(refusal) = %q", got)
	}
	if ErrorCode(errors.New("other")) != "" {
		t.Fatal("an unknown error got a code")
	}
}

func TestCampaignSkipFollowsTheEligibilityOrder(t *testing.T) {
	cases := []struct {
		running, missing, overCap bool
		want                      SkipReason
	}{
		{want: ""},
		{running: true, want: SkipAlreadyInRunningCampaign},
		{missing: true, want: SkipMissingVariable},
		{overCap: true, want: SkipOverCap},
		{running: true, missing: true, overCap: true, want: SkipAlreadyInRunningCampaign},
		{missing: true, overCap: true, want: SkipMissingVariable},
	}
	for _, tc := range cases {
		if got := CampaignSkip(tc.running, tc.missing, tc.overCap); got != tc.want {
			t.Fatalf("CampaignSkip(%v, %v, %v) = %q, want %q", tc.running, tc.missing, tc.overCap, got, tc.want)
		}
		facts := sendable()
		facts.InRunningCampaign, facts.MissingVariable, facts.OverCap = tc.running, tc.missing, tc.overCap
		if _, reason := Eligibility(facts); reason != tc.want {
			t.Fatalf("Eligibility disagrees with CampaignSkip: %q vs %q", reason, tc.want)
		}
	}
}

func TestSendBudgetRefusalNamesTheLimitWithoutAFirstN(t *testing.T) {
	if err := (SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: 100}).Refusal(); err != nil {
		t.Fatalf("an affordable send was refused: %v", err)
	}
	if err := (SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: 50}).Refusal(); !errors.Is(err, ErrUnaffordable) {
		t.Fatalf("Refusal = %v, want ErrUnaffordable", err)
	}
	if err := (SendBudget{Eligible: 10, CapRemaining: capOf(3)}).Refusal(); !errors.Is(err, ErrOverMonthlyCap) {
		t.Fatalf("Refusal = %v, want ErrOverMonthlyCap", err)
	}
}

func TestNewReviewSumsThePartsAndSeesAStartedSend(t *testing.T) {
	parts := []SendPart{{CampaignID: "a", Name: "Matrículas (1/2)", Status: StatusStopped}, {CampaignID: "b", Name: "Matrículas (2/2)", Status: StatusStopped}}
	tallies := []PartTally{
		{CampaignID: "a", Entries: 100, Tally: Tally{Eligible: 90, Skipped: map[SkipReason]int{SkipBlocked: 10}, Counted: map[CountedReason]int{CountedWindowOpen: 3}}},
		{CampaignID: "b", Entries: 50, Tally: Tally{Eligible: 45, Skipped: map[SkipReason]int{SkipBlocked: 2, SkipOptedOut: 3}, Counted: map[CountedReason]int{CountedNoConsentRecorded: 7}}},
	}
	review := NewReview(ChannelOfficial, parts, tallies)
	if review.Entries != 150 || review.Eligible != 135 || review.Skipped[SkipBlocked] != 12 || review.Skipped[SkipOptedOut] != 3 {
		t.Fatalf("review = %+v", review)
	}
	if review.Counted[CountedWindowOpen] != 3 || review.Counted[CountedNoConsentRecorded] != 7 || review.Started {
		t.Fatalf("review = %+v", review)
	}
	if review.Parts[1].Eligible != 45 {
		t.Fatalf("part eligible = %d", review.Parts[1].Eligible)
	}
	tallies[1].Eligible = 40
	if !NewReview(ChannelOfficial, parts, tallies).Started {
		t.Fatal("entries that left PENDING without a skip code mean the send started")
	}
	parts[0].Status = StatusRunning
	tallies[1].Eligible = 45
	if !NewReview(ChannelOfficial, parts, tallies).Started {
		t.Fatal("a running part means the send started")
	}
}

func TestQuoteJudgeNamesWhatFits(t *testing.T) {
	var q SendQuote
	q.Judge(SendBudget{Eligible: 100, Priced: true, UnitPriceMicros: 10, BalanceMicros: 500, CapRemaining: capOf(80)})
	if q.Fits != 50 || q.Refusal != "unaffordable" || q.CapRemaining == nil || *q.CapRemaining != 80 {
		t.Fatalf("quote = %+v", q)
	}
	q = SendQuote{}
	q.Judge(SendBudget{Eligible: 10, Priced: true, UnitPriceMicros: 10, BalanceMicros: 500})
	if q.Fits != 10 || q.Refusal != "" {
		t.Fatalf("quote = %+v", q)
	}
}

func TestPartsNeeded(t *testing.T) {
	for count, want := range map[int]int{0: 0, 1: 1, MaxEntries: 1, MaxEntries + 1: 2, 187831: 2} {
		if got := PartsNeeded(count); got != want {
			t.Fatalf("PartsNeeded(%d) = %d, want %d", count, got, want)
		}
	}
}

func TestCompleteParts(t *testing.T) {
	ids, ok := CompleteParts("base", []KeyedPart{{ID: "b", Key: "base:2/2"}, {ID: "a", Key: "base:1/2"}})
	if !ok || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("CompleteParts = %v %v", ids, ok)
	}
	if _, ok := CompleteParts("base", []KeyedPart{{ID: "a", Key: "base:1/2"}}); ok {
		t.Fatal("a send missing a part was complete")
	}
	if _, ok := CompleteParts("base", nil); ok {
		t.Fatal("no part was complete")
	}
	if _, ok := CompleteParts("base", []KeyedPart{{ID: "a", Key: "other"}}); ok {
		t.Fatal("a foreign key was complete")
	}
}

func TestASendReviewBudgetsWhatItsChannelCharges(t *testing.T) {
	remaining := int64(70)
	official := SendReview{Channel: ChannelOfficial, Quote: SendQuote{UnitPriceMicros: 8000, BalanceMicros: 400000, CapRemaining: &remaining}}
	want := SendBudget{Eligible: 90, Priced: true, UnitPriceMicros: 8000, BalanceMicros: 400000, CapRemaining: &remaining}
	if got := official.Budget(90); got != want {
		t.Fatalf("official budget = %+v, want %+v", got, want)
	}
	unofficial := SendReview{Channel: ChannelUnofficial, Quote: SendQuote{UnitPriceMicros: 8000, BalanceMicros: 400000}}
	if got := unofficial.Budget(90); got != (SendBudget{Eligible: 90}) {
		t.Fatalf("unofficial budget = %+v", got)
	}
}

func TestAClonedSendReviewSharesNothingWithTheOriginal(t *testing.T) {
	remaining := int64(70)
	r := &SendReview{Parts: []SendPart{{Name: "a"}}, Skipped: map[SkipReason]int{SkipBlocked: 1}, Counted: map[CountedReason]int{CountedWindowOpen: 2}, Quote: SendQuote{CapRemaining: &remaining}}
	c := r.Clone()
	r.Parts[0].Name, r.Skipped[SkipBlocked], r.Counted[CountedWindowOpen], *r.Quote.CapRemaining = "b", 9, 9, 1
	if c.Parts[0].Name != "a" || c.Skipped[SkipBlocked] != 1 || c.Counted[CountedWindowOpen] != 2 || *c.Quote.CapRemaining != 70 {
		t.Fatalf("the clone followed the original: %+v", c)
	}
	var none *SendReview
	if none.Clone() != nil {
		t.Fatal("a nil review clones to nil")
	}
}
