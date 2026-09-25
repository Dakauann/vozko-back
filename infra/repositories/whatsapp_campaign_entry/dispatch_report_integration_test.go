package whatsapp_campaign_entry

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/database/schema"
)

func reportDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
}

func reportIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(reportDSN()), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "wce_report_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(reportDSN()+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	for _, ddl := range []string{
		`CREATE TABLE whatsapp_campaigns (
			id uuid PRIMARY KEY, workspace_id uuid NOT NULL, department_id uuid, name text NOT NULL,
			type varchar(20), deleted_at timestamptz)`,
		`CREATE TABLE whatsapp_campaign_entries (
			id uuid PRIMARY KEY, campaign_id uuid NOT NULL, status varchar(40) NOT NULL,
			error_code int NOT NULL DEFAULT 0, created_at timestamptz NOT NULL, deleted_at timestamptz,
			sent_at timestamptz, delivered_at timestamptz, read_at timestamptz, failed_at timestamptz)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	if err := db.AutoMigrate(&schema.ConversationMessage{}, &schema.Label{}, &schema.EntryLabel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type seededCampaign struct {
	id, workspace, name, kind string
	department                *string
}

type seededEntry struct {
	id, campaign, status          string
	errorCode                     int
	created                       time.Time
	sent, delivered, read, delete *time.Time
}

func at(tm time.Time) *time.Time { return &tm }

func TestDispatchReportReaderAgainstPostgres(t *testing.T) {
	db := reportIntegrationDB(t)
	reader := NewDispatchReportReader(db)

	workspace, otherWorkspace := uuid.NewString(), uuid.NewString()
	sales, support := uuid.NewString(), uuid.NewString()
	launch, organic, otherDept, foreign := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	day1 := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	beforePeriod := day1.AddDate(0, -2, 0)

	for _, c := range []seededCampaign{
		{id: launch, workspace: workspace, name: "Lançamento", kind: "standard", department: &sales},
		{id: organic, workspace: workspace, name: "Receptivo", kind: "organic", department: &sales},
		{id: otherDept, workspace: workspace, name: "Suporte", kind: "standard", department: &support},
		{id: foreign, workspace: otherWorkspace, name: "Alheia", kind: "standard", department: &sales},
	} {
		if err := db.Exec(`INSERT INTO whatsapp_campaigns (id, workspace_id, department_id, name, type) VALUES (?, ?, ?, ?, ?)`,
			c.id, c.workspace, c.department, c.name, c.kind).Error; err != nil {
			t.Fatalf("seed campaign: %v", err)
		}
	}

	readEntry, legacyDelivered, failedAfterSend, pending := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, e := range []seededEntry{
		{id: readEntry, campaign: launch, status: "READ", created: day1, sent: at(day1), delivered: at(day1), read: at(day2)},
		{id: legacyDelivered, campaign: launch, status: "DELIVERED", created: day1},
		{id: failedAfterSend, campaign: launch, status: "FAILED", errorCode: 131026, created: day1, sent: at(day1.Add(time.Hour))},
		{id: pending, campaign: launch, status: "PENDING", created: day2},
		{id: uuid.NewString(), campaign: launch, status: "READ", created: day1, sent: at(day1), delete: at(day2)},
		{id: uuid.NewString(), campaign: launch, status: "DELIVERED", created: beforePeriod},
		{id: uuid.NewString(), campaign: organic, status: "READ", created: day1, sent: at(day1)},
		{id: uuid.NewString(), campaign: otherDept, status: "READ", created: day1, sent: at(day1)},
		{id: uuid.NewString(), campaign: foreign, status: "READ", created: day1, sent: at(day1)},
	} {
		if err := db.Exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, status, error_code, created_at, sent_at, delivered_at, read_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.id, e.campaign, e.status, e.errorCode, e.created, e.sent, e.delivered, e.read, e.delete).Error; err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}

	for _, m := range []schema.ConversationMessage{
		{EntryID: readEntry, EntryType: "whatsapp", MessageType: "template", SenderKind: "campaign", CreatedAt: day1},
		{EntryID: readEntry, EntryType: "whatsapp", MessageType: "user_message", SenderKind: "contact", CreatedAt: day2},
		{EntryID: readEntry, EntryType: "whatsapp", MessageType: "user_message", SenderKind: "contact", CreatedAt: day2.Add(time.Hour)},
		{EntryID: pending, EntryType: "whatsapp", MessageType: "operator", SenderKind: "human", CreatedAt: day2},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}

	live := schema.Label{ID: uuid.NewString(), WorkspaceID: workspace, Name: "interessado", Color: "#00D09A"}
	gone := schema.Label{ID: uuid.NewString(), WorkspaceID: workspace, Name: "antigo"}
	for _, l := range []*schema.Label{&live, &gone} {
		if err := db.Create(l).Error; err != nil {
			t.Fatalf("seed label: %v", err)
		}
	}
	if err := db.Delete(&gone).Error; err != nil {
		t.Fatalf("delete label: %v", err)
	}
	for _, el := range []schema.EntryLabel{
		{LabelID: live.ID, EntryID: readEntry, EntryType: "whatsapp", WorkspaceID: workspace},
		{LabelID: live.ID, EntryID: legacyDelivered, EntryType: "whatsapp", WorkspaceID: workspace},
		{LabelID: gone.ID, EntryID: failedAfterSend, EntryType: "whatsapp", WorkspaceID: workspace},
	} {
		if err := db.Create(&el).Error; err != nil {
			t.Fatalf("seed entry label: %v", err)
		}
	}

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	window, err := wce.NewDayWindow("2026-09-01", "2026-09-02", loc)
	if err != nil {
		t.Fatal(err)
	}
	campaignScope, err := wce.CampaignScope(launch)
	if err != nil {
		t.Fatal(err)
	}
	salesScope, err := wce.WorkspaceScope(workspace, []string{sales}, "organic", window)
	if err != nil {
		t.Fatal(err)
	}
	wholeWorkspace, err := wce.WorkspaceScope(workspace, nil, "organic", window)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("campaign funnel counts the whole campaign", func(t *testing.T) {
		got, err := reader.Funnel(campaignScope)
		if err != nil {
			t.Fatalf("Funnel() error = %v", err)
		}
		if got.TrackedSince == nil || !got.TrackedSince.Equal(day1) {
			t.Fatalf("TrackedSince = %v, want %v", got.TrackedSince, day1)
		}
		got.TrackedSince = nil
		want := wce.Funnel{Base: 5, Sent: 4, Delivered: 3, Read: 1, Replied: 1, Failed: 1, Pending: 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Funnel() = %+v, want %+v", got, want)
		}
	})

	t.Run("workspace funnel keeps to the caller's departments, dispatch campaigns and the period", func(t *testing.T) {
		got, err := reader.Funnel(salesScope)
		if err != nil {
			t.Fatalf("Funnel() error = %v", err)
		}
		got.TrackedSince = nil
		want := wce.Funnel{Base: 4, Sent: 3, Delivered: 2, Read: 1, Replied: 1, Failed: 1, Pending: 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Funnel() = %+v, want %+v", got, want)
		}
	})

	t.Run("campaign breakdown ranks every campaign in scope", func(t *testing.T) {
		got, err := reader.Campaigns(wholeWorkspace, 50)
		if err != nil {
			t.Fatalf("Campaigns() error = %v", err)
		}
		want := []wce.CampaignFunnel{
			{CampaignID: launch, CampaignName: "Lançamento", Base: 4, Sent: 3, Delivered: 2, Read: 1, Replied: 1, Failed: 1},
			{CampaignID: otherDept, CampaignName: "Suporte", Base: 1, Sent: 1, Delivered: 1, Read: 1},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Campaigns() = %+v, want %+v", got, want)
		}
	})

	t.Run("daily in the viewer's timezone", func(t *testing.T) {
		got, err := reader.Daily(campaignScope, window)
		if err != nil {
			t.Fatalf("Daily() error = %v", err)
		}
		want := []wce.DayCount{
			{Day: "2026-09-01", Sent: 2, Delivered: 1},
			{Day: "2026-09-02", Read: 1, Replied: 1},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Daily() = %+v, want %+v", got, want)
		}
	})

	t.Run("failure reasons", func(t *testing.T) {
		got, err := reader.FailureReasons(salesScope)
		if err != nil {
			t.Fatalf("FailureReasons() error = %v", err)
		}
		if want := []wce.FailureReasonCount{{Code: 131026, Count: 1}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("FailureReasons() = %+v, want %+v", got, want)
		}
	})

	t.Run("tags", func(t *testing.T) {
		got, err := reader.Tags(campaignScope, 10)
		if err != nil {
			t.Fatalf("Tags() error = %v", err)
		}
		want := []wce.TagCount{{LabelID: live.ID, Name: "interessado", Color: "#00D09A", Count: 2}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Tags() = %+v, want %+v", got, want)
		}
	})
}
