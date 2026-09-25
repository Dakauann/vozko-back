package attendance_repository

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/attendance"
	"vozko/domain/conversation"
)

func attendanceDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
}

func attendanceIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run the attendance queries against Postgres")
	}
	db, err := gorm.Open(postgres.Open(attendanceDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

const emptyWorkspace = "00000000-0000-0000-0000-000000000000"

func workspaceWithBacklog(t *testing.T, db *gorm.DB) string {
	t.Helper()

	branches := make([]string, 0, len(channelSources))
	for _, src := range channelSources {
		branches = append(branches, `
			SELECT `+src.WorkspaceColumn+`::text AS workspace_id, COUNT(*) AS backlog
			`+trendJoins(src)+`
			WHERE `+src.EntryAlias+`.deleted_at IS NULL
			  AND `+src.StatusColumn+` <> 'finished'
			  AND `+trendEngagedPredicate(src)+`
			GROUP BY 1
		`)
	}

	var workspaceID string
	err := db.Raw(`
		SELECT workspace_id
		FROM (` + strings.Join(branches, " UNION ALL ") + `) scoped
		GROUP BY workspace_id
		ORDER BY SUM(backlog) DESC
		LIMIT 1
	`).Scan(&workspaceID).Error
	if err != nil {
		t.Fatalf("looking for a workspace with backlog: %v", err)
	}
	if workspaceID == "" {
		t.Skip("no workspace in this database has an engaged unfinished conversation, so the backlog branches cannot be exercised")
	}
	return workspaceID
}

func integrationFilters() map[string]attendance.OverviewFilter {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	return map[string]attendance.OverviewFilter{
		"no filters":     {},
		"date range":     {DateFrom: &from, DateTo: &to},
		"one channel":    {DateFrom: &from, DateTo: &to, Channel: "whatsapp"},
		"department":     {DateFrom: &from, DateTo: &to, DepartmentID: emptyWorkspace},
		"member":         {DateFrom: &from, DateTo: &to, MemberID: emptyWorkspace},
		"campaign":       {DateFrom: &from, DateTo: &to, CampaignID: emptyWorkspace},
		"campaign typed": {DateFrom: &from, DateTo: &to, CampaignID: emptyWorkspace, CampaignType: "whatsapp"},
		"everything": {
			DateFrom:     &from,
			DateTo:       &to,
			Channel:      "unofficial_whatsapp",
			DepartmentID: emptyWorkspace,
			MemberID:     emptyWorkspace,
			CampaignID:   emptyWorkspace,
		},
	}
}

func readEverySection(t *testing.T, repo attendance.Repository, workspaceID string, filter attendance.OverviewFilter) {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.ReadSummary(ctx, workspaceID, filter); err != nil {
		t.Fatalf("ReadSummary: %v", err)
	}
	if _, err := repo.ReadTeam(ctx, workspaceID, filter); err != nil {
		t.Fatalf("ReadTeam: %v", err)
	}
	if _, err := repo.ReadStages(ctx, workspaceID, filter); err != nil {
		t.Fatalf("ReadStages: %v", err)
	}
	if _, err := repo.ReadBacklog(ctx, workspaceID, filter, time.Now().UTC()); err != nil {
		t.Fatalf("ReadBacklog: %v", err)
	}
	if _, err := repo.ReadRework(ctx, workspaceID, filter); err != nil {
		t.Fatalf("ReadRework: %v", err)
	}
}

func TestSectionReadsRunAgainstPostgres(t *testing.T) {
	db := attendanceIntegrationDB(t)
	repo := New(db)

	for name, filter := range integrationFilters() {
		t.Run("empty workspace/"+name, func(t *testing.T) {
			readEverySection(t, repo, emptyWorkspace, filter)
		})
	}
}

func TestSectionReadsAgreeOnAPopulatedWorkspace(t *testing.T) {
	db := attendanceIntegrationDB(t)
	workspaceID := workspaceWithBacklog(t, db)
	repo := New(db)
	ctx := context.Background()

	wide := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	filter := attendance.OverviewFilter{DateFrom: &wide, DateTo: &now, IncludeAI: true}

	readEverySection(t, repo, workspaceID, filter)

	summary, err := repo.ReadSummary(ctx, workspaceID, filter)
	if err != nil {
		t.Fatalf("ReadSummary: %v", err)
	}
	stages, err := repo.ReadStages(ctx, workspaceID, filter)
	if err != nil {
		t.Fatalf("ReadStages: %v", err)
	}
	backlog, err := repo.ReadBacklog(ctx, workspaceID, filter, now)
	if err != nil {
		t.Fatalf("ReadBacklog: %v", err)
	}
	team, err := repo.ReadTeam(ctx, workspaceID, filter)
	if err != nil {
		t.Fatalf("ReadTeam: %v", err)
	}

	if got := stages.StagedEngaged + stages.UnstagedEngaged; got != summary.KPIs.Engaged {
		t.Fatalf("stages see %d engaged, summary sees %d: two sections disagree about one scope",
			got, summary.KPIs.Engaged)
	}
	if want := summary.KPIs.Engaged - summary.KPIs.Finished; backlog.Total != want {
		t.Fatalf("backlog total = %d, want engaged - finished = %d", backlog.Total, want)
	}
	if backlog.Total == 0 {
		t.Fatalf("the chosen workspace reported an empty backlog, so the x-ray queries never ran")
	}
	if !backlog.Available || !backlog.Age.Available {
		t.Fatalf("backlog x-ray came back unavailable (%s / %s) although the backlog has %d conversations",
			backlog.Reason, backlog.Age.Reason, backlog.Total)
	}
	if len(backlog.Reachability) == 0 {
		t.Fatalf("backlog reachability returned no channel rows")
	}
	var memberFinished int64
	for _, row := range team.ByDepartment {
		memberFinished += row.Finished
	}
	if memberFinished != summary.KPIs.Finished {
		t.Fatalf("departments add up to %d finished, summary says %d", memberFinished, summary.KPIs.Finished)
	}
}

func TestSummaryWithOutcomeCaptureRunsAgainstPostgres(t *testing.T) {
	db := attendanceIntegrationDB(t)
	repo := New(db)

	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	for name, codes := range map[string][]string{
		"with durable codes": {"sale", "scheduled"},
		"no durable codes":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			filter := attendance.OverviewFilter{
				DateFrom: &from,
				DateTo:   &to,
				Quality: attendance.QualityPolicy{
					Enabled:      true,
					EnabledAt:    &enabled,
					Threshold:    30,
					DurableCodes: codes,
				},
			}
			out, err := repo.ReadSummary(context.Background(), emptyWorkspace, filter)
			if err != nil {
				t.Fatalf("ReadSummary with capture (%s): %v", name, err)
			}
			if out.Quality.Reason == attendance.ReasonCaptureDisabled {
				t.Fatalf("quality block reported capture disabled although the policy was enabled")
			}
		})
	}
}

