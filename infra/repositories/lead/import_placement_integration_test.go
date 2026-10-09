package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
)

func TestImportStampsItsAddressesAndCountsWhereTheyLandedAgainstPostgres(t *testing.T) {
	db, repo, imports := importDB(t)
	ctx := context.Background()
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Name: "Maria Aparecida", Number: "5511987654321"})
	job := importingJob(t, imports, ws)
	found, err := imports.ByIdentity(ctx, ws, []string{"5511987654321"})
	if err != nil {
		t.Fatal(err)
	}

	exact := &lead.Address{Label: lead.AddressHome, Primary: true, Postal: paulista, GeoStatus: lead.GeoLocated,
		Fix: &geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionExact, Source: geo.SourceImport, FixedAt: time.Now().UTC()}}
	pending := &lead.Address{Label: lead.AddressHome, Primary: true, Postal: bahia, GeoStatus: lead.GeoPending}
	enrich := record(ws, 2, "5511987654321", "Maria Souza")
	enrich.Address = exact
	bruna := record(ws, 3, "5511988887777", "Bruna Souza")
	bruna.Address = pending
	davi := record(ws, 4, "5511977776666", "Davi Dias")

	batch, _ := rowBatch(job, []leadimport.RowItem{{Record: enrich, Existing: found["5511987654321"]}, {Record: bruna}, {Record: davi}}, 3, nil)
	if err := imports.WriteRows(ctx, batch); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	var stamped int64
	if err := db.Raw("SELECT COUNT(*) FROM lead_addresses WHERE workspace_id = ? AND import_id = ?", ws, job.ID).Scan(&stamped).Error; err != nil || stamped != 2 {
		t.Fatalf("addresses stamped with the import = %d, %v", stamped, err)
	}
	other := newLead(t, repo, ws, lead.Draft{Name: "Rui Lima", Number: "5511966665555"})
	if err := db.Exec("INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, position, geo_status, fingerprint, created_at, updated_at)"+
		" VALUES (?, ?, ?, 'home', true, 0, 'pending', 'x', now(), now())", uuid.NewString(), ws, other.ID).Error; err != nil {
		t.Fatal(err)
	}

	got, err := imports.Placement(ctx, ws, job.ID)
	if err != nil || got != (leadimport.Placement{OnMap: 1, Pending: 1}) {
		t.Fatalf("Placement = %+v, %v; want Maria on the map and Bruna waiting, and no address from outside the import", got, err)
	}
	if err := db.Exec("UPDATE lead_addresses SET geo_status = 'approximate', latitude = -19.92, longitude = -43.94, geo_precision = 'district' WHERE import_id = ? AND lead_id <> ?", job.ID, maria.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := imports.Placement(ctx, ws, job.ID); err != nil || got != (leadimport.Placement{OnMap: 1, Approximate: 1}) {
		t.Fatalf("after the sweeper placed Bruna by bairro: %+v, %v", got, err)
	}
	if err := db.Exec("UPDATE leads SET deleted_at = now(), version = version + 1 WHERE workspace_id = ? AND number = ?", ws, "5511988887777").Error; err != nil {
		t.Fatal(err)
	}
	if got, err := imports.Placement(ctx, ws, job.ID); err != nil || got != (leadimport.Placement{OnMap: 1}) {
		t.Fatalf("after Bruna was deleted: %+v, %v; want only live leads, as on the map", got, err)
	}
	if got, err := imports.Placement(ctx, uuid.NewString(), job.ID); err != nil || got != (leadimport.Placement{}) {
		t.Fatalf("another workspace read the placement: %+v, %v", got, err)
	}
}

func TestMineListsTheImportersLiveImportsAgainstPostgres(t *testing.T) {
	_, _, imports := importDB(t)
	ctx := context.Background()
	ws, user := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	for i, spec := range []struct {
		requestedBy string
		age         time.Duration
	}{{user, time.Hour}, {user, 2 * time.Hour}, {uuid.NewString(), time.Hour}, {user, leadimport.Retention + time.Hour}} {
		created := now.Add(-spec.age)
		job, err := leadimport.NewJob(ws, spec.requestedBy, leadimport.File{Name: "escola.csv", SizeBytes: 10}, leadimport.Preview{Headers: []string{"telefone"}}, 10, created)
		if err != nil {
			t.Fatal(err)
		}
		job.ID = uuid.NewString()
		job.Status = leadimport.StatusDone
		if err := imports.Create(ctx, job); err != nil {
			t.Fatalf("job %d: %v", i, err)
		}
	}
	jobs, err := imports.Mine(ctx, ws, user, now, leadimport.MaxListed)
	if err != nil || len(jobs) != 2 || !jobs[0].CreatedAt.After(jobs[1].CreatedAt) {
		t.Fatalf("Mine = %+v, %v; want the importer's two live imports, newest first", jobs, err)
	}
}
