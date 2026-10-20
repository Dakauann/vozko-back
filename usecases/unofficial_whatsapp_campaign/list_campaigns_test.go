package unofficial_whatsapp_campaign

import (
	"context"
	"reflect"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type listedCampaigns struct {
	*fakeCampaignRepo
	inputs []uwc.ListCampaignsInput
}

func (l *listedCampaigns) List(in uwc.ListCampaignsInput) (*shared.PaginatedResult[*uwc.Campaign], error) {
	l.inputs = append(l.inputs, in)
	l.mu.Lock()
	defer l.mu.Unlock()
	items := []*uwc.Campaign{}
	for _, id := range []string{"camp-g", "camp-l"} {
		if c, ok := l.campaigns[id]; ok {
			clone := *c
			items = append(items, &clone)
		}
	}
	return &shared.PaginatedResult[*uwc.Campaign]{Items: items, Page: 1, PageSize: 15, TotalItems: int64(len(items)), TotalPages: 1}, nil
}

func TestTheCampaignListNamesTheFirstGroupsOfAGroupCampaign(t *testing.T) {
	campaigns := &listedCampaigns{fakeCampaignRepo: newFakeCampaignRepo()}
	entries := newFakeEntryRepo()
	campaigns.put(&uwc.Campaign{ID: "camp-g", WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, Status: campaign.StatusRunning})
	campaigns.put(&uwc.Campaign{ID: "camp-l", WorkspaceID: "ws-1", Audience: uwc.AudienceLeads, InstanceID: "inst-1"})
	for i, name := range []string{"Ofertas VIP #1", "Ofertas VIP #2", "Avisos", "Clientes"} {
		entry := uwc.NewGroupEntry("camp-g", "ws-1", "12036300000000000"+string(rune('1'+i))+"@g.us", name, time.Now())
		entry.ID = "g" + string(rune('1'+i))
		entries.put(&entry)
	}
	entries.put(&uwc.Entry{ID: "l1", CampaignID: "camp-l", LeadID: "lead-1", Name: "Ana", Status: campaign.SendStatusPending})

	uc := NewListCampaignsUseCase(campaigns, entries, &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected}})
	result, err := uc.Execute(context.Background(), uwc.ListCampaignsInput{WorkspaceID: "ws-1", Audience: uwc.AudienceGroups})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	byID := map[string]*uwc.Campaign{}
	for _, c := range result.Items {
		byID[c.ID] = c
	}
	if names := byID["camp-g"].TargetNames; len(names) != listTargetNames {
		t.Fatalf("group target names = %v, want the first %d", names, listTargetNames)
	}
	if names := byID["camp-l"].TargetNames; names != nil {
		t.Fatalf("person campaign target names = %v, want none", names)
	}
	if got := campaigns.inputs[0].Audience; got != uwc.AudienceGroups {
		t.Fatalf("audience filter = %q, want it passed to the repository", got)
	}
}

func TestEntriesNameTheNumberThatSentThem(t *testing.T) {
	entries := &listedEntries{fakeEntryRepo: newFakeEntryRepo(), rows: []*uwc.EntryWithLead{
		{Entry: &uwc.Entry{ID: "g1", SenderInstanceID: "inst-2"}},
		{Entry: &uwc.Entry{ID: "g2"}},
		{Entry: &uwc.Entry{ID: "g3", SenderInstanceID: "inst-gone"}},
	}}
	gateway := &fakeGateway{byID: map[string]*uw.Instance{"inst-2": {ID: "inst-2", WorkspaceID: "ws-1", DisplayName: "Vendas 2"}}}

	result, err := NewListEntriesUseCase(entries, gateway).Execute(uwc.ListEntriesInput{CampaignID: "camp-g"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	labels := []string{}
	for _, row := range result.Items {
		labels = append(labels, row.Entry.SenderLabel)
	}
	if !reflect.DeepEqual(labels, []string{"Vendas 2", "", ""}) {
		t.Fatalf("sender labels = %v, want only the known sender named", labels)
	}
}

type listedEntries struct {
	*fakeEntryRepo
	rows []*uwc.EntryWithLead
}

func (l *listedEntries) List(uwc.ListEntriesInput) (*shared.PaginatedResult[*uwc.EntryWithLead], error) {
	return &shared.PaginatedResult[*uwc.EntryWithLead]{Items: l.rows, Page: 1, PageSize: 20, TotalItems: int64(len(l.rows)), TotalPages: 1}, nil
}
