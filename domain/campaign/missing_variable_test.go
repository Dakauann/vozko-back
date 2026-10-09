package campaign

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/address"
	"vozko/domain/lead"
)

func TestEveryEmptySlotIsNamedWithItsSource(t *testing.T) {
	plan, err := PlanBindings([]VariableBinding{{Source: BindLiteral, Value: "Olá"}, {Source: BindDistrict}, {Source: CustomBinding("escola")}}, 3, bindingDefs())
	if err != nil {
		t.Fatalf("PlanBindings: %v", err)
	}
	district, school := MissingVariable{Slot: 2, Source: BindDistrict}, MissingVariable{Slot: 3, Source: CustomBinding("escola")}
	cases := []struct {
		name string
		lead *lead.Lead
		want []MissingVariable
	}{
		{name: "no primary address", lead: &lead.Lead{CustomFields: map[string]any{"escola": "Prisma"}}, want: []MissingVariable{district}},
		{name: "no custom value", lead: &lead.Lead{Addresses: []lead.Address{{Primary: true, Postal: address.Postal{District: "Centro"}}}}, want: []MissingVariable{school}},
		{name: "both empty", lead: &lead.Lead{}, want: []MissingVariable{district, school}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values, err := plan.Resolve(BindingSubject{Lead: tc.lead})
			if !errors.Is(err, ErrMissingVariable) || values != nil {
				t.Fatalf("Resolve = %v %v, want ErrMissingVariable and no values", values, err)
			}
			got, ok := MissingVariablesOf(err)
			if !ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MissingVariablesOf = %+v %v, want %+v", got, ok, tc.want)
			}
		})
	}
	if _, ok := MissingVariablesOf(errors.New("other")); ok {
		t.Fatal("an unrelated error named a slot")
	}
	if _, ok := MissingVariablesOf(nil); ok {
		t.Fatal("no error named a slot")
	}
	if _, ok := MissingVariablesOf(&MissingVariableError{}); ok {
		t.Fatal("an error without slots named a slot")
	}
}

func TestMissingVariablesMessageRoundTrips(t *testing.T) {
	district, school := MissingVariable{Slot: 2, Source: BindDistrict}, MissingVariable{Slot: 1, Source: CustomBinding("escola")}
	long := MissingVariable{Slot: 3, Source: CustomBinding(strings.Repeat("k", 600))}
	cases := []struct {
		name    string
		missing []MissingVariable
		message string
		parsed  []MissingVariable
	}{
		{name: "a lead field", missing: []MissingVariable{district}, message: "missing_variable:2:lead.district", parsed: []MissingVariable{district}},
		{name: "a custom field", missing: []MissingVariable{school}, message: "missing_variable:1:lead.custom:escola", parsed: []MissingVariable{school}},
		{name: "every empty slot in slot order", missing: []MissingVariable{district, school}, message: "missing_variable:1:lead.custom:escola;2:lead.district", parsed: []MissingVariable{school, district}},
		{name: "sources too long for the entry keep the slots", missing: []MissingVariable{district, long}, message: "missing_variable:2;3", parsed: []MissingVariable{{Slot: 2}, {Slot: 3}}},
		{name: "a source holding the separator keeps its slot", missing: []MissingVariable{{Slot: 4, Source: CustomBinding("a;b")}, district}, message: "missing_variable:2:lead.district;4", parsed: []MissingVariable{district, {Slot: 4}}},
		{name: "no slot known", message: "missing_variable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MissingVariablesMessage(tc.missing)
			if got != tc.message {
				t.Fatalf("MissingVariablesMessage = %q, want %q", got, tc.message)
			}
			parsed, ok := ParseMissingVariables(got)
			if ok != (tc.parsed != nil) || !reflect.DeepEqual(parsed, tc.parsed) {
				t.Fatalf("ParseMissingVariables = %+v %v, want %+v", parsed, ok, tc.parsed)
			}
		})
	}
}

