package lead

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/address"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/infra/database/schema"
)

func linkCounts(job *leadimport.Job) func(created int, failed []lead.ImportIssue) leadimport.Counts {
	return func(created int, failed []lead.ImportIssue) leadimport.Counts {
		result := job.Result.Clone()
		result.LinksCreated += created
		for _, f := range failed {
			result.Issue(f)
		}
		return result
	}
}

func TestImportReportsFamilyLinksThatAlreadyExistAgainstPostgres(t *testing.T) {
	db, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Name: "Maria", Number: "5511987654321"})
	bruna := newLead(t, repo, ws, lead.Draft{Name: "Bruna", Number: "5511988887777"})
	ana := newLead(t, repo, ws, lead.Draft{Name: "Ana", Number: "5511977776666"})
	sibling, err := lead.NewRelation(maria.ID, bruna.ID, lead.KindSibling, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddRelation(ctx, ws, sibling); err != nil {
		t.Fatal(err)
	}
	job := importingJob(t, imports, ws)
	if err := db.Exec("UPDATE lead_imports SET stage = 'links' WHERE id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	link := func(line int, leadID, otherID string, kind lead.RelationKind) leadimport.LinkWrite {
		rel, err := lead.NewRelation(otherID, leadID, kind, job.RequestedBy)
		if err != nil {
			t.Fatal(err)
		}
		return leadimport.LinkWrite{Link: leadimport.PendingLink{ID: uuid.NewString(), Line: line, LeadID: leadID, Kind: kind}, Relation: &rel}
	}
	writes := []leadimport.LinkWrite{
		link(2, bruna.ID, maria.ID, lead.KindChild),
		link(3, ana.ID, maria.ID, lead.KindChild),
		link(4, ana.ID, maria.ID, lead.KindChild),
	}
	if err := imports.WriteLinks(ctx, leadimport.LinkBatch{Job: job, Links: writes, Checkpoint: linkCounts(job)}); err != nil {
		t.Fatalf("WriteLinks: %v", err)
	}
	issues, err := imports.Issues(ctx, job.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, issue := range issues {
		if issue.Reason != lead.ReasonRelationExists || issue.Field != lead.ImportFieldRelative || issue.Rejected {
			t.Fatalf("issue = %+v", issue)
		}
		lines = append(lines, issue.Line)
	}
	if len(lines) != 2 || lines[0] != 2 || lines[1] != 4 {
		t.Fatalf("issue lines = %v, want the pair already linked and the repeated row", lines)
	}
	stored, _ := imports.Get(ctx, ws, job.ID)
	if stored.Result.LinksCreated != 1 || stored.Result.Issues[string(lead.ReasonRelationExists)] != 2 {
		t.Fatalf("result = %+v", stored.Result)
	}
}

func TestImportFillsAnAddressThatOnlyHadItsCityAgainstPostgres(t *testing.T) {
	db, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Number: "5511987654321",
		Addresses: []lead.AddressInput{{Label: lead.AddressHome, Primary: true, Postal: address.Postal{City: "São Paulo", State: "SP"}}}})
	job := importingJob(t, imports, ws)
	found, err := imports.ByIdentity(ctx, ws, []string{"5511987654321"})
	if err != nil {
		t.Fatal(err)
	}
	row := record(ws, 2, "5511987654321", "")
	row.Address = &lead.Address{Label: lead.AddressHome, Primary: true, Postal: paulista, GeoStatus: lead.GeoPending}
	batch, outcomes := rowBatch(job, []leadimport.RowItem{{Record: row, Existing: found["5511987654321"]}}, 1, nil)
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if d := (*outcomes)[0].Decision; d.Verdict != lead.ImportEnriched || d.FilledAddress == nil {
		t.Fatalf("decision = %+v", d)
	}
	stored := reload(t, repo, ws, maria.ID)
	if len(stored.Addresses) != 1 || stored.Addresses[0].ID != maria.Addresses[0].ID || stored.Addresses[0].Postal.Street != "Avenida Paulista" ||
		stored.Addresses[0].GeoStatus != lead.GeoPending || stored.Version != maria.Version+1 {
		t.Fatalf("stored = %+v / %+v", stored, stored.Addresses)
	}
	var queued int64
	db.Model(&schema.LeadAddress{}).Where("id = ? AND geo_next_at IS NOT NULL AND fingerprint = ?", maria.Addresses[0].ID, paulista.Fingerprint()).Count(&queued)
	if queued != 1 {
		t.Fatal("the filled address is queued for the geocoder with its new fingerprint")
	}
	var stamped int64
	db.Model(&schema.LeadAddress{}).Where("id = ? AND import_id = ?", maria.Addresses[0].ID, job.ID).Count(&stamped)
	if stamped != 1 {
		t.Fatal("the address the import completed is tagged with the import")
	}
	if got, err := imports.Placement(ctx, ws, job.ID); err != nil || got != (leadimport.Placement{Pending: 1}) {
		t.Fatalf("Placement = %+v, %v; want the completed address waiting for its position", got, err)
	}
}

