package campaignaudience_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/selection"
)

func leadsWorld(t *testing.T) (*world, campaignaudience.Spec) {
	w := newWorld(t)
	w.selections.ids = []string{"a", "b", "c", "d"}
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}, {Key: "2", Suggested: campaign.BindDistrict}}
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{
		"a": {Name: "Ana", Contact: "5511900000001", HasContact: true, Values: []string{"Ana", "Centro"}},
		"b": {Name: "Bia", Contact: "5511900000002", HasContact: true, Values: []string{"Bia", ""}},
		"c": {Name: "Caio", Refusal: string(campaign.SkipNoIdentity)},
		"d": {Name: "Duda", Contact: "5511900000004", HasContact: true, Values: []string{"", ""}},
	}}
	spec := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection(), Variables: []campaignaudience.Variable{
		{Key: "1", Missing: campaignaudience.MissingText, Text: "cliente"}, {Key: "2", Missing: campaignaudience.MissingExclude},
	}}
	return w, spec
}

func TestTheAudienceLeadsAreTheCountsLeadsWithTheirStatus(t *testing.T) {
	w, spec := leadsWorld(t)
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec)
	if err != nil {
		t.Fatal(err)
	}
	all, err := w.service.Leads(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec, LeadsQuery{Page: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != count.Matched || len(all.Items) != 4 {
		t.Fatalf("page = %+v", all)
	}
	want := campaignaudience.AudienceLead{LeadID: "d", Name: "Duda", Contact: "5511900000004", Status: campaignaudience.LeadOut, Reason: campaignaudience.ReasonMissingVariable, Missing: []string{"1", "2"}, MetadataMissing: []string{}, Variables: []string{"", ""}}
	if !reflect.DeepEqual(all.Items[3], want) {
		t.Fatalf("Duda = %+v", all.Items[3])
	}
	filters := []struct {
		filter campaignaudience.LeadFilter
		want   int
	}{
		{filter: campaignaudience.LeadFilter{Reason: campaignaudience.ReasonMissingVariable}, want: count.Out[campaignaudience.ReasonMissingVariable]},
		{filter: campaignaudience.LeadFilter{Reason: string(campaign.SkipNoIdentity)}, want: count.Out[string(campaign.SkipNoIdentity)]},
		{filter: campaignaudience.LeadFilter{Status: campaignaudience.LeadReady}, want: *count.Sendable},
		{filter: campaignaudience.LeadFilter{Variable: "2"}, want: count.Variables[1].Missing},
		{filter: campaignaudience.LeadFilter{Variable: "1"}, want: count.Variables[0].Missing},
	}
	for _, f := range filters {
		page, err := w.service.Leads(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec, LeadsQuery{Filter: f.filter, Page: 1, Limit: 50})
		if err != nil || page.Total != f.want || f.want == 0 {
			t.Fatalf("%+v = %d, count says %d (%v)", f.filter, page.Total, f.want, err)
		}
	}
	second, err := w.service.Leads(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec, LeadsQuery{Filter: campaignaudience.LeadFilter{Search: "bia"}, Page: 1, Limit: 1})
	if err != nil || second.Total != 1 || second.Items[0].LeadID != "b" {
		t.Fatalf("search = %+v, %v", second, err)
	}
}

func TestTheAudienceLeadsRefuseABadPageOrStatus(t *testing.T) {
	w, spec := leadsWorld(t)
	if _, err := w.service.Leads(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec, LeadsQuery{Page: 0, Limit: 10}); !errors.Is(err, ErrPageInvalid) {
		t.Fatalf("page 0 = %v", err)
	}
	if _, err := w.service.Leads(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec, LeadsQuery{Filter: campaignaudience.LeadFilter{Status: "sent"}, Page: 1, Limit: 10}); !errors.Is(err, campaignaudience.ErrLeadStatusInvalid) {
		t.Fatalf("bad status = %v", err)
	}
}

func TestPickedLeadsAreCountedListedAndCreatedWithoutAConfirmation(t *testing.T) {
	w, spec := leadsWorld(t)
	picked := selection.Selection{Mode: selection.ModeIDs, IDs: []string{"a", "d"}}
	spec.Selection = &picked
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec)
	if err != nil || count.Matched != 2 || count.Selection.ConfirmCount != 0 || count.Selection.Fingerprint != "" {
		t.Fatalf("count = %+v, %v", count, err)
	}
	page, err := w.service.Leads(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec, LeadsQuery{Page: 1, Limit: 10})
	if err != nil || page.Total != 2 {
		t.Fatalf("leads = %+v, %v", page, err)
	}
	got, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec)
	if err != nil || len(got) != 2 || got[0].LeadID != "a" || got[1].LeadID != "d" || got[1].Skip != campaign.SkipMissingVariable {
		t.Fatalf("recipients = %+v, %v", got, err)
	}
}
