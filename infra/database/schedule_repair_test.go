package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func scheduleFixtures(t *testing.T) *gorm.DB {
	t.Helper()
	tx := postgresTx(t)
	for _, ddl := range []string{
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, status varchar(20), scheduled_start timestamptz)`,
		`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid, status varchar(40))`,
		`CREATE TABLE unofficial_whatsapp_campaigns (id uuid PRIMARY KEY, status varchar(20), scheduled_start timestamptz)`,
		`CREATE TABLE unofficial_whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid, status varchar(40))`,
	} {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("fixture table: %v", err)
		}
	}
	return tx
}

func seedScheduled(t *testing.T, tx *gorm.DB, status string, scheduled time.Time, entryStatuses ...string) string {
	t.Helper()
	id := uuid.NewString()
	if err := tx.Exec(`INSERT INTO whatsapp_campaigns (id, status, scheduled_start) VALUES (?, ?, ?)`, id, status, scheduled).Error; err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	for _, s := range entryStatuses {
		if err := tx.Exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, status) VALUES (?, ?, ?)`, uuid.NewString(), id, s).Error; err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}
	return id
}

func TestScheduleRepairAgainstPostgres(t *testing.T) {
	tx := scheduleFixtures(t)
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	neverStarted := seedScheduled(t, tx, "STOPPED", past, "PENDING", "PENDING")
	stoppedAfterSending := seedScheduled(t, tx, "STOPPED", past, "PENDING", "SENT")
	paused := seedScheduled(t, tx, "PAUSED", past, "PENDING")
	completed := seedScheduled(t, tx, "COMPLETED", past, "READ")
	rescheduled := seedScheduled(t, tx, "STOPPED", future, "SENT")
	unscheduled := seedScheduled(t, tx, "STOPPED", time.Time{}, "SENT")

	if err := consumeUsedSchedules(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if err := consumeUsedSchedules(tx); err != nil {
		t.Fatalf("a second run must be harmless: %v", err)
	}

	cleared := func(id string) bool {
		var at time.Time
		tx.Raw(`SELECT scheduled_start FROM whatsapp_campaigns WHERE id = ?`, id).Scan(&at)
		return at.IsZero() || at.Year() == 1
	}
	for name, id := range map[string]string{"stopped after sending": stoppedAfterSending, "paused": paused, "completed": completed} {
		if !cleared(id) {
			t.Fatalf("%s: a used schedule was kept and would restart the campaign", name)
		}
	}
	for name, id := range map[string]string{"never started": neverStarted, "rescheduled": rescheduled} {
		if cleared(id) {
			t.Fatalf("%s: a schedule that still has to fire was cleared", name)
		}
	}
	if !cleared(unscheduled) {
		t.Fatal("an unscheduled campaign gained a schedule")
	}
}

func TestScheduleRepairAlsoCoversUnofficialCampaignsAgainstPostgres(t *testing.T) {
	tx := scheduleFixtures(t)
	used, waiting := uuid.NewString(), uuid.NewString()
	past := time.Now().Add(-time.Hour)
	if err := tx.Exec(`INSERT INTO unofficial_whatsapp_campaigns (id, status, scheduled_start) VALUES (?, 'STOPPED', ?), (?, 'STOPPED', ?)`, used, past, waiting, past).Error; err != nil {
		t.Fatalf("seed campaigns: %v", err)
	}
	if err := tx.Exec(`INSERT INTO unofficial_whatsapp_campaign_entries (id, campaign_id, status) VALUES (?, ?, 'SENT'), (?, ?, 'PENDING')`, uuid.NewString(), used, uuid.NewString(), waiting).Error; err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	if err := consumeUsedSchedules(tx); err != nil {
		t.Fatalf("repair: %v", err)
	}

	var cleared []string
	tx.Raw(`SELECT id::text FROM unofficial_whatsapp_campaigns WHERE scheduled_start <= '0001-01-01 00:00:00+00'`).Scan(&cleared)
	if len(cleared) != 1 || cleared[0] != used {
		t.Fatalf("cleared %v, want only the campaign that already sent", cleared)
	}
}
