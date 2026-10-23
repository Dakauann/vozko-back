package choicestats_repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/choicestats"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/testenv"
)

func postgresStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "choice_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
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
	if err := db.AutoMigrate(schema.ChoiceStatsModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{database.ChoicePromptSentAtIndex, database.ChoicePromptMessageIndex, database.ChoiceCountTemplateIndex, database.ChoiceCountDayIndex} {
		stmt, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return New(db), db
}

const testWorkspace = "8f6c2a52-1b0f-4f7e-9a34-0d5f8f1e2c3b"

func recordedSend(t *testing.T, key string, sentAt time.Time, shown ...string) choicestats.SendRecord {
	t.Helper()
	rec, err := choicestats.Send{
		WorkspaceID: testWorkspace, PromptKey: key, ProviderMessageID: "wamid." + key,
		Source: choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf-1", NodeID: "ask", Version: 2, TemplateID: "tpl-1"},
		Shown:  shown, SentAt: sentAt,
	}.Record(time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func clickOn(t *testing.T, key, option string, at time.Time) choicestats.ClickRecord {
	t.Helper()
	rec, err := choicestats.Click{WorkspaceID: testWorkspace, PromptKey: key, Option: option, At: at}.Record()
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func countsByOption(t *testing.T, store *Store, day time.Time) map[string][2]int64 {
	t.Helper()
	rows, err := store.Counts(context.Background(), choicestats.CountQuery{
		WorkspaceID: testWorkspace, Kind: choicestats.SourceWorkflow, SourceID: "wf-1", From: day, To: day,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]int64{}
	for _, row := range rows {
		out[row.OptionID] = [2]int64{row.Shown, row.Clicks}
	}
	return out
}

func TestConcurrentIdenticalSendsAndClicksCountOnceOnPostgres(t *testing.T) {
	store, _ := postgresStore(t)
	ctx := context.Background()
	sentAt := time.Now().UTC().Add(-time.Hour)
	send := recordedSend(t, "wf:run-1:ask#1", sentAt, "sim", "nao")

	var wg sync.WaitGroup
	var mu sync.Mutex
	newSends := 0
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			isNew, err := store.RecordSend(ctx, send)
			if err != nil {
				t.Error(err)
				return
			}
			if isNew {
				mu.Lock()
				newSends++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if newSends != 1 {
		t.Fatalf("%d of 16 identical sends counted, want 1", newSends)
	}

	clicks := 0
	for i := range 16 {
		option := "sim"
		if i%2 == 1 {
			option = "nao"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			counted, err := store.RecordClick(ctx, clickOn(t, send.PromptKey, option, time.Now().UTC()))
			if err != nil {
				t.Error(err)
				return
			}
			if counted {
				mu.Lock()
				clicks++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if clicks != 1 {
		t.Fatalf("%d of 16 taps on one prompt counted, want the first only", clicks)
	}
	got := countsByOption(t, store, send.SentDay)
	if got[""] != [2]int64{1, 1} || got["sim"][0] != 1 || got["nao"][0] != 1 || got["sim"][1]+got["nao"][1] != 1 {
		t.Fatalf("counters = %v", got)
	}
}

func TestShardsSumIntoOneCountAndOnlyShownOptionsInsideTheWindowAreClickedOnPostgres(t *testing.T) {
	store, _ := postgresStore(t)
	ctx := context.Background()
	sentAt := time.Now().UTC().Add(-time.Hour)
	shards := map[int]bool{}
	for i := range 24 {
		send := recordedSend(t, fmt.Sprintf("wf:run-%d:ask#1", i), sentAt, "sim", "nao")
		shards[send.Shard] = true
		if _, err := store.RecordSend(ctx, send); err != nil {
			t.Fatal(err)
		}
	}
	if len(shards) < 2 {
		t.Fatalf("24 prompts used %d shard(s)", len(shards))
	}
	clicked, _ := store.RecordClick(ctx, clickOn(t, "wf:run-0:ask#1", "sim", time.Now().UTC()))
	freeText, _ := store.RecordClick(ctx, clickOn(t, "wf:run-1:ask#1", choicestats.OptionFreeText, time.Now().UTC()))
	notShown, _ := store.RecordClick(ctx, clickOn(t, "wf:run-2:ask#1", "talvez", time.Now().UTC()))
	afterFreeText, _ := store.RecordClick(ctx, clickOn(t, "wf:run-1:ask#1", "sim", time.Now().UTC()))
	if !clicked || !freeText || notShown || afterFreeText {
		t.Fatalf("clicked=%v freeText=%v notShown=%v afterFreeText=%v", clicked, freeText, notShown, afterFreeText)
	}

	old := recordedSend(t, "wf:run-old:ask#1", time.Now().UTC().Add(-31*24*time.Hour), "sim")
	if _, err := store.RecordSend(ctx, old); err != nil {
		t.Fatal(err)
	}
	if late, _ := store.RecordClick(ctx, clickOn(t, old.PromptKey, "sim", time.Now().UTC())); late {
		t.Fatal("a tap past the 30 day attribution window was counted")
	}

	got := countsByOption(t, store, recordedSend(t, "x", sentAt, "a").SentDay)
	if got[""] != [2]int64{24, 1} || got["sim"] != [2]int64{24, 1} || got["nao"] != [2]int64{24, 0} || got[choicestats.OptionFreeText] != [2]int64{0, 1} {
		t.Fatalf("summed counters = %v", got)
	}

	byMessage, err := choicestats.Click{WorkspaceID: testWorkspace, ProviderMessageID: "wamid.wf:run-3:ask#1", Option: "nao", At: time.Now().UTC()}.Record()
	if err != nil {
		t.Fatal(err)
	}
	if counted, err := store.RecordClick(ctx, byMessage); err != nil || !counted {
		t.Fatalf("a click by the replied message: %v %v", counted, err)
	}
	rows, err := store.Counts(ctx, choicestats.CountQuery{WorkspaceID: testWorkspace, TemplateID: "tpl-1", From: old.SentDay, To: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, row := range rows {
		if row.OptionID == "" {
			total = row.Shown
		}
	}
	if total != 25 {
		t.Fatalf("template sends = %d, want 25", total)
	}
}

func TestTheReadsUseTheirIndexesOnPostgres(t *testing.T) {
	_, db := postgresStore(t)
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		sql   string
		args  []any
		index string
	}{
		{"a workflow read walks the counter primary key", sourceCountsSQL(true), []any{testWorkspace, "workflow", "wf-1", day, day, 2}, "choice_daily_counts_pkey"},
		{"a template read walks its index", templateCountsSQL, []any{testWorkspace, "tpl-1", day, day}, database.ChoiceCountTemplateIndex},
		{"a tap on a template finds its prompt by message", "SELECT prompt_key FROM choice_prompts WHERE workspace_id = ? AND provider_message_id = ?", []any{testWorkspace, "wamid.1"}, database.ChoicePromptMessageIndex},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var plan []string
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
					return err
				}
				return tx.Raw("EXPLAIN "+tc.sql, tc.args...).Scan(&plan).Error
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(plan, "\n"), tc.index) {
				t.Fatalf("plan does not use %s:\n%s", tc.index, strings.Join(plan, "\n"))
			}
		})
	}
}

func TestPruningDrainsInBatchesOnPostgres(t *testing.T) {
	store, _ := postgresStore(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -45)
	oldShards := map[int]bool{}
	for i := range 12 {
		send := recordedSend(t, fmt.Sprintf("wf:old-%d:ask#1", i), old, "sim")
		oldShards[send.Shard] = true
		if _, err := store.RecordSend(ctx, send); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordSend(ctx, recordedSend(t, "wf:fresh:ask#1", time.Now().UTC(), "sim")); err != nil {
		t.Fatal(err)
	}
	cutoff := choicestats.AttributionCutoff(time.Now().UTC())
	var batches []int64
	for {
		n, err := store.PrunePrompts(cutoff, 5)
		if err != nil {
			t.Fatal(err)
		}
		batches = append(batches, n)
		if n < 5 {
			break
		}
	}
	if fmt.Sprint(batches) != "[5 5 2]" {
		t.Fatalf("prompt batches = %v, want [5 5 2]", batches)
	}
	n, err := store.PruneCounts(time.Now().UTC().AddDate(0, 0, -10), 1)
	if err != nil || n != 1 {
		t.Fatalf("one counter batch = %d, %v", n, err)
	}
	n, err = store.PruneCounts(time.Now().UTC().AddDate(0, 0, -10), 100)
	if want := int64(2*len(oldShards) - 1); err != nil || n != want {
		t.Fatalf("the rest of the old counters = %d, %v, want %d (total and sim per shard of the old day)", n, err, want)
	}
	if got := countsByOption(t, store, recordedSend(t, "y", time.Now().UTC(), "a").SentDay); got[""] != [2]int64{1, 0} {
		t.Fatalf("pruning touched fresh counters: %v", got)
	}
}