func TestGetTrendRunsAgainstPostgres(t *testing.T) {
	db := attendanceIntegrationDB(t)
	repo := New(db)
	ctx := context.Background()

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	for name, filter := range integrationFilters() {
		t.Run(name, func(t *testing.T) {
			for _, buckets := range []int{1, 13, 24} {
				if _, err := repo.GetTrend(ctx, emptyWorkspace, filter, buckets, loc); err != nil {
					t.Fatalf("GetTrend(%s, %d buckets): %v", name, buckets, err)
				}
			}
		})
	}

	if _, err := repo.GetTrend(ctx, emptyWorkspace, attendance.OverviewFilter{}, 13, nil); err != nil {
		t.Fatalf("GetTrend with no timezone: %v", err)
	}
}

func TestGetRevenueRunsAgainstPostgres(t *testing.T) {
	db := attendanceIntegrationDB(t)
	repo := New(db)
	ctx := context.Background()

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)

	if _, _, err := repo.GetRevenue(ctx, emptyWorkspace, from, to, attendance.RevenueScope{}); err != nil {
		t.Fatalf("GetRevenue: %v", err)
	}
	if _, err := repo.GetRevenueByMonth(ctx, emptyWorkspace, from.AddDate(-1, 0, 0), to, loc, "", attendance.RevenueScope{}); err != nil {
		t.Fatalf("GetRevenueByMonth: %v", err)
	}
	if _, err := repo.GetRevenueByMonth(ctx, emptyWorkspace, from, to, nil, "", attendance.RevenueScope{}); err != nil {
		t.Fatalf("GetRevenueByMonth with no timezone: %v", err)
	}
	if _, err := repo.GetRevenueByMonth(
		ctx, emptyWorkspace, from.AddDate(-1, 0, 0), to, loc,
		"9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f8", attendance.RevenueScope{},
	); err != nil {
		t.Fatalf("GetRevenueByMonth for one owner: %v", err)
	}
	scoped := attendance.RevenueScope{
		CampaignID:   "9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f8",
		DepartmentID: "9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f9",
	}
	if _, _, err := repo.GetRevenue(ctx, emptyWorkspace, from, to, scoped); err != nil {
		t.Fatalf("GetRevenue scoped to a campaign and department: %v", err)
	}
	if _, err := repo.GetRevenueByMonth(ctx, emptyWorkspace, from, to, loc, "ai:9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f8", scoped); err != nil {
		t.Fatalf("GetRevenueByMonth scoped for an AI owner: %v", err)
	}
}

