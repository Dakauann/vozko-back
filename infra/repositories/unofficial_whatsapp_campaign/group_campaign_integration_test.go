package unofficial_whatsapp_campaign_repository

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func groupCampaignDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "uwc_groups")
	if err := db.Exec(`CREATE TABLE leads (id uuid PRIMARY KEY, workspace_id uuid, number varchar(20), name varchar(255), deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaignEntry{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAGroupCampaignIsStoredWithoutANumberAndWithTheNumbersItMayUse(t *testing.T) {
	db := groupCampaignDB(t)
	repo, entries, summary := NewRepository(db), NewEntryRepository(db), NewSummaryRepository(db)
	ws, n1, n2, n3 := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()

	groups := &uwc.Campaign{ID: uuid.NewString(), WorkspaceID: ws, Name: "Ofertas", Audience: uwc.AudienceGroups, Status: campaign.StatusStopped, SenderInstanceIDs: []string{n1, n2}}
	people := &uwc.Campaign{ID: uuid.NewString(), WorkspaceID: ws, InstanceID: n3, Name: "Leads", Audience: uwc.AudienceLeads, Status: campaign.StatusStopped}
	for _, c := range []*uwc.Campaign{groups, people} {
		if err := repo.Create(c); err != nil {
			t.Fatalf("create %s: %v", c.Name, err)
		}
	}
	now := time.Now().UTC()
	var made []uwc.Entry
	for i, name := range []string{"Ofertas VIP #1", "Ofertas VIP #2", "Avisos", "Clientes"} {
		e := uwc.NewGroupEntry(groups.ID, ws, "12036300000000000"+string(rune('1'+i))+"@g.us", name, now.Add(time.Duration(i)*time.Second))
		e.ID = uuid.NewString()
		made = append(made, e)
	}
	if _, err := entries.CreateMany(made); err != nil {
		t.Fatalf("entries: %v", err)
	}

	saved, err := repo.FindByID(groups.ID)
	if err != nil || saved.InstanceID != "" || !reflect.DeepEqual(saved.SenderInstanceIDs, []string{n1, n2}) {
		t.Fatalf("saved = %+v, %v", saved, err)
	}

	if err := entries.RecordSend(made[0].ID, uwc.RecordSendInput{SenderInstanceID: n2, SentAt: now}); err != nil {
		t.Fatalf("record send: %v", err)
	}
	sent, err := entries.FindByID(made[0].ID)
	if err != nil || sent.SenderInstanceID != n2 || sent.Status != campaign.SendStatusSent {
		t.Fatalf("sent = %+v, %v", sent, err)
	}

	names, err := entries.TargetNamesForCampaigns([]string{groups.ID, people.ID}, 3)
	if err != nil || !reflect.DeepEqual(names[groups.ID], []string{"Avisos", "Clientes", "Ofertas VIP #1"}) || len(names[people.ID]) != 0 {
		t.Fatalf("names = %+v, %v", names, err)
	}

	list := func(in uwc.ListCampaignsInput) []string {
		t.Helper()
		in.WorkspaceID = ws
		in.Options = shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 10}}
		page, err := repo.List(in)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := []string{}
		for _, c := range page.Items {
			out = append(out, c.Name)
		}
		return out
	}
	if got := list(uwc.ListCampaignsInput{Audience: uwc.AudienceGroups}); !reflect.DeepEqual(got, []string{"Ofertas"}) {
		t.Fatalf("groups only = %v", got)
	}
	if got := list(uwc.ListCampaignsInput{InstanceIDs: []string{n2}}); !reflect.DeepEqual(got, []string{"Ofertas"}) {
		t.Fatalf("by a pool number = %v", got)
	}
	if got := list(uwc.ListCampaignsInput{InstanceIDs: []string{n3}}); !reflect.DeepEqual(got, []string{"Leads"}) {
		t.Fatalf("by the person number = %v", got)
	}

	counts, err := summary.CountByStatusForWorkspace(uwc.WorkspaceSummaryFilter{WorkspaceID: ws, InstanceIDs: []string{n2}})
	if err != nil || counts.Total != 1 {
		t.Fatalf("summary by sender = %+v, %v; want only the entry n2 sent", counts, err)
	}

	if _, err := entries.ResetAllStatuses(groups.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset, _ := entries.FindByID(made[0].ID); reset.SenderInstanceID != "" || reset.Status != campaign.SendStatusPending {
		t.Fatalf("reset entry = %+v, want pending with no sender", reset)
	}
}