func TestAMessageForEverySlotStaysWithinTheColumn(t *testing.T) {
	var missing []MissingVariable
	for slot := 1; slot <= 300; slot++ {
		missing = append(missing, MissingVariable{Slot: slot, Source: BindDistrict})
	}
	message := MissingVariablesMessage(missing)
	if len(message) > MaxSkipMessage {
		t.Fatalf("message is %d bytes, over the column", len(message))
	}
	parsed, ok := ParseMissingVariables(message)
	if !ok || len(parsed) == 0 || parsed[0].Slot != 1 {
		t.Fatalf("ParseMissingVariables = %+v %v", parsed, ok)
	}
}

func TestParseMissingVariablesRefusesWhatNamesNoSlot(t *testing.T) {
	for _, message := range []string{
		"", "missing_variable", "cooldown", "missing_variable:", "missing_variable:x:lead.name", "missing_variable:0:lead.name",
		"missing_variable:-1", "missing_variable:2:", "blocked:2:lead.name", "missing_variable:1;", "missing_variable:2;1", "missing_variable:1;1", "missing_variable:1;x",
	} {
		if got, ok := ParseMissingVariables(message); ok {
			t.Fatalf("ParseMissingVariables(%q) = %+v, want refused", message, got)
		}
	}
}

func TestASkipOutcomeRecordsItsDetailOnlyForItsOwnReason(t *testing.T) {
	district := MissingVariable{Slot: 2, Source: BindDistrict}
	detail := SkipDetail{Missing: []MissingVariable{district}, CooldownDays: 30}
	cases := []struct {
		name    string
		reason  SkipReason
		detail  SkipDetail
		status  SendStatus
		code    int
		message string
	}{
		{name: "the slot is recorded", reason: SkipMissingVariable, detail: detail, status: SendStatusFailed, code: 920006, message: "missing_variable:2:lead.district"},
		{name: "no slot known", reason: SkipMissingVariable, status: SendStatusFailed, code: 920006, message: "missing_variable"},
		{name: "the days in force are recorded", reason: SkipCooldown, detail: detail, status: SendStatusNotEligiblePossibleSpam, code: 920004, message: "cooldown:30"},
		{name: "no days known", reason: SkipCooldown, status: SendStatusNotEligiblePossibleSpam, code: 920004, message: "cooldown"},
		{name: "another reason won", reason: SkipBlocked, detail: detail, status: SendStatusFailed, code: 920001, message: "blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code, message := tc.reason.OutcomeWith(tc.detail)
			if status != tc.status || code != tc.code || message != tc.message {
				t.Fatalf("OutcomeWith = (%s, %d, %q)", status, code, message)
			}
			if got := ParseSkipDetail(tc.reason, message); tc.reason != SkipBlocked && !reflect.DeepEqual(got, onlyFor(tc.reason, tc.detail)) {
				t.Fatalf("ParseSkipDetail = %+v, want %+v", got, onlyFor(tc.reason, tc.detail))
			}
		})
	}
}

func onlyFor(reason SkipReason, detail SkipDetail) SkipDetail {
	if reason == SkipCooldown {
		return SkipDetail{CooldownDays: detail.CooldownDays}
	}
	return SkipDetail{Missing: detail.Missing}
}

func TestParseSkipDetailReadsOnlyTheDetailOfItsReason(t *testing.T) {
	cases := []struct {
		reason  SkipReason
		message string
		want    SkipDetail
	}{
		{reason: SkipCooldown, message: "cooldown:7", want: SkipDetail{CooldownDays: 7}},
		{reason: SkipCooldown, message: "cooldown", want: SkipDetail{}},
		{reason: SkipCooldown, message: "cooldown:0", want: SkipDetail{}},
		{reason: SkipCooldown, message: "cooldown:-3", want: SkipDetail{}},
		{reason: SkipCooldown, message: "cooldown:x", want: SkipDetail{}},
		{reason: SkipCooldown, message: "missing_variable:1:lead.name", want: SkipDetail{}},
		{reason: SkipMissingVariable, message: "cooldown:7", want: SkipDetail{}},
		{reason: SkipBlocked, message: "missing_variable:1:lead.name", want: SkipDetail{}},
	}
	for _, tc := range cases {
		if got := ParseSkipDetail(tc.reason, tc.message); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("ParseSkipDetail(%s, %q) = %+v, want %+v", tc.reason, tc.message, got, tc.want)
		}
	}
}

