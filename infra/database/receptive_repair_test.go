package database

import (
	"slices"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestReceptiveRepairPinsSilentConversationsBeforeAligningTheContainers(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE whatsapp_campaign_entries AS e\s+SET automation_enabled = false`).WillReturnResult(sqlmock.NewResult(0, 93))
	mock.ExpectExec(`UPDATE whatsapp_campaigns AS c\s+SET agent_id = newest\.agent_id`).WillReturnResult(sqlmock.NewResult(0, 13))
	mock.ExpectExec(`UPDATE whatsapp_campaigns AS c\s+SET enable_agent_responses = false`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE whatsapp_campaigns\s+SET name = 'Receptivo '`).WillReturnResult(sqlmock.NewResult(0, 2))

	if err := moveReceptiveToTheNumber(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReceptiveRepairOnlyEverSilencesConversations(t *testing.T) {
	if !strings.Contains(receptivePinSilentConversationsSQL, "e.automation_enabled IS NULL") ||
		strings.Contains(receptivePinSilentConversationsSQL, "automation_enabled = true") {
		t.Fatal("the pin must only switch off conversations nobody switched by hand")
	}
}

func TestTheNewestReceptiveMatchesTheRuntimeLookup(t *testing.T) {
	if !strings.Contains(receptiveNewestSQL, "ORDER BY c.created_at DESC, c.id DESC") || !strings.Contains(receptiveNewestSQL, "NOT c.archived") {
		t.Fatal("the repair must pick the same container FindLatestOrganicByBusinessPhone returns")
	}
}

func TestReceptiveRepairIsRegistered(t *testing.T) {
	if !slices.Contains(repairNames(), "wc_move_receptive_to_the_number") {
		t.Fatal("the receptive repair is not registered in runDataRepairs")
	}
}

func seedReceptiveTables(t *testing.T, tx *gorm.DB) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE whatsapp_business_phone_numbers (id uuid PRIMARY KEY, owner_workspace_id uuid)`,
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, workspace_id uuid, business_phone_id uuid, name text, type text, archived boolean DEFAULT false,
			agent_id uuid, workflow_id uuid, pipeline_id uuid, enable_agent_responses boolean DEFAULT false, enable_workflow boolean DEFAULT false,
			enable_analysis boolean DEFAULT false, enable_auto_staging boolean DEFAULT false, enable_auto_memory boolean DEFAULT false,
			created_at timestamptz, updated_at timestamptz)`,
		`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid, automation_enabled boolean)`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture table: %v", err)
		}
	}
}

func seedReceptive(t *testing.T, tx *gorm.DB, workspaceID, phoneID, name string, ageDays int, agentID string) string {
	t.Helper()
	id := uuid.New().String()
	var agent any
	if agentID != "" {
		agent = agentID
	}
	if err := tx.Exec(`INSERT INTO whatsapp_campaigns (id, workspace_id, business_phone_id, name, type, agent_id, enable_agent_responses, enable_analysis, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'organic', ?, ?, ?, NOW() - make_interval(days => ?), NOW())`,
		id, workspaceID, phoneID, name, agent, agentID != "", agentID != "", ageDays).Error; err != nil {
		t.Fatalf("seed receptive: %v", err)
	}
	return id
}

func seedEntry(t *testing.T, tx *gorm.DB, campaignID string, automation any) string {
	t.Helper()
	id := uuid.New().String()
	if err := tx.Exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, automation_enabled) VALUES (?, ?, ?)`, id, campaignID, automation).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	return id
}

func TestReceptiveRepairAgainstPostgres(t *testing.T) {
	tx := repairTx(t)
	seedReceptiveTables(t, tx)

	owner, granted, phone, agent := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	if err := tx.Exec(`INSERT INTO whatsapp_business_phone_numbers (id, owner_workspace_id) VALUES (?, ?)`, phone, owner).Error; err != nil {
		t.Fatal(err)
	}
	newest := seedReceptive(t, tx, owner, phone, "Receptivo novo", 1, agent)
	older := seedReceptive(t, tx, owner, phone, "Organic – +55 11 4000 1234 (coexistence)", 30, "")
	grantedContainer := seedReceptive(t, tx, granted, phone, "Receptivo concedido", 2, agent)
	humanHandled := seedEntry(t, tx, older, nil)
	switchedOn := seedEntry(t, tx, older, true)
	onNewest := seedEntry(t, tx, newest, nil)

	for run := 0; run < 2; run++ {
		if err := moveReceptiveToTheNumber(tx); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}

	automation := func(entryID string) *bool {
		var v *bool
		if err := tx.Raw(`SELECT automation_enabled FROM whatsapp_campaign_entries WHERE id = ?`, entryID).Scan(&v).Error; err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := automation(humanHandled); v == nil || *v {
		t.Fatalf("a conversation humans handled must stay without automation, got %v", v)
	}
	if v := automation(switchedOn); v == nil || !*v {
		t.Fatal("a conversation switched on by hand must keep its choice")
	}
	if automation(onNewest) != nil {
		t.Fatal("conversations of the newest container are untouched")
	}

	type row struct {
		Name                 string
		AgentID              *string
		EnableAgentResponses bool
		EnableAnalysis       bool
	}
	read := func(id string) row {
		var r row
		if err := tx.Raw(`SELECT name, agent_id::text AS agent_id, enable_agent_responses, enable_analysis FROM whatsapp_campaigns WHERE id = ?`, id).Scan(&r).Error; err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := read(older); r.AgentID == nil || *r.AgentID != agent || !r.EnableAgentResponses || !r.EnableAnalysis {
		t.Fatalf("older container not aligned with the number: %+v", r)
	}
	if r := read(older); r.Name != "Receptivo +55 11 4000 1234" {
		t.Fatalf("name %q", r.Name)
	}
	if r := read(grantedContainer); r.EnableAgentResponses || r.EnableAnalysis {
		t.Fatalf("a granted workspace's receptive must run nothing: %+v", r)
	}
}
