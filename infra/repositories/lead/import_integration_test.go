package lead

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func importDB(t *testing.T) (*gorm.DB, *repository, *Imports) {
	t.Helper()
	db, repo := collectionsDB(t)
	if err := db.AutoMigrate(&schema.LeadImport{}, &schema.LeadImportIssue{}, &schema.LeadImportLink{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_lead_imports_workspace_active ON lead_imports (workspace_id) WHERE status IN ('analyzing', 'importing')").Error; err != nil {
		t.Fatal(err)
	}
	imports, err := NewImports(repo)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, imports
}

func importingJob(t *testing.T, imports *Imports, ws string) *leadimport.Job {
	t.Helper()
	now := time.Now().UTC()
	job, err := leadimport.NewJob(ws, uuid.NewString(), leadimport.File{MediaID: uuid.NewString(), Name: "escola.csv", SizeBytes: 10, Owned: true},
		leadimport.Preview{Headers: []string{"telefone"}}, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	job.ID = uuid.NewString()
	job.Settings = &leadimport.Settings{Columns: []leadimport.Column{{Index: 0, Field: leadimport.FieldNumber}}, Policy: lead.PolicyFillEmpty}
	job.Status, job.Stage, job.Result = leadimport.StatusImporting, leadimport.StageRows, &leadimport.Counts{}
	if err := imports.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	claimed, err := imports.Claim(context.Background(), job.ID, uuid.NewString(), now)
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

func fillEmpty(existing *lead.Lead, row lead.ImportRecord) (lead.ImportDecision, error) {
	return lead.ApplyImport(existing, row, lead.ImportRules{Policy: lead.PolicyFillEmpty})
}

func rowBatch(job *leadimport.Job, items []leadimport.RowItem, processed int, issues []lead.ImportIssue) (leadimport.RowBatch, *[]leadimport.RowOutcome) {
	var seen []leadimport.RowOutcome
	return leadimport.RowBatch{Job: job, ActorID: job.RequestedBy, Items: items, Decide: fillEmpty,
		Checkpoint: func(outcomes []leadimport.RowOutcome) leadimport.Checkpoint {
			seen = outcomes
			var result leadimport.Counts
			var links []leadimport.PendingLink
			all := append([]lead.ImportIssue{}, issues...)
			for _, o := range outcomes {
				result.Record(o.Decision, o.Matched)
				all = append(all, o.Decision.Issues...)
				if o.Record.Relative != nil && o.LeadID != "" {
					links = append(links, leadimport.PendingLink{Line: o.Record.Line, LeadID: o.LeadID, RelativeNumber: o.Record.Relative.Number, Kind: o.Record.Relative.Kind})
				}
			}
			return leadimport.Checkpoint{Processed: processed, Result: result, Issues: all, Links: links}
		}}, &seen
}

func record(ws string, line int, number, name string) lead.ImportRecord {
	return lead.ImportRecord{WorkspaceID: ws, At: time.Now().UTC(), Line: line, Number: number, Name: name}
}

func TestImportWritesCreatesEnrichmentsAndFamilyLinksAgainstPostgres(t *testing.T) {
	db, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Name: "Maria Aparecida", Number: "5511987654321"})
	job := importingJob(t, imports, ws)

	found, err := imports.ByIdentity(ctx, ws, []string{"551187654321", "5511988887777"})
	if err != nil || found["5511987654321"] == nil || found["551187654321"] == nil || found["5511987654321"].Phones == nil {
		t.Fatalf("ByIdentity = %+v, %v; want Maria by both formats with her collections", found, err)
	}
	existing := found["5511987654321"]

	located := &lead.Address{Label: lead.AddressHome, Primary: true, Postal: paulista, GeoStatus: lead.GeoLocated,
		Fix: &geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionExact, Source: geo.SourceImport, FixedAt: time.Now().UTC()}}
	enrich := record(ws, 2, "5511987654321", "Maria Souza")
	enrich.Email, enrich.Address = "maria@example.com", located
	enrich.Phones = []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}
	bruna := record(ws, 3, "5511988887777", "Bruna Souza")
	bruna.Consent = &lead.Consent{GrantedAt: time.Now().UTC().Truncate(time.Second), Source: lead.ConsentImport, Purpose: "Matrícula 2027"}
	bruna.CustomFields = map[string]any{"interesse": "Visita"}
	bruna.Relative = &lead.ImportRelative{Number: "5511987654321", Kind: lead.KindChild}
	bruna.Phones = []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}
	joao := record(ws, 4, "", "João Souza")
	joao.Phones = []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}
	joao.Relative = &lead.ImportRelative{Number: "5511955554444", Kind: lead.KindParent}

	batch, outcomes := rowBatch(job, []leadimport.RowItem{{Record: enrich, Existing: existing}, {Record: bruna}, {Record: joao}}, 4,
		[]lead.ImportIssue{{Line: 1, Reason: lead.ReasonInvalid, Rejected: true, Field: lead.ImportFieldNumber}})
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if len(*outcomes) != 3 || (*outcomes)[0].Decision.Verdict != lead.ImportEnriched || (*outcomes)[1].Decision.Verdict != lead.ImportCreated {
		t.Fatalf("outcomes = %+v", *outcomes)
	}

	stored := reload(t, repo, ws, maria.ID)
	if stored.Name != "Maria Aparecida" || stored.Email != "maria@example.com" || stored.Version != maria.Version+1 {
		t.Fatalf("maria = %+v, want the manual name kept, the e-mail filled and one version bump", stored)
	}
	if len(stored.Phones) != 1 || len(stored.Addresses) != 1 || !stored.Addresses[0].Primary || stored.Addresses[0].Fix == nil || stored.Addresses[0].GeoStatus != lead.GeoLocated {
		t.Fatalf("maria collections = %+v / %+v", stored.Phones, stored.Addresses)
	}
	createdBruna, err := repo.FindByNumber(ws, "5511988887777")
	if err != nil {
		t.Fatal(err)
	}
	if createdBruna.Source != lead.SourceImport || createdBruna.WhatsAppOptIn == nil || createdBruna.WhatsAppOptIn.Purpose != "Matrícula 2027" || createdBruna.CustomFields["interesse"] != "Visita" {
		t.Fatalf("bruna = %+v", createdBruna)
	}
	var events int64
	db.Model(&schema.LeadEvent{}).Where("workspace_id = ? AND kind IN ?", ws, []string{string(lead.EventImported), string(lead.EventImportEnriched)}).Count(&events)
	if events != 3 {
		t.Fatalf("events = %d, want one per written lead", events)
	}
	afterRows, err := imports.Get(ctx, ws, job.ID)
	if err != nil || afterRows.Processed != 4 || afterRows.Result.Created != 2 || afterRows.Result.Enriched != 1 {
		t.Fatalf("checkpoint = %+v, %v", afterRows, err)
	}

	pending, err := imports.PendingLinks(ctx, job.ID, 10)
	if err != nil || len(pending) != 2 || pending[0].Line != 3 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	brunaLink, joaoLink := pending[0], pending[1]
	rel, err := lead.NewRelation(maria.ID, brunaLink.LeadID, brunaLink.Kind, job.RequestedBy)
	if err != nil {
		t.Fatal(err)
	}
	missing := lead.ImportIssue{Line: joaoLink.Line, Reason: lead.ReasonRelativeNotFound, Field: lead.ImportFieldRelative}
	err = imports.WriteLinks(ctx, leadimport.LinkBatch{Job: afterRows, Links: []leadimport.LinkWrite{{Link: brunaLink, Relation: &rel}, {Link: joaoLink, Issue: &missing}},
		Checkpoint: func(created int, failed []lead.ImportIssue) leadimport.Counts {
			result := afterRows.Result.Clone()
			result.LinksCreated += created
			for _, f := range failed {
				result.Issue(f)
			}
			return result
		}})
	if err != nil {
		t.Fatalf("WriteLinks: %v", err)
	}
	mariaNow, brunaNow := reload(t, repo, ws, maria.ID), reload(t, repo, ws, brunaLink.LeadID)
	if mariaNow.RelativesCount != 1 || brunaNow.RelativesCount != 1 || mariaNow.Version != stored.Version+1 {
		t.Fatalf("counts = %d/%d, version %d", mariaNow.RelativesCount, brunaNow.RelativesCount, mariaNow.Version)
	}
	if left, _ := imports.PendingLinks(ctx, job.ID, 10); len(left) != 0 {
		t.Fatalf("links left = %+v", left)
	}
	final, _ := imports.Get(ctx, ws, job.ID)
	if final.Result.LinksCreated != 1 || final.Result.Issues[string(lead.ReasonRelativeNotFound)] != 1 {
		t.Fatalf("result = %+v", final.Result)
	}
	issues, err := imports.Issues(ctx, job.ID, 0, 100)
	if err != nil || len(issues) != 2 || issues[0].Line != 1 || !issues[0].Rejected || issues[1].Reason != lead.ReasonRelativeNotFound {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
	page, _ := imports.Issues(ctx, job.ID, issues[0].Seq, 100)
	if len(page) != 1 {
		t.Fatalf("the next page = %+v", page)
	}
}