func TestOnlyTheCooldownAndMissingVariableCodesCarryDetail(t *testing.T) {
	if got := DetailedSkipCodes(); !reflect.DeepEqual(got, []int{920004, 920006}) {
		t.Fatalf("DetailedSkipCodes = %v", got)
	}
}

func TestNewReviewListsTheMissingVariablesBySlot(t *testing.T) {
	district, school := MissingVariable{Slot: 2, Source: BindDistrict}, MissingVariable{Slot: 1, Source: CustomBinding("escola")}
	parts := []SendPart{{CampaignID: "a", Status: StatusStopped}, {CampaignID: "b", Status: StatusStopped}}
	tallies := []PartTally{
		{CampaignID: "a", Entries: 20, Missing: map[MissingVariable]int{district: 4, school: 2}, Tally: Tally{Eligible: 15, Skipped: map[SkipReason]int{SkipMissingVariable: 5}}},
		{CampaignID: "b", Entries: 10, Missing: map[MissingVariable]int{district: 3}, Tally: Tally{Eligible: 7, Skipped: map[SkipReason]int{SkipMissingVariable: 3}}},
	}
	review := NewReview(ChannelOfficial, parts, tallies)
	want := []MissingVariableCount{{Slot: 1, Source: CustomBinding("escola"), Count: 2}, {Slot: 2, Source: BindDistrict, Count: 7}}
	if !reflect.DeepEqual(review.MissingVariables, want) {
		t.Fatalf("MissingVariables = %+v, want %+v", review.MissingVariables, want)
	}
	if review.Skipped[SkipMissingVariable] != 8 {
		t.Fatalf("skipped = %v", review.Skipped)
	}
	if none := NewReview(ChannelOfficial, parts[:1], []PartTally{{CampaignID: "a"}}); none.MissingVariables == nil || len(none.MissingVariables) != 0 {
		t.Fatalf("no missing slot = %#v, want an empty list", none.MissingVariables)
	}
}

func TestNewReviewStatesTheLongestCooldownTheEntriesRecorded(t *testing.T) {
	parts := []SendPart{{CampaignID: "a", Status: StatusStopped}, {CampaignID: "b", Status: StatusStopped}}
	tallies := []PartTally{
		{CampaignID: "a", Entries: 3, CooldownDays: 7, Tally: Tally{Eligible: 1, Skipped: map[SkipReason]int{SkipCooldown: 2}}},
		{CampaignID: "b", Entries: 3, CooldownDays: 30, Tally: Tally{Eligible: 2, Skipped: map[SkipReason]int{SkipCooldown: 1}}},
	}
	if review := NewReview(ChannelOfficial, parts, tallies); review.CooldownDays != 30 || review.Skipped[SkipCooldown] != 3 {
		t.Fatalf("review = cooldown days %d skipped %v, want 30", review.CooldownDays, review.Skipped)
	}
	unrecorded := []PartTally{{CampaignID: "a", Entries: 1, Tally: Tally{Skipped: map[SkipReason]int{SkipCooldown: 1}}}}
	if review := NewReview(ChannelOfficial, parts[:1], unrecorded); review.CooldownDays != 0 {
		t.Fatalf("cooldown days = %d with no entry recording them", review.CooldownDays)
	}
}

func TestAClonedReviewKeepsItsOwnMissingVariables(t *testing.T) {
	r := &SendReview{MissingVariables: []MissingVariableCount{{Slot: 1, Source: BindName, Count: 2}}}
	c := r.Clone()
	r.MissingVariables[0].Count = 9
	if c.MissingVariables[0].Count != 2 {
		t.Fatalf("the clone followed the original: %+v", c.MissingVariables)
	}
}
