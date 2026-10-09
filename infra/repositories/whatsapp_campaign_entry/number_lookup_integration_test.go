package whatsapp_campaign_entry

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

type lookupSeed struct {
	db    *gorm.DB
	phone string
}

func newLookupSeed(t *testing.T) lookupSeed {
	t.Helper()
	db := repotest.IsolatedDB(t, "wce_lookup")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return lookupSeed{db: db, phone: uuid.NewString()}
}

func (s lookupSeed) entry(t *testing.T, workspaceID, number string, createdAt time.Time, leadDeleted bool) string {
	t.Helper()
	leadID, campaignID, entryID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	var deletedAt *time.Time
	if leadDeleted {
		deletedAt = &createdAt
	}
	statements := []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO leads (id, workspace_id, number, name, version, created_at, updated_at, deleted_at) VALUES (?, ?, ?, '', 1, now(), now(), ?)`, []interface{}{leadID, workspaceID, number, deletedAt}},
		{`INSERT INTO whatsapp_campaigns (id, workspace_id, name, business_phone_id, created_at, updated_at) VALUES (?, ?, 'Campanha', ?, now(), now())`, []interface{}{campaignID, workspaceID, s.phone}},
		{`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at) VALUES (?, ?, ?, 'SENT', ?, ?)`, []interface{}{entryID, campaignID, leadID, createdAt, createdAt}},
	}

	for _, st := range statements {
		if err := s.db.Exec(st.sql, st.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return entryID
}

func TestTheOutreachLookupNeverCrossesWorkspacesOnAGrantedPhoneAgainstPostgres(t *testing.T) {
	seed := newLookupSeed(t)
	repo := &repository{db: seed.db}
	workspaceA, workspaceB := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()

	liveA := seed.entry(t, workspaceA, "5511987654321", now.Add(-3*time.Hour), false)
	seed.entry(t, workspaceA, "5511987654321", now.Add(-time.Hour), true)
	newestB := seed.entry(t, workspaceB, "5511987654321", now, false)

	got, err := repo.FindByNumberBusinessPhoneAndWorkspace("5511987654321", seed.phone, workspaceA)
	if err != nil || got.ID != liveA {
		t.Fatalf("workspace A must get its own live entry %s, got %+v, %v", liveA, got, err)
	}
	gotB, err := repo.FindByNumberBusinessPhoneAndWorkspace("551187654321", seed.phone, workspaceB)
	if err != nil || gotB.ID != newestB {
		t.Fatalf("workspace B must get its own entry through the other number format, got %+v, %v", gotB, err)
	}
	if _, err := repo.FindByNumberBusinessPhoneAndWorkspace("5511987654321", seed.phone, uuid.NewString()); err == nil {
		t.Fatal("a workspace without entries must find nothing")
	}

	inbound, err := repo.FindInboundRouteByNumberAndBusinessPhone("5511987654321", seed.phone)
	if err != nil || inbound.ID != newestB {
		t.Fatalf("inbound routing still looks across workspaces for the newest entry, got %+v, %v", inbound, err)
	}
}

func TestInboundRoutingSkipsEntriesOfDeletedLeadsAgainstPostgres(t *testing.T) {
	seed := newLookupSeed(t)
	repo := &repository{db: seed.db}
	workspaceA, workspaceB := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()

	liveB := seed.entry(t, workspaceB, "5511987654321", now.Add(-2*time.Hour), false)
	seed.entry(t, workspaceA, "5511987654321", now, true)

	inbound, err := repo.FindInboundRouteByNumberAndBusinessPhone("5511987654321", seed.phone)
	if err != nil || inbound.ID != liveB {
		t.Fatalf("inbound must reach the live entry %s of another workspace, not the deleted lead's newer one, got %+v, %v", liveB, inbound, err)
	}

	onlyDeleted := newLookupSeed(t)
	onlyDeleted.entry(t, workspaceA, "5511912345678", now, true)
	if _, err := (&repository{db: onlyDeleted.db}).FindInboundRouteByNumberAndBusinessPhone("5511912345678", onlyDeleted.phone); err == nil {
		t.Fatal("an entry whose lead was deleted must not receive inbound messages")
	}
}

func TestEntryPlacementsReadTheCampaignWorkspaceAndDepartmentAgainstPostgres(t *testing.T) {
	seed := newLookupSeed(t)
	workspaceA := uuid.NewString()
	live := seed.entry(t, workspaceA, "5511987654321", time.Now().UTC(), false)
	gone := seed.entry(t, workspaceA, "5511912345678", time.Now().UTC(), false)
	if err := seed.db.Exec(`UPDATE whatsapp_campaign_entries SET deleted_at = now() WHERE id = ?`, gone).Error; err != nil {
		t.Fatal(err)
	}

	placements, err := NewPlacementDirectory(seed.db).EntryPlacements([]string{live, gone, uuid.NewString()})
	if err != nil {
		t.Fatalf("EntryPlacements: %v", err)
	}
	if len(placements) != 1 || placements[live].WorkspaceID != workspaceA || placements[live].DepartmentID != "" {
		t.Fatalf("placements = %+v", placements)
	}
}