func TestOutcomeColumnsExistInPostgres(t *testing.T) {
	db := attendanceIntegrationDB(t)

	tables := map[string]string{
		"whatsapp_campaign_entries":         "close_outcome",
		"instagram_conversations":           "close_outcome",
		"telegram_conversations":            "close_outcome",
		"unofficial_whatsapp_conversations": "close_outcome",
		"workspace_configs":                 "outcome_capture",
		"attendance_targets":                "metric_key",
	}

	for table, column := range tables {
		var exists bool
		err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = ? AND column_name = ?
			)
		`, table, column).Scan(&exists).Error
		if err != nil {
			t.Fatalf("information_schema lookup for %s.%s: %v", table, column, err)
		}
		if !exists {
			t.Fatalf("%s.%s is missing; run the migration before serving the attendance page", table, column)
		}
	}

	if conversation.ReservedOutcomePrefix != "_" {
		t.Fatalf("the reserved outcome prefix changed; the quality SQL filters on a literal underscore")
	}
}

func activeBeforeWindowOracle(src channelSource, from, to time.Time) string {
	return `
		SELECT ` + src.EntryAlias + `.id
		FROM ` + src.ContainerTable + `
		JOIN ` + src.EntryTable + ` ON ` + src.ContainerJoin + ` AND ` + src.EntryAlias + `.deleted_at IS NULL
		WHERE ` + src.WorkspaceColumn + ` = @ws AND ` + src.ContainerAlias + `.deleted_at IS NULL
		  AND (` + src.EntryAlias + `.created_at < @from OR ` + src.EntryAlias + `.created_at > @to)
		  AND EXISTS (
			SELECT 1 FROM conversation_messages cm
			WHERE cm.entry_id = ` + src.EntryAlias + `.id AND cm.entry_type = '` + string(src.EntryType) + `'
			  AND cm.deleted_at IS NULL AND cm.created_at >= @from AND cm.created_at <= @to
		  )`
}

func TestScopeAdmitsEveryConversationActiveInTheWindow(t *testing.T) {
	db := attendanceIntegrationDB(t)
	workspaceID := workspaceWithBacklog(t, db)
	now := time.Now().UTC()

	for name, days := range map[string]int{"7 days": 7, "30 days": 30, "365 days": 365} {
		t.Run(name, func(t *testing.T) {
			from := now.AddDate(0, 0, -days)
			filter := attendance.OverviewFilter{DateFrom: &from, DateTo: &now}

			body, args := overviewEntrySelect(workspaceID, filter)
			var scoped []string
			err := db.Raw(`SELECT entry_type || ':' || entry_id::text FROM (`+body+`) s WHERE NOT is_new_contact`, args...).
				Scan(&scoped).Error
			if err != nil {
				t.Fatalf("scope: %v", err)
			}

			var oracle []string
			for _, src := range channelSources {
				var ids []string
				err := db.Raw(activeBeforeWindowOracle(src, from, now), map[string]interface{}{
					"ws": workspaceID, "from": from, "to": now,
				}).Scan(&ids).Error
				if err != nil {
					t.Fatalf("oracle %s: %v", src.EntryType, err)
				}
				for _, id := range ids {
					oracle = append(oracle, string(src.EntryType)+":"+id)
				}
			}

			slices.Sort(scoped)
			slices.Sort(oracle)
			if !slices.Equal(scoped, oracle) {
				t.Fatalf("scope admitted %d conversations active in the window, the plain definition finds %d", len(scoped), len(oracle))
			}
		})
	}
}
