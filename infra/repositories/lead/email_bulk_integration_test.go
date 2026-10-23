package lead

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/lead"
	"vozko/infra/repositories/repotest"
)

func TestEmailBulkMatchesByAddressAndCreatesTheRestAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_email_bulk")
	migrateLeadTables(t, db)
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	old, newer, foreign := uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	for _, row := range []struct {
		id, workspace, email, name string
		created                    time.Time
	}{
		{old, ws, "Ana@Exemplo.com.br", "Ana Antiga", now.Add(-time.Hour)},
		{newer, ws, "ana@exemplo.com.br", "Ana Nova", now},
		{foreign, other, "bia@exemplo.com.br", "Bia", now},
	} {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, name, email, version, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
			row.id, row.workspace, row.name, row.email, row.created, row.created).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(db).(*repository)

	ids, err := repo.IDsByEmails(ctx, ws, []string{"ana@exemplo.com.br", "bia@exemplo.com.br", " ANA@exemplo.com.br "})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids["ana@exemplo.com.br"] != old {
		t.Fatalf("IDsByEmails = %v, want only the oldest lead of this workspace", ids)
	}

	got, err := repo.FindOrCreateManyByEmail(ctx, ws, []lead.BulkEmailInput{
		{Source: lead.SourceImport, Email: "ana@exemplo.com.br", Name: "Ana"},
		{Source: lead.SourceImport, Email: "bia@exemplo.com.br", Name: "Bia Souza"},
		{Source: lead.SourceImport, Email: "Bia@Exemplo.com.br", Name: "Repetida"},
		{Source: lead.SourceImport, Email: "não é email", Name: "Nada"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["ana@exemplo.com.br"].ID != old {
		t.Fatalf("FindOrCreateManyByEmail = %+v", got)
	}
	created := got["bia@exemplo.com.br"]
	if created == nil || created.ID == foreign || created.WorkspaceID != ws || created.Source != lead.SourceImport || created.Name != "Bia Souza" || created.Email != "bia@exemplo.com.br" {
		t.Fatalf("created = %+v", created)
	}
	if _, err := repo.FindOrCreateManyByEmail(ctx, ws, []lead.BulkEmailInput{{Source: "invalid", Email: "x@exemplo.com.br"}}); err == nil {
		t.Fatal("an invalid source was accepted")
	}

	var wg sync.WaitGroup
	results := make([]map[string]*lead.Lead, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := repo.FindOrCreateManyByEmail(ctx, ws, []lead.BulkEmailInput{{Source: lead.SourceImport, Email: "caio@exemplo.com.br", Name: "Caio"}})
			if err != nil {
				t.Error(err)
				return
			}
			results[i] = out
		}(i)
	}
	wg.Wait()
	var count int64
	db.Raw(`SELECT count(*) FROM leads WHERE workspace_id = ? AND lower(email) = 'caio@exemplo.com.br' AND deleted_at IS NULL`, ws).Scan(&count)
	if count != 1 {
		t.Fatalf("concurrent imports created %d leads for one address", count)
	}
	for _, r := range results {
		if r["caio@exemplo.com.br"] == nil || r["caio@exemplo.com.br"].ID != results[0]["caio@exemplo.com.br"].ID {
			t.Fatalf("results disagree: %+v", results)
		}
	}
}
