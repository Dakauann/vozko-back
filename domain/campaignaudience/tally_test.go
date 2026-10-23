package campaignaudience

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
)

func whatsappBounds() []Bound {
	return []Bound{
		{Variable: Variable{Key: "1", Source: campaign.BindFirstName, Missing: MissingText, Text: "cliente"}, Suggested: true},
		{Variable: Variable{Key: "2", Source: campaign.BindDistrict, Missing: MissingExclude}},
		{Variable: Variable{Key: "3", Source: campaign.CustomBinding("serie")}},
	}
}

func TestTallyCountsEachLeadOnceAndEachMissingVariableForEveryLeadThatLacksIt(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceLeads, whatsappBounds())
	decisions := []Decision{
		tally.Add(Candidate{LeadID: "blocked", Contact: "5511900000001", HasContact: true, Refusal: string(campaign.SkipBlocked), Values: []string{"", "", ""}}),
		tally.Add(Candidate{LeadID: "no-phone", Refusal: string(campaign.SkipNoIdentity), Values: []string{"", "", ""}}),
		tally.Add(Candidate{LeadID: "fallback", Contact: "5511900000003", HasContact: true, Values: []string{"", "Centro", "1º ano"}}),
		tally.Add(Candidate{LeadID: "no-district", Contact: "5511900000004", HasContact: true, Values: []string{"Ana", "", ""}}),
		tally.Add(Candidate{LeadID: "undecided", Contact: "5511900000005", HasContact: true, Values: []string{"Bia", "Centro", ""}}),
	}
	wantOutcomes := []Outcome{OutcomeOut, OutcomeOut, OutcomeSend, OutcomeOut, OutcomeUndecided}
	for i, d := range decisions {
		if d.Outcome != wantOutcomes[i] {
			t.Fatalf("decision %d = %+v, want %s", i, d, wantOutcomes[i])
		}
	}
	if !reflect.DeepEqual(decisions[2].Values, []string{"cliente", "Centro", "1º ano"}) {
		t.Fatalf("the fallback text fills the empty slot: %q", decisions[2].Values)
	}
	if decisions[3].Reason != string(campaign.SkipMissingVariable) || !reflect.DeepEqual(decisions[3].Missing, []campaign.MissingVariable{{Slot: 2, Source: campaign.BindDistrict}}) {
		t.Fatalf("excluded decision = %+v", decisions[3])
	}
	count := tally.Count()
	if count.Matched != 5 || count.WithContact != 4 || count.Sendable != nil || count.OutTotal != 3 {
		t.Fatalf("count = %+v", count)
	}
	wantOut := map[string]int{"blocked": 1, "no_identity": 1, "missing_variable": 1}
	if !reflect.DeepEqual(count.Out, wantOut) {
		t.Fatalf("out = %+v", count.Out)
	}
	wantVariables := []VariableCount{
		{Key: "1", Source: campaign.BindFirstName, Missing: 1, Policy: MissingText, Text: "cliente", Suggested: true},
		{Key: "2", Source: campaign.BindDistrict, Missing: 1, Policy: MissingExclude},
		{Key: "3", Source: campaign.CustomBinding("serie"), Missing: 2, ChoiceRequired: true},
	}
	if !reflect.DeepEqual(count.Variables, wantVariables) {
		t.Fatalf("variables = %+v\nwant %+v", count.Variables, wantVariables)
	}
	if !reflect.DeepEqual(count.Blockers, []Blocker{BlockerChoiceRequired}) || count.RequiredVariables != 3 {
		t.Fatalf("blockers = %v required = %d", count.Blockers, count.RequiredVariables)
	}
}

func TestTallyGivesTheSendableNumberOnceEveryVariableHasAChoice(t *testing.T) {
	bounds := whatsappBounds()
	bounds[2].Missing = MissingText
	bounds[2].Text = "seu filho"
	tally := NewTally(ChannelUnofficial, SourceLeads, bounds)
	tally.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"Ana", "Centro", ""}})
	tally.Add(Candidate{LeadID: "b", HasContact: true, Values: []string{"Bia", "Centro", "1º ano"}})
	count := tally.Count()
	if count.Sendable == nil || *count.Sendable != 2 || len(count.Blockers) != 0 || count.OutTotal != 0 {
		t.Fatalf("count = %+v", count)
	}
	if count.Out == nil {
		t.Fatal("out is an empty map, never null")
	}
}

