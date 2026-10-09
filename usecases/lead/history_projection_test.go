package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/lead"
)

func projectedHistory(t *testing.T, perms fakePermissions, defs *fakeDefinitions) *History {
	t.Helper()
	stored := classifiedLead()
	stored.Phones, stored.Relations = []lead.ContactPhone{}, []lead.Relation{}
	h, err := NewHistory(HistoryDeps{
		Leads: historyLeads{"l-1": stored}, Entries: historyEntries{}, Messages: historyMessages{}, Analyses: &historyAnalyses{},
		Access: &accessResolver{}, Windows: historyWindows{}, CampaignNames: historyCampaignNames{},
		Permissions: perms, Definitions: defs, Relatives: &historyRelatives{}, EntryLeads: historyEntryLeads{},
		Owners: &ownerNames{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTheDetailShowsOnlyWhatTheViewerMayRead(t *testing.T) {
	h := projectedHistory(t, fakePermissions{"leads:read": true}, &fakeDefinitions{defs: leadDefinitions()})
	detail, err := h.Detail(context.Background(), operator(), "l-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if !reflect.DeepEqual(detail.Lead.CustomFields, map[string]any{"cor": "azul"}) {
		t.Fatalf("custom fields = %v", detail.Lead.CustomFields)
	}
	if a := detail.Lead.Addresses[0].Postal; a.Street != "" || a.ZipCode != "" || a.District != "Bela Vista" {
		t.Fatalf("address = %+v", a)
	}
}

func TestTheDetailShowsEverythingToAManager(t *testing.T) {
	perms := withAddresses(withSensitive(fakePermissions{"leads:read": true}))
	detail, err := projectedHistory(t, perms, &fakeDefinitions{defs: leadDefinitions()}).Detail(context.Background(), operator(), "l-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Lead.CustomFields["classificacao"] != "positivo" || detail.Lead.Addresses[0].Postal.Street == "" {
		t.Fatalf("lead = %+v", detail.Lead)
	}
}

func TestTheDetailFailsWhenTheLeadFieldsCannotBeRead(t *testing.T) {
	h := projectedHistory(t, fakePermissions{"leads:read": true}, &fakeDefinitions{err: errors.New("db down")})
	if _, err := h.Detail(context.Background(), operator(), "l-1"); err == nil {
		t.Fatal("the detail must not answer without knowing which fields the viewer may see")
	}
}

func TestTheDetailRefusesSomeoneWhoCannotReadLeads(t *testing.T) {
	h := projectedHistory(t, fakePermissions{"leads:update": true}, &fakeDefinitions{defs: leadDefinitions()})
	if _, err := h.Detail(context.Background(), operator(), "l-1"); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("err = %v, want ErrLeadForbidden", err)
	}
}