func TestImportRedecidesALeadThatChangedAfterItWasReadAgainstPostgres(t *testing.T) {
	_, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Number: "5511987654321"})
	job := importingJob(t, imports, ws)
	found, _ := imports.ByIdentity(ctx, ws, []string{"5511987654321"})
	read := found["5511987654321"]

	edited := reload(t, repo, ws, maria.ID)
	edited.Name, edited.NameSource, edited.Email = "Maria Aparecida", lead.SourceManual, "cida@example.com"
	if err := repo.Save(ctx, edited, edited.Version, []recordevent.Event{}); err != nil {
		t.Fatal(err)
	}

	row := record(ws, 2, "5511987654321", "Maria Souza")
	row.Email, row.Nickname = "maria@example.com", "Cida"
	batch, outcomes := rowBatch(job, []leadimport.RowItem{{Record: row, Existing: read}}, 1, nil)
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatal(err)
	}
	stored := reload(t, repo, ws, maria.ID)
	if stored.Name != "Maria Aparecida" || stored.Email != "cida@example.com" || stored.Nickname != "Cida" {
		t.Fatalf("stored = %+v, want the newer manual edit kept and only the empty nickname filled", stored)
	}
	if got := (*outcomes)[0].Decision; got.Verdict != lead.ImportEnriched || len(got.Conflicts) != 2 {
		t.Fatalf("decision = %+v", got)
	}
}