func TestTallyOfEmailUsesTheTagFallbackWhenNoChoiceWasMade(t *testing.T) {
	bounds := []Bound{
		{Variable: Variable{Key: "first_name"}, Fallback: "cliente"},
		{Variable: Variable{Key: "field.serie", Missing: MissingExclude}},
	}
	tally := NewTally(ChannelEmail, SourceLeads, bounds)
	send := tally.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"", "1º ano"}})
	out := tally.Add(Candidate{LeadID: "b", HasContact: true, Values: []string{"Bia", ""}})
	if send.Outcome != OutcomeSend || out.Outcome != OutcomeOut || out.Reason != "missing_variable" {
		t.Fatalf("decisions = %+v %+v", send, out)
	}
	count := tally.Count()
	if count.Sendable == nil || *count.Sendable != 1 || len(count.Blockers) != 0 {
		t.Fatalf("count = %+v", count)
	}
	if count.Variables[0].Fallback != "cliente" || count.Variables[0].ChoiceRequired || count.Variables[0].Missing != 1 {
		t.Fatalf("first_name = %+v", count.Variables[0])
	}
}

func TestTallyWaitsForASourceOnEveryWhatsAppSlot(t *testing.T) {
	bounds := whatsappBounds()
	bounds[2].Source = ""
	tally := NewTally(ChannelWhatsApp, SourceLeads, bounds)
	tally.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"Ana", "Centro", "anything"}})
	count := tally.Count()
	if count.Sendable != nil || !reflect.DeepEqual(count.Blockers, []Blocker{BlockerSourceRequired}) || count.Variables[2].Missing != 0 {
		t.Fatalf("count = %+v", count)
	}
}

func TestEmptyFixedTextKeepsTheVariableEditableAndBlocksCreation(t *testing.T) {
	for _, source := range []Source{SourceLeads, SourceList} {
		t.Run(string(source), func(t *testing.T) {
			bound := []Bound{{Variable: Variable{Key: "1", Source: campaign.BindLiteral, Value: "  "}, Name: "1"}}
			if _, err := campaign.PlanBindings(Bindings(bound), 1, nil); err != nil {
				t.Fatalf("draft binding plan: %v", err)
			}
			tally := NewTally(ChannelWhatsApp, source, bound)
			tally.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"-"}})
			count := tally.Count()
			if count.Sendable != nil || !reflect.DeepEqual(count.Blockers, []Blocker{BlockerSourceRequired}) {
				t.Fatalf("incomplete fixed text must block sending: %+v", count)
			}
			if len(count.Variables) != 1 || count.Variables[0].Source != campaign.BindLiteral {
				t.Fatalf("variable metadata was lost: %+v", count.Variables)
			}
			if err := tally.Creatable(); err != ErrSourceRequired {
				t.Fatalf("creation must stay blocked: %v", err)
			}
			bound[0].Value = "Dakauann"
			complete := NewTally(ChannelWhatsApp, source, bound)
			complete.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"Dakauann"}})
			if err := complete.Creatable(); err != nil {
				t.Fatalf("completed fixed text should be accepted: %v", err)
			}
		})
	}
}