func TestHoldingPhonesRefusesToGuessPastTheCapAgainstPostgres(t *testing.T) {
	db, _, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	if err := db.Exec("INSERT INTO leads (id, workspace_id, name, source, version, created_at, updated_at)"+
		" SELECT gen_random_uuid(), ?, 'Pessoa ' || g, 'manual', 1, now(), now() FROM generate_series(1, ?) AS g", ws, leadimport.MaxContactHolders+1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO lead_phones (id, workspace_id, lead_id, number, label, position, created_at)"+
		" SELECT gen_random_uuid(), workspace_id, id, '551133334444', 'landline', 0, now() FROM leads WHERE workspace_id = ?", ws).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := imports.HoldingPhones(ctx, ws, []string{"551133334444"}); err != leadimport.ErrTooManyHolders {
		t.Fatalf("HoldingPhones = %v, want ErrTooManyHolders", err)
	}
	if err := db.Exec("UPDATE leads SET number = '5511987650000' WHERE id = (SELECT id FROM leads WHERE workspace_id = ? ORDER BY id DESC LIMIT 1)", ws).Error; err != nil {
		t.Fatal(err)
	}
	counts, err := imports.PhoneHolderCounts(ctx, ws, []string{"551133334444", "5511987650000", "551155556666"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["551133334444"] != leadimport.MaxContactHolders+1 || counts["5511987650000"] != 1 || counts["551187650000"] != 0 || counts["551155556666"] != 0 {
		t.Fatalf("counts = %v", counts)
	}
	if err := db.Exec("DELETE FROM lead_phones WHERE lead_id = (SELECT id FROM leads WHERE workspace_id = ? ORDER BY id LIMIT 1)", ws).Error; err != nil {
		t.Fatal(err)
	}
	holders, err := imports.HoldingPhones(ctx, ws, []string{"551133334444"})
	if err != nil || len(holders) != leadimport.MaxContactHolders {
		t.Fatalf("holders = %d, %v; want every holder at the cap", len(holders), err)
	}
	if err := db.Exec("UPDATE leads SET deleted_at = now() WHERE id = (SELECT id FROM leads WHERE workspace_id = ? ORDER BY id DESC LIMIT 1)", ws).Error; err != nil {
		t.Fatal(err)
	}
	if counts, err = imports.PhoneHolderCounts(ctx, ws, []string{"551133334444", "5511987650000"}); err != nil || counts["551133334444"] != leadimport.MaxContactHolders-1 || counts["5511987650000"] != 0 {
		t.Fatalf("counts after a deletion = %v, %v", counts, err)
	}
}

func TestUnusedUploadsAndLongIssueFieldsAgainstPostgres(t *testing.T) {
	_, _, imports := importDB(t)
	ctx := context.Background()
	ws, user := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	var ids []string
	for _, status := range []leadimport.Status{leadimport.StatusUploaded, leadimport.StatusAnalyzed, leadimport.StatusFailed, leadimport.StatusDone} {
		job, err := leadimport.NewJob(ws, user, leadimport.File{Name: "a.csv"}, leadimport.Preview{}, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		job.ID, job.Status = uuid.NewString(), status
		if err := imports.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, job.ID)
		now = now.Add(time.Second)
	}
	unused, err := imports.Unused(ctx, ws, user)
	if err != nil || len(unused) != 3 || unused[0].ID != ids[0] || unused[2].ID != ids[2] {
		t.Fatalf("unused = %+v, %v", unused, err)
	}
	if other, _ := imports.Unused(ctx, ws, uuid.NewString()); len(other) != 0 {
		t.Fatalf("another person's uploads = %+v", other)
	}

	job := importingJob(t, imports, uuid.NewString())
	long := lead.ImportCustomField(strings.Repeat("k", 120))
	batch, _ := rowBatch(job, nil, 1, []lead.ImportIssue{{Line: 2, Reason: lead.ReasonCustomFieldInvalid, Field: long}})
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatalf("an issue on a 120 character custom field key = %v", err)
	}
	issues, err := imports.Issues(ctx, job.ID, 0, 10)
	if err != nil || len(issues) != 1 || issues[0].Field != long {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
}

func explainPreferringIndexes(t *testing.T, db *gorm.DB, sql string, args ...any) string {
	t.Helper()
	var plan []string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan = off; SET LOCAL enable_bitmapscan = off; SET LOCAL enable_sort = off").Error; err != nil {
			return err
		}
		return tx.Raw("EXPLAIN "+sql, args...).Scan(&plan).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

func TestImportPagesReadAnIndexInOrderAgainstPostgres(t *testing.T) {
	db, _, _ := importDB(t)
	id := uuid.NewString()
	cases := map[string]struct {
		sql   string
		args  []any
		index string
	}{
		"family links": {importPendingLinksSQL, []any{id, leadimport.BatchRows}, "idx_lead_import_links_page"},
		"rejections":   {importIssuesSQL, []any{id, 0, maxIssuePage}, "idx_lead_import_issues_page"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			plan := explainPreferringIndexes(t, db, tc.sql, tc.args...)
			if !strings.Contains(plan, tc.index) || strings.Contains(plan, "Sort") {
				t.Fatalf("plan does not walk %s in order:\n%s", tc.index, plan)
			}
		})
	}
}
