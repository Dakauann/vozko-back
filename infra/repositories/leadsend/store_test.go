package leadsend_repository

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/campaign"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func pinned(sql string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(sql), `\?`, `\$\d+`)
}

const campaignOne = "8f9b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"

func uuids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuid.NewString()
	}
	return out
}

func TestEverySelectionSendStatementBindsAFixedHandfulAndNoJSONBQuestionMark(t *testing.T) {
	statements := map[string]string{
		"running":            inRunningCampaignsSQL,
		"official tally":     tallySQL(campaign.ChannelOfficial),
		"unofficial tally":   tallySQL(campaign.ChannelUnofficial),
		"official skips":     skipsSQL(campaign.ChannelOfficial),
		"unofficial skips":   skipsSQL(campaign.ChannelUnofficial),
		"official beyond":    skipBeyondSQL(campaign.ChannelOfficial),
		"unofficial beyond":  skipBeyondSQL(campaign.ChannelUnofficial),
		"official running":   skipInRunningSQL(campaign.ChannelOfficial),
		"unofficial running": skipInRunningSQL(campaign.ChannelUnofficial),
		"official delete":    deleteStoppedSQL(campaign.ChannelOfficial),
		"unofficial delete":  deleteStoppedSQL(campaign.ChannelUnofficial),
	}
	for name, sql := range statements {
		if strings.Contains(sql, "?|") || strings.Contains(sql, "?&") || strings.Contains(sql, "@?") {
			t.Fatalf("%s uses a jsonb question mark operator", name)
		}
		if n := strings.Count(sql, "?"); n == 0 || n > 16 {
			t.Fatalf("%s binds %d placeholders, want a fixed handful", name, n)
		}
	}
}