func TestTallyOfAListCountsEveryRowMissingAColumnWithoutHidingTheSendableNumber(t *testing.T) {
	bounds := []Bound{{Variable: Variable{Key: "1"}}, {Variable: Variable{Key: "2"}}}
	tally := NewTally(ChannelWhatsApp, SourceList, bounds)
	full := tally.Add(Candidate{Contact: "5511900000001", HasContact: true, Values: []string{"Ana", "Centro"}})
	gap := tally.Add(Candidate{Contact: "5511900000002", HasContact: true, Values: []string{"Bia", ""}})
	tally.Add(Candidate{LeadID: "known", Contact: "5511900000003", HasContact: true, Refusal: "opted_out", Values: []string{"", "Vila"}})
	if full.Outcome != OutcomeSend || gap.Outcome != OutcomeSend || !reflect.DeepEqual(gap.Values, []string{"Bia", ""}) {
		t.Fatalf("decisions = %+v %+v", full, gap)
	}
	count := tally.Count()
	if count.Sendable == nil || *count.Sendable != 2 || !reflect.DeepEqual(count.Blockers, []Blocker{BlockerListVariablesMissing}) {
		t.Fatalf("count = %+v", count)
	}
	if count.Variables[1].Missing != 1 || count.Variables[0].Missing != 1 || count.Variables[1].ChoiceRequired {
		t.Fatalf("variables = %+v", count.Variables)
	}
}

func TestTallyRefusesAnUndecidedLeadAtCreation(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceLeads, whatsappBounds())
	tally.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"Ana", "Centro", ""}})
	if err := tally.Creatable(); err != ErrChoiceRequired {
		t.Fatalf("Creatable() = %v", err)
	}
	unbound := whatsappBounds()
	unbound[0].Source = ""
	if err := NewTally(ChannelWhatsApp, SourceLeads, unbound).Creatable(); err != ErrSourceRequired {
		t.Fatalf("unbound Creatable() = %v", err)
	}
	list := NewTally(ChannelWhatsApp, SourceList, []Bound{{Variable: Variable{Key: "1"}}})
	list.Add(Candidate{HasContact: true, Values: []string{""}})
	if err := list.Creatable(); err != ErrListVariablesMissing {
		t.Fatalf("list Creatable() = %v", err)
	}
}

func TestTheCountNamesEachSlotAndMarksTheHeader(t *testing.T) {
	slots := []Slot{{Key: "1", Name: "loja", Header: true}, {Key: "2", Name: "nome", Suggested: campaign.BindFirstName}}
	bound, err := Bind(slots, []Variable{{Key: "1", Source: campaign.BindLiteral, Value: "Prisma"}})
	if err != nil {
		t.Fatal(err)
	}
	tally := NewTally(ChannelWhatsApp, SourceLeads, bound)
	tally.Add(Candidate{LeadID: "a", HasContact: true, Values: []string{"Prisma", "Ana"}})
	got := tally.Count().Variables
	if got[0].Name != "loja" || !got[0].Header || got[1].Name != "nome" || got[1].Header {
		t.Fatalf("variables = %+v", got)
	}
}

func TestTallyAwaitingTheTemplateCountsEveryoneButBlocksTheCreate(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceLeads, nil).Awaiting(BlockerTemplateRequired)
	tally.Add(Candidate{LeadID: "a", HasContact: true})
	tally.Add(Candidate{LeadID: "b", Refusal: string(campaign.SkipNoIdentity)})
	count := tally.Count()
	if !reflect.DeepEqual(count.Blockers, []Blocker{BlockerTemplateRequired}) || count.Sendable == nil || *count.Sendable != 1 || count.OutTotal != 1 || len(count.Variables) != 0 {
		t.Fatalf("count = %+v", count)
	}
	if err := tally.Creatable(); !errors.Is(err, ErrTemplateRequired) {
		t.Fatalf("Creatable = %v", err)
	}
	unofficial := NewTally(ChannelUnofficial, SourceLeads, nil).Awaiting(BlockerMessageRequired)
	if err := unofficial.Creatable(); !errors.Is(err, ErrMessageRequired) || !reflect.DeepEqual(unofficial.Count().Blockers, []Blocker{BlockerMessageRequired}) {
		t.Fatalf("unofficial = %v %v", err, unofficial.Count().Blockers)
	}
	if ready := NewTally(ChannelWhatsApp, SourceLeads, nil).Awaiting(""); len(ready.Count().Blockers) != 0 {
		t.Fatalf("nothing awaited = %v", ready.Count().Blockers)
	}
}
