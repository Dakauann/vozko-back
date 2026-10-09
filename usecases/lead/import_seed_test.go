package lead_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
)

func storedLeadWithLegacyEmail() *lead.Lead {
	return &lead.Lead{ID: "lead-legacy", WorkspaceID: impWorkspace, Number: "5511987654321", Name: "Maria Aparecida", NameSource: lead.SourceManual, Email: "legacy-email", Version: 1}
}

const seedFileWithARejectedRow = "telefone;nome\n11987654321;Maria Souza\n11988887777;Bruna Souza\n"

func TestTheInboxSeedSkipsRowsTheImportRejected(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedLeadWithLegacyEmail())
	job := f.upload(t, seedFileWithARejectedRow)
	s := suggested(job)
	s.SeedInbox = true
	got := f.start(t, f.dryRun(t, job, s))
	if got.Status != leadimport.StatusDone || got.Result == nil || got.Result.Rejected != 1 || got.Result.Created != 1 {
		t.Fatalf("job = %+v, result = %+v", got, got.Result)
	}
	var numbers []string
	for _, batch := range f.seeder.published {
		for _, target := range batch.Targets {
			numbers = append(numbers, target.Number)
		}
	}
	if !reflect.DeepEqual(numbers, []string{"5511988887777"}) || got.Seed == nil || got.Seed.Queued != 1 {
		t.Fatalf("seeded %v (seed %+v), want only the imported row and never the rejected one", numbers, got.Seed)
	}
}

func TestTheInboxSeedRefusesWhenTheRejectedRowsCannotBeRead(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedLeadWithLegacyEmail())
	job := f.upload(t, seedFileWithARejectedRow)
	s := suggested(job)
	s.SeedInbox = true
	analyzed := f.dryRun(t, job, s)
	f.store.issuesErr = errors.New("issues unavailable")
	got := f.start(t, analyzed)
	if len(f.seeder.published) != 0 {
		t.Fatalf("published %d batches without knowing which rows were rejected", len(f.seeder.published))
	}
	if got.Status == leadimport.StatusDone {
		t.Fatalf("job = %+v, want the import not finished while the rejected rows are unknown", got)
	}
}
