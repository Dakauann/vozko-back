package audiencecheck

import (
	"context"
	"testing"

	"vozko/domain/campaignaudience"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

type Audience struct {
	Service *campaignaudience_usecase.Service
	Actor   campaignaudience_usecase.Actor
	Channel campaignaudience.Channel
	Params  campaignaudience_usecase.Params
	Spec    campaignaudience.Spec
}

func (a Audience) Leads(t testing.TB, filter campaignaudience.LeadFilter) campaignaudience_usecase.LeadsPage {
	t.Helper()
	page, err := a.Service.Leads(context.Background(), a.Actor, a.Channel, a.Params, a.Spec, campaignaudience_usecase.LeadsQuery{Filter: filter, Page: 1, Limit: campaignaudience.MaxRowsPage})
	if err != nil {
		t.Fatalf("%s leads %+v: %v", a.Channel, filter, err)
	}
	return page
}

func (a Audience) RequireLeadsMatchTheCount(t testing.TB) campaignaudience.Count {
	t.Helper()
	count, err := a.Service.Count(context.Background(), a.Actor, a.Channel, a.Params, a.Spec)
	if err != nil {
		t.Fatalf("%s count: %v", a.Channel, err)
	}
	expect := func(filter campaignaudience.LeadFilter, want int) {
		if got := a.Leads(t, filter).Total; got != want {
			t.Fatalf("%s %+v lists %d leads, the count says %d", a.Channel, filter, got, want)
		}
	}
	expect(campaignaudience.LeadFilter{}, count.Matched)
	for reason, n := range count.Out {
		expect(campaignaudience.LeadFilter{Reason: reason}, n)
	}
	for _, v := range count.Variables {
		expect(campaignaudience.LeadFilter{Variable: v.Key}, v.Missing)
	}
	for _, gap := range count.MetadataMissing {
		expect(campaignaudience.LeadFilter{MetadataKey: gap.Key}, gap.Missing)
	}
	if count.Sendable != nil {
		expect(campaignaudience.LeadFilter{Status: campaignaudience.LeadReady}, *count.Sendable)
	}
	return count
}
