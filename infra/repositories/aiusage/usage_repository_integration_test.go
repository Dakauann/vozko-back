package aiusage_repository

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/aiusage"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestUsageUnderAReferenceSumsTokensAndCountsCallsAndBilledRows(t *testing.T) {
	db := repotest.IsolatedDB(t, "ai_usage", &schema.AIUsageRecord{})
	repo := NewUsageRepository(db)
	ws := uuid.New().String()
	other := uuid.New().String()
	records := []aiusage.Record{
		{ReferenceID: "aichat:th-1:a", WorkspaceID: ws, Model: "m", Billed: true, Tokens: aiusage.Tokens{Input: 1000, Output: 100, CacheRead: 800, CacheWrite: 50, Reasoning: 20}},
		{ReferenceID: "aichat:th-1:b", WorkspaceID: ws, Model: "m", Tokens: aiusage.Tokens{Input: 2000, Output: 200, CacheRead: 1500}},
		{ReferenceID: "aichat:th-1:job", WorkspaceID: ws, Model: "imagen", Billed: true},
		{ReferenceID: "aichat:th-10:c", WorkspaceID: ws, Model: "m", Billed: true, Tokens: aiusage.Tokens{Input: 9999}},
		{ReferenceID: "aichat:th-1:e", WorkspaceID: other, Model: "m", Billed: true, Tokens: aiusage.Tokens{Input: 9999}},
		{ReferenceID: "aichat:t_1:f", WorkspaceID: ws, Model: "m", Billed: true, Tokens: aiusage.Tokens{Input: 9999}},
	}
	for _, record := range records {
		if err := repo.Record(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Record(records[0]); err != nil {
		t.Fatalf("recording the same reference again must be a no-op, got %v", err)
	}

	totals, err := repo.TotalsUnder(ws, "aichat:th-1:")
	if err != nil {
		t.Fatal(err)
	}
	want := aiusage.Totals{Calls: 2, Billed: 2, Tokens: aiusage.Tokens{Input: 3000, Output: 300, CacheRead: 2300, CacheWrite: 50, Reasoning: 20}}
	if totals != want {
		t.Fatalf("totals = %+v, want %+v", totals, want)
	}
}

func TestUsageUnderAnUnknownReferenceIsEmpty(t *testing.T) {
	db := repotest.IsolatedDB(t, "ai_usage", &schema.AIUsageRecord{})
	totals, err := NewUsageRepository(db).TotalsUnder(uuid.New().String(), "aichat:none:")
	if err != nil {
		t.Fatal(err)
	}
	if totals != (aiusage.Totals{}) {
		t.Fatalf("totals = %+v, want zero", totals)
	}
}