func TestImportAdoptsALeadCreatedByARacingWriterAgainstPostgres(t *testing.T) {
	_, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	job := importingJob(t, imports, ws)
	racer := newLead(t, repo, ws, lead.Draft{Number: "5511987654321"})

	row := record(ws, 2, "5511987654321", "Maria Souza")
	batch, outcomes := rowBatch(job, []leadimport.RowItem{{Record: row}}, 1, nil)
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if got := (*outcomes)[0]; got.Decision.Verdict != lead.ImportEnriched || got.LeadID != racer.ID {
		t.Fatalf("outcome = %+v, want the racing lead named", got)
	}
	if stored := reload(t, repo, ws, racer.ID); stored.Name != "Maria Souza" || stored.NameSource != lead.SourceImport {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestImportCheckpointNeedsTheClaimAgainstPostgres(t *testing.T) {
	_, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	job := importingJob(t, imports, ws)
	job.Claim = uuid.NewString()
	batch, _ := rowBatch(job, []leadimport.RowItem{{Record: record(ws, 2, "5511987654321", "Ana")}}, 1, nil)
	if err := imports.WriteRows(ctx, batch); !errors.Is(err, leadimport.ErrClaimLost) {
		t.Fatalf("WriteRows with a lost claim = %v", err)
	}
	if _, err := repo.FindByNumber(ws, "5511987654321"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("a batch whose claim was lost left a lead behind: %v", err)
	}
}

func TestImportJobLifecycleAgainstPostgres(t *testing.T) {
	db, _, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	now := time.Now().UTC()
	job := importingJob(t, imports, ws)

	second, _ := leadimport.NewJob(ws, uuid.NewString(), leadimport.File{Name: "b.csv"}, leadimport.Preview{}, 1, now)
	second.ID = uuid.NewString()
	if err := imports.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	second.Settings = &leadimport.Settings{Policy: lead.PolicySkip}
	second.Status = leadimport.StatusAnalyzing
	if err := imports.Save(ctx, second, leadimport.Guard{From: []leadimport.Status{leadimport.StatusUploaded}}); !errors.Is(err, leadimport.ErrRunning) {
		t.Fatalf("a second active import = %v, want ErrRunning", err)
	}
	if err := imports.Save(ctx, second, leadimport.Guard{From: []leadimport.Status{leadimport.StatusAnalyzed}}); !errors.Is(err, leadimport.ErrNotReady) {
		t.Fatalf("a save from the wrong status = %v, want ErrNotReady", err)
	}
	if _, err := imports.Claim(ctx, job.ID, uuid.NewString(), now); !errors.Is(err, leadimport.ErrClaimLost) {
		t.Fatalf("claiming a live import = %v", err)
	}
	refs, err := imports.Claimable(ctx, now.Add(leadimport.StaleAfter+time.Minute), 10)
	if err != nil || len(refs) != 1 || refs[0] != (leadimport.Ref{ID: job.ID, WorkspaceID: ws}) {
		t.Fatalf("claimable = %v, %v", refs, err)
	}
	dryRunWorkspace := uuid.NewString()
	dryRun, _ := leadimport.NewJob(dryRunWorkspace, uuid.NewString(), leadimport.File{Name: "c.csv"}, leadimport.Preview{}, 1, now)
	dryRun.ID, dryRun.Status = uuid.NewString(), leadimport.StatusAnalyzing
	if err := imports.Create(ctx, dryRun); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE lead_imports SET attempts = ? WHERE id IN (?, ?)", leadimport.MaxAttempts, job.ID, dryRun.ID).Error; err != nil {
		t.Fatal(err)
	}
	stalled, err := imports.FailStalled(ctx, now.Add(leadimport.StaleAfter+time.Minute))
	if err != nil || len(stalled) != 2 {
		t.Fatalf("FailStalled = %+v, %v", stalled, err)
	}
	statuses := map[leadimport.Ref]leadimport.Status{}
	for _, s := range stalled {
		statuses[s.Ref] = s.Status
	}
	if statuses[leadimport.Ref{ID: job.ID, WorkspaceID: ws}] != leadimport.StatusImporting || statuses[leadimport.Ref{ID: dryRun.ID, WorkspaceID: dryRunWorkspace}] != leadimport.StatusAnalyzing {
		t.Fatalf("stalled = %+v, want each import with the status it stalled in", stalled)
	}
	if again, err := imports.FailStalled(ctx, now.Add(leadimport.StaleAfter+time.Minute)); err != nil || len(again) != 0 {
		t.Fatalf("a second sweep failed again: %+v, %v", again, err)
	}
	if err := imports.Delete(ctx, dryRun.ID); err != nil {
		t.Fatal(err)
	}
	failed, _ := imports.Get(ctx, ws, job.ID)
	if failed.Status != leadimport.StatusFailed || failed.FailureCode != leadimport.FailureStalled || failed.Claim != "" {
		t.Fatalf("failed = %+v", failed)
	}
	if err := db.Exec("INSERT INTO lead_import_issues (import_id, line, reason, rejected) VALUES (?, 2, 'invalid', true)", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	expired, err := imports.Expired(ctx, now.Add(leadimport.Retention+time.Hour), 10)
	if err != nil || len(expired) != 2 || (expired[0].Settings == nil && expired[1].Settings == nil) {
		t.Fatalf("expired = %+v, %v", expired, err)
	}
	if err := imports.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	var issues int64
	db.Model(&schema.LeadImportIssue{}).Where("import_id = ?", job.ID).Count(&issues)
	if _, err := imports.Get(ctx, ws, job.ID); !errors.Is(err, leadimport.ErrNotFound) || issues != 0 {
		t.Fatalf("after delete: %v, %d issues", err, issues)
	}
}

func TestImportLookupsAgainstPostgres(t *testing.T) {
	_, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	bruna := newLead(t, repo, ws, lead.Draft{Name: "Bruna Souza", Phones: []lead.ContactPhone{{Number: "1133334444", Label: lead.PhoneLandline}}})
	maria := newLead(t, repo, ws, lead.Draft{Name: "Maria", Number: "5511987654321"})
	newLead(t, repo, uuid.NewString(), lead.Draft{Name: "Outra", Number: "5511987654321"})

	holders, err := imports.HoldingPhones(ctx, ws, []string{"551133334444"})
	if err != nil || len(holders) != 1 || holders[0].ID != bruna.ID || len(holders[0].Phones) != 1 {
		t.Fatalf("holders = %+v, %v", holders, err)
	}
	ids, err := imports.IdentityIDs(ctx, ws, []string{"551187654321"})
	if err != nil || ids["5511987654321"] != maria.ID || ids["551187654321"] != maria.ID {
		t.Fatalf("ids = %+v, %v", ids, err)
	}
}

func TestImportWritesFullBatchesAndEnrichesThemAgainstPostgres(t *testing.T) {
	db, _, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	job := importingJob(t, imports, ws)
	numberOf := func(i int) string { return fmt.Sprintf("55119%08d", 10000000+i) }
	const rows = 3 * leadimport.BatchRows
	for start := 0; start < rows; start += leadimport.BatchRows {
		items := make([]leadimport.RowItem, 0, leadimport.BatchRows)
		for i := start; i < start+leadimport.BatchRows; i++ {
			r := record(ws, i+2, numberOf(i), fmt.Sprintf("Pessoa %d", i))
			r.Phones = []lead.ContactPhone{{Number: fmt.Sprintf("55113%07d", 1000000+i), Label: lead.PhoneLandline}}
			r.Address = &lead.Address{Label: lead.AddressHome, Primary: true, Postal: paulista, GeoStatus: lead.GeoPending}
			items = append(items, leadimport.RowItem{Record: r})
		}
		batch, _ := rowBatch(job, items, start+leadimport.BatchRows, nil)
		if err := imports.WriteRows(ctx, batch); err != nil {
			t.Fatalf("batch at %d: %v", start, err)
		}
	}
	var leads, phones, addresses int64
	db.Model(&schema.Lead{}).Where("workspace_id = ?", ws).Count(&leads)
	db.Model(&schema.LeadPhone{}).Where("workspace_id = ?", ws).Count(&phones)
	db.Model(&schema.LeadAddress{}).Where("workspace_id = ? AND is_primary AND geo_status = 'pending' AND geo_next_at IS NOT NULL", ws).Count(&addresses)
	if leads != rows || phones != rows || addresses != rows {
		t.Fatalf("leads = %d, phones = %d, pending primary addresses = %d, want %d each", leads, phones, addresses, rows)
	}

	numbers := make([]string, leadimport.BatchRows)
	for i := range numbers {
		numbers[i] = numberOf(i)
	}
	found, err := imports.ByIdentity(ctx, ws, numbers)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]leadimport.RowItem, 0, leadimport.BatchRows)
	for i, n := range numbers {
		r := record(ws, i+2, n, "")
		r.Email = fmt.Sprintf("p%d@example.com", i)
		items = append(items, leadimport.RowItem{Record: r, Existing: found[n]})
	}
	batch, _ := rowBatch(job, items, rows, nil)
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatal(err)
	}
	var enriched int64
	db.Model(&schema.Lead{}).Where("workspace_id = ? AND email IS NOT NULL AND version = 2", ws).Count(&enriched)
	if enriched != leadimport.BatchRows {
		t.Fatalf("enriched = %d, want %d with one version bump each", enriched, leadimport.BatchRows)
	}
}

func TestImportBindsUnderPreparedStatementsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDBWith(t, "lead_import_prep", repotest.Options{TimeZone: "America/Sao_Paulo", PrepareStmt: true})
	migrateLeadTables(t, db)
	if err := db.AutoMigrate(&schema.LeadImport{}, &schema.LeadImportIssue{}, &schema.LeadImportLink{}); err != nil {
		t.Fatal(err)
	}
	repo := &repository{db: db}
	imports, err := NewImports(repo)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Number: "5511987654321"})
	job := importingJob(t, imports, ws)
	found, err := imports.ByIdentity(ctx, ws, []string{"5511987654321"})
	if err != nil {
		t.Fatal(err)
	}
	birth := found["5511987654321"].CreatedAt
	date := shared.DateOf(birth.AddDate(-30, 0, 0))
	row := record(ws, 2, "5511987654321", "Maria Souza")
	row.BirthDate, row.Owner = &date, uuid.NewString()
	row.Consent = &lead.Consent{GrantedAt: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC), Source: lead.ConsentImport, Purpose: "Novidades"}
	row.CustomFields = map[string]any{"nota": 8.5}
	batch, _ := rowBatch(job, []leadimport.RowItem{{Record: row, Existing: found["5511987654321"]}}, 1, nil)
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatal(err)
	}
	stored := reload(t, repo, ws, maria.ID)
	if stored.Name != "Maria Souza" || stored.BirthDate == nil || *stored.BirthDate != date || stored.Owner != row.Owner || stored.CustomFields["nota"] != 8.5 ||
		stored.WhatsAppOptIn == nil || !stored.WhatsAppOptIn.GrantedAt.Equal(row.Consent.GrantedAt) || stored.WhatsAppOptIn.Purpose != "Novidades" || stored.StoredAge != nil {
		t.Fatalf("stored = %+v", stored)
	}
}
