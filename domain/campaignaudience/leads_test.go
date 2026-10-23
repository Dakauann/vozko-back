package campaignaudience

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
)

func TestEachDecisionNamesTheEmptyVariablesAndMetadataTheCountCounted(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceLeads, whatsappBounds()).Requiring(needsSchoolAndClass)
	blocked := tally.Add(Candidate{LeadID: "blocked", HasContact: true, Refusal: string(campaign.SkipBlocked), Values: []string{"", "", ""}})
	excluded := tally.Add(Candidate{LeadID: "no-district", HasContact: true, Values: []string{"", "", "1º"}, Metadata: map[string]any{"escola": "Prisma"}})
	if len(blocked.Empty) != 0 || !reflect.DeepEqual(blocked.MetadataMissing, []string{"escola", "turma"}) {
		t.Fatalf("a refused lead = %+v", blocked)
	}
	if !reflect.DeepEqual(excluded.Empty, []string{"1", "2"}) || !reflect.DeepEqual(excluded.MetadataMissing, []string{"turma"}) {
		t.Fatalf("an excluded lead = %+v", excluded)
	}
	list := NewTally(ChannelWhatsApp, SourceList, whatsappBounds())
	row := list.Add(Candidate{Line: 4, Contact: "5511900000001", HasContact: true, Refusal: string(campaign.SkipOptedOut), Values: []string{"Ana", "", "1º"}})
	if !reflect.DeepEqual(row.Empty, []string{"2"}) {
		t.Fatalf("a list row = %+v", row)
	}
}

func TestTheAudienceLeadSaysItsStatusAndTheValuesItSends(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceLeads, whatsappBounds())
	cases := []struct {
		candidate Candidate
		want      AudienceLead
	}{
		{candidate: Candidate{LeadID: "ana", Name: "Ana", Contact: "5511900000001", HasContact: true, Values: []string{"", "Centro", "1º"}},
			want: AudienceLead{LeadID: "ana", Name: "Ana", Contact: "5511900000001", Status: LeadReady, Missing: []string{"1"}, MetadataMissing: []string{}, Variables: []string{"cliente", "Centro", "1º"}}},
		{candidate: Candidate{LeadID: "bia", Name: "Bia", HasContact: true, Values: []string{"Bia", "", "1º"}},
			want: AudienceLead{LeadID: "bia", Name: "Bia", Status: LeadOut, Reason: ReasonMissingVariable, Missing: []string{"2"}, MetadataMissing: []string{}, Variables: []string{"Bia", "", "1º"}}},
		{candidate: Candidate{LeadID: "caio", Name: "Caio", Refusal: string(campaign.SkipNoIdentity)},
			want: AudienceLead{LeadID: "caio", Name: "Caio", Status: LeadOut, Reason: string(campaign.SkipNoIdentity), Missing: []string{}, MetadataMissing: []string{}, Variables: []string{}}},
		{candidate: Candidate{LeadID: "duda", Name: "Duda", HasContact: true, Values: []string{"Duda", "Centro", ""}},
			want: AudienceLead{LeadID: "duda", Name: "Duda", Status: LeadUndecided, Missing: []string{"3"}, MetadataMissing: []string{}, Variables: []string{"Duda", "Centro", ""}}},
	}
	for _, tc := range cases {
		if got := LeadOf(tc.candidate, tally.Add(tc.candidate)); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("lead = %+v\nwant %+v", got, tc.want)
		}
	}
}

func TestTheLeadFilterKeepsWhatTheCountCountedUnderEachNumber(t *testing.T) {
	ana := AudienceLead{Name: "Ana Souza", Contact: "ana@escola.example", Status: LeadOut, Reason: "no_email", Missing: []string{"2"}, MetadataMissing: []string{"turma"}}
	cases := []struct {
		filter LeadFilter
		want   bool
	}{
		{filter: LeadFilter{}, want: true},
		{filter: LeadFilter{Status: LeadOut}, want: true},
		{filter: LeadFilter{Status: LeadReady}, want: false},
		{filter: LeadFilter{Reason: "no_email"}, want: true},
		{filter: LeadFilter{Reason: "blocked"}, want: false},
		{filter: LeadFilter{Variable: "2"}, want: true},
		{filter: LeadFilter{Variable: "1"}, want: false},
		{filter: LeadFilter{MetadataKey: "turma"}, want: true},
		{filter: LeadFilter{Search: "SOUZA"}, want: true},
		{filter: LeadFilter{Search: "escola.example"}, want: true},
		{filter: LeadFilter{Search: "bia"}, want: false},
	}
	for _, tc := range cases {
		if got := tc.filter.Matches(ana); got != tc.want {
			t.Fatalf("%+v = %v", tc.filter, got)
		}
	}
	if err := (LeadFilter{Status: "sent"}).Validate(); !errors.Is(err, ErrLeadStatusInvalid) {
		t.Fatalf("an unknown status = %v", err)
	}
	if err := (LeadFilter{Status: LeadUndecided}).Validate(); err != nil {
		t.Fatalf("undecided = %v", err)
	}
}

func TestAnAudienceLeadAlwaysCarriesItsLeadLineAndReasonKeys(t *testing.T) {
	raw, err := json.Marshal(LeadOf(Candidate{Name: "Ana", Contact: "5561999990001"}, Decision{}))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"leadId", "line", "reason", "missing", "metadataMissing", "variables"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("key %q missing from %s", key, raw)
		}
	}
}