func TestInRunningCampaignsReadsBothPipelinesWithOneArrayPerChunk(t *testing.T) {
	db, mock := newMockDB(t)
	ids := uuids(70000)
	for chunk := 0; chunk < 14; chunk++ {
		rows := sqlmock.NewRows([]string{"lead_id"})
		if chunk == 0 {
			rows.AddRow(ids[0])
		}
		mock.ExpectQuery(pinned(`SELECT e.lead_id FROM whatsapp_campaign_entries e WHERE e.campaign_id IN (SELECT c.id FROM whatsapp_campaigns c WHERE c.workspace_id = ?::uuid AND c.status = ? AND c.deleted_at IS NULL) AND e.status = ? AND e.deleted_at IS NULL AND e.lead_id = ANY(?::uuid[])`)+`.*UNION.*unofficial_whatsapp_campaign_entries`).
			WithArgs("ws-1", "RUNNING", "PENDING", sqlmock.AnyArg(), "ws-1", "RUNNING", "PENDING", sqlmock.AnyArg()).
			WillReturnRows(rows)
	}
	running, err := NewStore(db).InRunningCampaigns(context.Background(), "ws-1", ids)
	if err != nil {
		t.Fatalf("InRunningCampaigns: %v", err)
	}
	if !running[ids[0]] || len(running) != 1 {
		t.Fatalf("running = %d leads, want only the first", len(running))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

func TestInRunningCampaignsRefusesAMissingWorkspace(t *testing.T) {
	db, _ := newMockDB(t)
	if _, err := NewStore(db).InRunningCampaigns(context.Background(), " ", uuids(1)); err == nil {
		t.Fatal("a missing workspace was accepted")
	}
}

func TestInRunningCampaignsSurfacesAReadFailure(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM whatsapp_campaign_entries`).WillReturnError(errors.New("db down"))
	if _, err := NewStore(db).InRunningCampaigns(context.Background(), "ws-1", uuids(3)); err == nil {
		t.Fatal("a failed read was hidden")
	}
}

func TestTallyCountsEntriesEligibilityReasonsAndNotes(t *testing.T) {
	db, mock := newMockDB(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	mock.ExpectQuery(pinned(`FROM whatsapp_campaign_entries e JOIN whatsapp_campaigns c ON c.id = e.campaign_id AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL LEFT JOIN leads l ON l.id = e.lead_id WHERE e.campaign_id = ANY(?::uuid[]) AND e.deleted_at IS NULL GROUP BY e.campaign_id`)).
		WithArgs("PENDING", "PENDING", "PENDING", "bp-1", cutoff, "ws-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"campaign_id", "entries", "eligible", "no_consent", "window_open"}).AddRow(campaignOne, 1204, 1122, 310, 52))
	mock.ExpectQuery(pinned(`SELECT e.campaign_id, e.error_code, CASE WHEN e.error_code = ANY(?::int[]) THEN COALESCE(e.error_message, '') ELSE '' END AS detail, COUNT(*) AS n FROM whatsapp_campaign_entries e`+
		` JOIN whatsapp_campaigns c ON c.id = e.campaign_id AND c.workspace_id = ?::uuid AND c.deleted_at IS NULL`+
		` WHERE e.campaign_id = ANY(?::uuid[]) AND e.deleted_at IS NULL AND e.error_code = ANY(?::int[]) GROUP BY 1, 2, 3`)).
		WithArgs(`{920004,920006}`, "ws-1", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"campaign_id", "error_code", "detail", "n"}).
			AddRow(campaignOne, 920003, "", 41).AddRow(campaignOne, 920001, "", 6).AddRow(campaignOne, 920002, "", 9).
			AddRow(campaignOne, 920004, "cooldown:7", 12).AddRow(campaignOne, 920004, "cooldown:30", 3).AddRow(campaignOne, 920004, "cooldown", 4).
			AddRow(campaignOne, 920006, "missing_variable:2:lead.district", 4).AddRow(campaignOne, 920006, "missing_variable:1:lead.custom:escola;2:lead.district", 1).
			AddRow(campaignOne, 920006, "missing_variable", 2))

	parts, err := NewStore(db).Tally(context.Background(), campaign.ChannelOfficial, "ws-1", []string{campaignOne}, "bp-1", now)
	if err != nil {
		t.Fatalf("Tally: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("parts = %+v", parts)
	}
	p := parts[0]
	if p.CampaignID != campaignOne || p.Entries != 1204 || p.Eligible != 1122 {
		t.Fatalf("part = %+v", p)
	}
	if p.Skipped[campaign.SkipNoIdentity] != 41 || p.Skipped[campaign.SkipBlocked] != 6 || p.Skipped[campaign.SkipOptedOut] != 9 || p.Skipped[campaign.SkipCooldown] != 19 || p.Skipped[campaign.SkipMissingVariable] != 7 {
		t.Fatalf("skipped = %v", p.Skipped)
	}
	if p.Counted[campaign.CountedWindowOpen] != 52 || p.Counted[campaign.CountedNoConsentRecorded] != 310 {
		t.Fatalf("counted = %v", p.Counted)
	}
	wantMissing := map[campaign.MissingVariable]int{{Slot: 2, Source: campaign.BindDistrict}: 5, {Slot: 1, Source: campaign.CustomBinding("escola")}: 1}
	if !reflect.DeepEqual(p.Missing, wantMissing) {
		t.Fatalf("missing = %v, want %v (every slot of an entry counts, an entry without a slot counts only in skipped)", p.Missing, wantMissing)
	}
	if p.CooldownDays != 30 {
		t.Fatalf("cooldown days = %d, want the longest the entries recorded", p.CooldownDays)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

func TestTheUnofficialTallyHasNoWindow(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(pinned(`FROM unofficial_whatsapp_campaign_entries e JOIN unofficial_whatsapp_campaigns c`)).
		WithArgs("PENDING", "PENDING", "ws-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"campaign_id", "entries", "eligible", "no_consent", "window_open"}).AddRow(campaignOne, 10, 8, 3, 0))
	mock.ExpectQuery(`FROM unofficial_whatsapp_campaign_entries e`).
		WillReturnRows(sqlmock.NewRows([]string{"campaign_id", "error_code", "detail", "n"}))

	parts, err := NewStore(db).Tally(context.Background(), campaign.ChannelUnofficial, "ws-1", []string{campaignOne}, "", time.Now())
	if err != nil || len(parts) != 1 || parts[0].Counted[campaign.CountedWindowOpen] != 0 || parts[0].Counted[campaign.CountedNoConsentRecorded] != 3 {
		t.Fatalf("parts = %+v, %v", parts, err)
	}
}

func TestTallyRefusesAnUnknownChannel(t *testing.T) {
	db, _ := newMockDB(t)
	if _, err := NewStore(db).Tally(context.Background(), "sms", "ws-1", []string{campaignOne}, "", time.Now()); err == nil {
		t.Fatal("an unknown channel was accepted")
	}
}

func TestSkipBeyondMarksThePendingEntriesAfterTheFirstNInLeadOrder(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(pinned(`UPDATE whatsapp_campaign_entries SET status = ?, error_code = ?, error_message = ?, updated_at = ? WHERE id IN (SELECT pending.id FROM whatsapp_campaign_entries pending JOIN whatsapp_campaigns c ON c.id = pending.campaign_id AND c.workspace_id = ?::uuid AND c.status = ? AND c.deleted_at IS NULL WHERE pending.campaign_id = ?::uuid AND pending.status = ? AND pending.deleted_at IS NULL ORDER BY pending.lead_id OFFSET ?)`)).
		WithArgs("FAILED", 920008, "over_cap", sqlmock.AnyArg(), "ws-1", "STOPPED", campaignOne, "PENDING", 55).
		WillReturnResult(sqlmock.NewResult(0, 45))

	n, err := NewStore(db).SkipBeyond(context.Background(), campaign.ChannelOfficial, "ws-1", campaignOne, 55)
	if err != nil || n != 45 {
		t.Fatalf("SkipBeyond = %d, %v", n, err)
	}
}

func TestSkipInRunningMarksTheLeadsPendingInAnotherRunningCampaign(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(pinned(`UPDATE whatsapp_campaign_entries SET status = ?, error_code = ?, error_message = ?, updated_at = ? WHERE campaign_id = ?::uuid AND status = ? AND deleted_at IS NULL`+
		` AND EXISTS (SELECT 1 FROM whatsapp_campaigns part WHERE part.id = ?::uuid AND part.workspace_id = ?::uuid AND part.status = ? AND part.deleted_at IS NULL)`+
		` AND lead_id IN (SELECT e.lead_id FROM whatsapp_campaign_entries e WHERE e.campaign_id IN (SELECT c.id FROM whatsapp_campaigns c WHERE c.workspace_id = ?::uuid AND c.status = ? AND c.deleted_at IS NULL) AND e.status = ? AND e.deleted_at IS NULL`+
		` UNION ALL SELECT e.lead_id FROM unofficial_whatsapp_campaign_entries e WHERE e.campaign_id IN (SELECT c.id FROM unofficial_whatsapp_campaigns c WHERE c.workspace_id = ?::uuid AND c.status = ? AND c.deleted_at IS NULL) AND e.status = ? AND e.deleted_at IS NULL)`)).
		WithArgs("FAILED", 920007, "already_in_running_campaign", sqlmock.AnyArg(), campaignOne, "PENDING", campaignOne, "ws-1", "STOPPED", "ws-1", "RUNNING", "PENDING", "ws-1", "RUNNING", "PENDING").
		WillReturnResult(sqlmock.NewResult(0, 4))

	n, err := NewStore(db).SkipInRunning(context.Background(), campaign.ChannelOfficial, "ws-1", campaignOne)
	if err != nil || n != 4 {
		t.Fatalf("SkipInRunning = %d, %v", n, err)
	}
}

func TestDeleteStoppedRemovesOnlyAStoppedCampaignAndItsEntries(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(pinned(`UPDATE whatsapp_campaigns SET deleted_at = ? WHERE id = ?::uuid AND workspace_id = ?::uuid AND status = ? AND deleted_at IS NULL`)).
		WithArgs(sqlmock.AnyArg(), campaignOne, "ws-1", "STOPPED").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(pinned(`UPDATE whatsapp_campaign_entries SET deleted_at = ? WHERE campaign_id = ?::uuid AND deleted_at IS NULL`)).
		WithArgs(sqlmock.AnyArg(), campaignOne).
		WillReturnResult(sqlmock.NewResult(0, 1204))
	mock.ExpectCommit()

	deleted, err := NewStore(db).DeleteStopped(context.Background(), campaign.ChannelOfficial, "ws-1", campaignOne)
	if err != nil || !deleted {
		t.Fatalf("DeleteStopped = %v, %v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStoppedLeavesACampaignThatAlreadyStarted(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE unofficial_whatsapp_campaigns SET deleted_at`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	deleted, err := NewStore(db).DeleteStopped(context.Background(), campaign.ChannelUnofficial, "ws-1", campaignOne)
	if err != nil || deleted {
		t.Fatalf("DeleteStopped = %v, %v, want nothing deleted", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkipBeyondRefusesANegativeKeep(t *testing.T) {
	db, _ := newMockDB(t)
	if _, err := NewStore(db).SkipBeyond(context.Background(), campaign.ChannelOfficial, "ws-1", campaignOne, -1); err == nil {
		t.Fatal("a negative keep was accepted")
	}
}

func TestKeyedPartsListsTheLiveCampaignsOfOneSendKey(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(pinned(`SELECT id, idempotency_key FROM whatsapp_campaigns WHERE workspace_id = ?::uuid AND deleted_at IS NULL AND idempotency_key LIKE ? ORDER BY idempotency_key`)).
		WithArgs("ws-1", "base:%").
		WillReturnRows(sqlmock.NewRows([]string{"id", "idempotency_key"}).AddRow("c-1", "base:1/2").AddRow("c-2", "base:2/2"))

	parts, err := NewStore(db).KeyedParts(context.Background(), campaign.ChannelOfficial, "ws-1", "base")
	if err != nil || len(parts) != 2 || parts[1].ID != "c-2" || parts[1].Key != "base:2/2" {
		t.Fatalf("parts = %+v, %v", parts, err)
	}
}

func TestKeyedPartsRefusesAKeyThatCouldMatchOtherKeys(t *testing.T) {
	db, _ := newMockDB(t)
	for _, base := range []string{"", "a%b", "a_b"} {
		if _, err := NewStore(db).KeyedParts(context.Background(), campaign.ChannelOfficial, "ws-1", base); err == nil {
			t.Fatalf("base %q was accepted", base)
		}
	}
}
