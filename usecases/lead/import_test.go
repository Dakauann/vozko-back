package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/unofficial_whatsapp"
	"vozko/domain/workspace"
)

const (
	impWorkspace = "ws-imp"
	impUser      = "user-imp"
)

var importClock = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

type importFixture struct {
	store   *fakeImportStore
	base    *fakeLeadBase
	writer  *fakeImportWriter
	files   *fakeImportFiles
	seeder  *fakeSeeder
	metrics *fakeImportMetrics
	perms   fakePermissions
	runs    *queuedRuns
	clock   time.Time
	imp     *Import
}

func allImportPermissions() fakePermissions {
	return fakePermissions{
		"leads:create": true, "leads:read": true, "leads:update": true, "leads:assign": true,
		"leads:read_addresses": true, "leads:read_sensitive": true, "unofficial_whatsapp_instances:send": true, "media:read": true,
	}
}

func newImportFixture(t *testing.T, perms fakePermissions, leads ...*lead.Lead) *importFixture {
	t.Helper()
	f := &importFixture{store: newFakeImportStore(), base: newFakeLeadBase(leads...), files: &fakeImportFiles{files: map[string][]byte{}},
		seeder: &fakeSeeder{}, metrics: newFakeImportMetrics(), perms: perms, runs: &queuedRuns{}, clock: importClock}
	f.writer = &fakeImportWriter{base: f.base, store: f.store}
	ids := 0
	imp, err := NewImport(ImportDeps{
		Jobs: f.store, Writer: f.writer, Lookup: f.base, Files: f.files,
		Members:     fakeMembers{"clara@escola.com": "u-clara", "rui@escola.com": "u-rui"},
		Permissions: perms, Visibility: fakeVisibility{"u-clara": true},
		Definitions: &fakeDefinitions{defs: []*customfield.Definition{
			{Key: "interesse", Label: "Interesse", Type: customfield.TypeText},
			{Key: "classificacao", Label: "Classificação", Type: customfield.TypeText, Sensitive: true},
		}},
		Seeder:     f.seeder,
		Metrics:    f.metrics,
		Now:        func() time.Time { return f.clock },
		NewID:      func() string { ids++; return fmt.Sprintf("id-%d", ids) },
		Background: f.runs.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.imp = imp
	return f
}

func importer() Actor { return Actor{UserID: impUser, WorkspaceID: impWorkspace} }

const schoolFile = "telefone;nome;email;cep;cidade;uf;familiar de;parentesco;responsavel;fone casa\n" +
	"11987654321;Maria Souza;maria@x.com;06404000;Barueri;SP;;;clara@escola.com;1133334444\n" +
	"11988887777;Bruna Souza;;;;;11987654321;filha;;\n" +
	"11912345678;João Lima;joao@;;;;11955554444;pai;rui@escola.com;\n" +
	"abc;Errado;;;;;;;;\n" +
	"11977776666;Carla Dias;;;;;11966665555;irmã;nobody@escola.com;\n" +
	"11966665555;Davi Dias;;;;;;;;\n"

func storedMaria() *lead.Lead {
	return &lead.Lead{ID: "lead-maria", WorkspaceID: impWorkspace, Number: "5511987654321", Name: "Maria Aparecida", NameSource: lead.SourceManual, Version: 3}
}

func (f *importFixture) upload(t *testing.T, data string) *leadimport.Job {
	t.Helper()
	job, err := f.imp.Upload(context.Background(), importer(), UploadInput{FileName: "C:\\docs\\escola.csv", Data: []byte(data)})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func suggested(job *leadimport.Job) leadimport.Settings {
	return leadimport.Settings{Columns: job.Preview.Columns, Policy: lead.PolicyFillEmpty}
}

func (f *importFixture) dryRun(t *testing.T, job *leadimport.Job, s leadimport.Settings) *leadimport.Job {
	t.Helper()
	if _, err := f.imp.DryRun(context.Background(), importer(), job.ID, s); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	return f.store.job(job.ID)
}

func (f *importFixture) start(t *testing.T, job *leadimport.Job) *leadimport.Job {
	t.Helper()
	if _, err := f.imp.Start(context.Background(), importer(), job.ID); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	return f.store.job(job.ID)
}

func TestNewImportRefusesAMissingDependency(t *testing.T) {
	if _, err := NewImport(ImportDeps{}); !errors.Is(err, errImportIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadKeepsTheFileAndSuggestsTheMapping(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)

	if job.Status != leadimport.StatusUploaded || job.TotalRows != 6 || job.File.Name != "escola.csv" || !job.File.Owned || job.File.MediaID == "" {
		t.Fatalf("job = %+v", job)
	}
	if got := f.files.files[impWorkspace+"/"+job.File.MediaID]; string(got) != schoolFile {
		t.Fatal("the uploaded file was not kept through the media pipeline")
	}
	want := []string{leadimport.FieldNumber, leadimport.FieldName, leadimport.FieldEmail, leadimport.FieldZipCode, leadimport.FieldCity, leadimport.FieldState,
		leadimport.FieldRelativeNumber, leadimport.FieldRelationKind, leadimport.FieldOwnerEmail, leadimport.FieldPhoneLandline}
	for i, c := range job.Preview.Columns {
		if c.Field != want[i] {
			t.Fatalf("suggested %q for %q, want %q", c.Field, c.Header, want[i])
		}
	}
	if len(job.Preview.Sample) != leadimport.SampleRows {
		t.Fatalf("sample = %d rows", len(job.Preview.Sample))
	}
}

func TestUploadRefusesWhatIsNotASheetAndKeepsNothing(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	for _, data := range []string{"PK\x03\x04xlsx", "telefone;nome\n"} {
		if _, err := f.imp.Upload(context.Background(), importer(), UploadInput{Data: []byte(data)}); err == nil {
			t.Fatalf("upload of %q was accepted", data)
		}
	}
	if len(f.files.files) != 0 || len(f.store.jobs) != 0 {
		t.Fatal("a refused upload left a file or a job behind")
	}
	if _, err := newImportFixture(t, fakePermissions{"leads:read": true}).imp.Upload(context.Background(), importer(), UploadInput{Data: []byte(schoolFile)}); !errors.Is(err, leadimport.ErrForbidden) {
		t.Fatalf("upload without leads:create = %v", err)
	}
}

func TestUploadFromTheMediaLibraryDoesNotOwnTheFile(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	f.files.files[impWorkspace+"/lib-1"] = []byte(schoolFile)
	job, err := f.imp.Upload(context.Background(), importer(), UploadInput{MediaID: "lib-1"})
	if err != nil {
		t.Fatal(err)
	}
	if job.File.Owned || job.File.MediaID != "lib-1" || len(f.files.files) != 1 {
		t.Fatalf("job file = %+v", job.File)
	}
	if _, err := f.imp.Upload(context.Background(), importer(), UploadInput{MediaID: "other"}); !errors.Is(err, leadimport.ErrFileUnavailable) {
		t.Fatalf("unknown media = %v", err)
	}
}

func TestOnlyTheImporterSeesTheImport(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	other := Actor{UserID: "user-2", WorkspaceID: impWorkspace}
	if _, err := f.imp.Get(context.Background(), other, job.ID); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("another member read the import: %v", err)
	}
	if _, err := f.imp.Rejections(context.Background(), other, job.ID, 0, 10); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("another member read the rejections: %v", err)
	}
	if _, err := f.imp.DryRun(context.Background(), other, job.ID, suggested(job)); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("another member configured the import: %v", err)
	}
	if got, err := f.imp.Get(context.Background(), importer(), job.ID); err != nil || got.ID != job.ID {
		t.Fatalf("the importer lost the import: %v", err)
	}
}

func TestDryRunCountsWithoutWritingAnything(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.dryRun(t, f.upload(t, schoolFile), suggested(f.upload(t, schoolFile)))

	if job.Status != leadimport.StatusAnalyzed || job.DryRun == nil {
		t.Fatalf("job = %+v", job)
	}
	got := *job.DryRun
	if got.Rows != 6 || got.Created != 4 || got.Enriched != 1 || got.Rejected != 1 || got.Conflicting != 1 {
		t.Fatalf("dry run = %+v", got)
	}
	if got.LinksPlanned != 2 || got.Issues[string(lead.ReasonRelativeNotFound)] != 1 {
		t.Fatalf("links = %d, issues = %+v; want Bruna and Carla linked and João's relative missing", got.LinksPlanned, got.Issues)
	}
	if got.Issues[string(lead.ReasonEmailInvalid)] != 1 || got.Issues[string(lead.ReasonOwnerNotFound)] != 1 || got.Issues[string(lead.ReasonOwnerOutOfReach)] != 1 {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if f.base.writes != 0 || len(f.base.leads) != 1 || f.base.leads["lead-maria"].Version != 3 {
		t.Fatal("a dry run wrote leads")
	}
}

func TestEachColumnNeedsItsPermission(t *testing.T) {
	base := func() fakePermissions {
		return fakePermissions{"leads:create": true, "leads:update": true}
	}
	cases := []struct {
		name   string
		fields []string
		policy lead.ExistingPolicy
		perms  fakePermissions
		want   workspace.Action
	}{
		{"an owner column without assign", []string{leadimport.FieldNumber, leadimport.FieldOwnerEmail}, lead.PolicySkip, base(), workspace.ActionAssign},
		{"a sensitive column without the sensitive permission", []string{leadimport.FieldNumber, lead.ImportCustomField("classificacao")}, lead.PolicySkip, base(), workspace.ActionReadSensitive},
		{"an address column without the address permission", []string{leadimport.FieldNumber, leadimport.FieldCity}, lead.PolicySkip, base(), workspace.ActionReadAddresses},
		{"filling existing leads without update", []string{leadimport.FieldNumber}, lead.PolicyFillEmpty, fakePermissions{"leads:create": true}, workspace.ActionUpdate},
		{"family columns without update", []string{leadimport.FieldNumber, leadimport.FieldRelativeNumber}, lead.PolicySkip, fakePermissions{"leads:create": true}, workspace.ActionUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newImportFixture(t, tc.perms)
			job := f.upload(t, "a;b\n11987654321;x\n")
			s := leadimport.Settings{Policy: tc.policy}
			for i, field := range tc.fields {
				s.Columns = append(s.Columns, leadimport.Column{Index: i, Field: field})
			}
			_, err := f.imp.DryRun(context.Background(), importer(), job.ID, s)
			var refusal *leadimport.PermissionError
			if !errors.As(err, &refusal) || refusal.Action != tc.want {
				t.Fatalf("DryRun = %v, want a refusal for leads:%s", err, tc.want)
			}
			if f.store.job(job.ID).Status != leadimport.StatusUploaded || len(f.runs.runs) != 0 {
				t.Fatal("a refused dry run changed the import")
			}
		})
	}
}

func TestStartNeedsTheDryRun(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	if _, err := f.imp.Start(context.Background(), importer(), job.ID); !errors.Is(err, leadimport.ErrNotReady) {
		t.Fatalf("start before a dry run = %v", err)
	}
}

func TestImportWritesEnrichesAndLinksFamilies(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.upload(t, schoolFile)
	job = f.dryRun(t, job, suggested(job))
	job = f.start(t, job)

	if job.Status != leadimport.StatusDone || job.Result == nil || job.Processed != 6 || job.Claim != "" || job.FinishedAt == nil {
		t.Fatalf("job = %+v", job)
	}
	r := *job.Result
	if r.Created != 4 || r.Enriched != 1 || r.Rejected != 1 || r.LinksPlanned != 3 || r.LinksCreated != 2 {
		t.Fatalf("result = %+v", r)
	}
	maria := f.base.leads["lead-maria"]
	if maria.Name != "Maria Aparecida" || maria.Email != "maria@x.com" || maria.Owner != "u-clara" || len(maria.Phones) != 1 || len(maria.Addresses) != 1 {
		t.Fatalf("maria = %+v, want the manual name kept and the empty fields filled", maria)
	}
	bruna := f.base.byNumber("5511988887777")
	if bruna == nil || bruna.Source != lead.SourceImport || bruna.NameSource != lead.SourceImport {
		t.Fatalf("bruna = %+v", bruna)
	}
	davi, carla := f.base.byNumber("5511966665555"), f.base.byNumber("5511977776666")
	if len(f.base.relations) != 2 {
		t.Fatalf("relations = %+v", f.base.relations)
	}
	rel := f.base.relations[1]
	if rel.KindFor(davi.ID) != lead.KindSibling || !rel.Involves(carla.ID) {
		t.Fatalf("relation = %+v, want Carla linked to Davi, who comes later in the file", rel)
	}
	bruneLinks := 0
	for _, issue := range f.store.issues[job.ID] {
		if issue.Reason == lead.ReasonRelativeNotFound {
			bruneLinks++
		}
	}
	if bruneLinks != 1 {
		t.Fatalf("issues = %+v", f.store.issues[job.ID])
	}
	rows, err := f.imp.Rejections(context.Background(), importer(), job.ID, 0, 100)
	if err != nil || len(rows) == 0 || rows[0].Line > rows[len(rows)-1].Line {
		t.Fatalf("rejections = %+v, %v", rows, err)
	}
}

func TestBrunaIsLinkedToTheMariaAlreadyInTheBase(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.upload(t, "telefone;nome;familiar de;parentesco\n11988887777;Bruna Souza;11987654321;filha\n")
	f.start(t, f.dryRun(t, job, suggested(job)))
	bruna := f.base.byNumber("5511988887777")
	if len(f.base.relations) != 1 || f.base.relations[0].KindFor("lead-maria") != lead.KindChild || !f.base.relations[0].Involves(bruna.ID) {
		t.Fatalf("relations = %+v, want Bruna as Maria's daughter", f.base.relations)
	}
}

func TestSkipLeavesExistingLeadsAlone(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.upload(t, schoolFile)
	s := suggested(job)
	s.Policy = lead.PolicySkip
	job = f.start(t, f.dryRun(t, job, s))
	maria := f.base.leads["lead-maria"]
	if job.Result.Skipped != 1 || maria.Email != "" || maria.Version != 3 {
		t.Fatalf("result = %+v, maria = %+v", job.Result, maria)
	}
}

func TestOneRunningImportPerWorkspace(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	first, second := f.upload(t, schoolFile), f.upload(t, schoolFile)
	if _, err := f.imp.DryRun(context.Background(), importer(), first.ID, suggested(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.imp.DryRun(context.Background(), importer(), second.ID, suggested(second)); !errors.Is(err, leadimport.ErrRunning) {
		t.Fatalf("a second running import = %v, want ErrRunning", err)
	}
	f.runs.drain()
	if _, err := f.imp.DryRun(context.Background(), importer(), second.ID, suggested(second)); err != nil {
		t.Fatalf("after the first finished = %v", err)
	}
}

func TestTheWorkerChecksThePermissionsAgain(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	job = f.dryRun(t, job, suggested(job))
	if _, err := f.imp.Start(context.Background(), importer(), job.ID); err != nil {
		t.Fatal(err)
	}
	delete(f.perms, "leads:assign")
	f.runs.drain()
	got := f.store.job(job.ID)
	if got.Status != leadimport.StatusFailed || got.FailureCode != leadimport.FailureForbidden || f.base.writes != 0 {
		t.Fatalf("job = %+v, writes = %d", got, f.base.writes)
	}
}

func TestAJobWithoutItsFileFails(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	delete(f.files.files, impWorkspace+"/"+job.File.MediaID)
	got := f.dryRun(t, job, suggested(job))
	if got.Status != leadimport.StatusFailed || got.FailureCode != leadimport.FailureFileUnavailable {
		t.Fatalf("job = %+v", got)
	}
}

func bigFile(rows int) string {
	var b strings.Builder
	b.WriteString("telefone;nome\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "119%08d;Pessoa %d\n", 10000000+i, i)
	}
	return b.String()
}

func TestASilentWorkerIsResumedFromItsCheckpoint(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, bigFile(1200))
	job = f.dryRun(t, job, suggested(job))
	if _, err := f.imp.Start(context.Background(), importer(), job.ID); err != nil {
		t.Fatal(err)
	}
	f.runs.runs = nil
	claimed, err := f.store.Claim(context.Background(), job.ID, "dead-worker", f.clock)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.imp.pass(claimed)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("worker died")
	src, _ := f.imp.fileSource(context.Background(), claimed)
	written := 0
	_ = inBatches(src, 0, nil, func(rows []lead.ImportRow) error {
		if written == 1 {
			return stop
		}
		items, issues, _ := p.prepare(context.Background(), rows)
		written++
		return f.writer.WriteRows(context.Background(), leadimport.RowBatch{Job: claimed, Items: items, Decide: p.decide,
			Checkpoint: func(o []leadimport.RowOutcome) leadimport.Checkpoint {
				return checkpointOf(leadimport.Counts{}, len(rows), issues, o)
			}})
	})
	if f.store.job(job.ID).Processed != leadimport.BatchRows {
		t.Fatalf("checkpoint = %d", f.store.job(job.ID).Processed)
	}

	f.clock = f.clock.Add(leadimport.StaleAfter + time.Second)
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	got := f.store.job(job.ID)
	if got.Status != leadimport.StatusDone || got.Result.Created != 1200 || got.Processed != 1200 || len(f.base.leads) != 1200 {
		t.Fatalf("job = %+v, leads = %d", got.Result, len(f.base.leads))
	}
	if first := f.writer.rows[1][0]; first != leadimport.BatchRows+2 {
		t.Fatalf("the resumed worker started at line %d, want %d", first, leadimport.BatchRows+2)
	}
}

func TestAWorkerThatKeepsDyingIsStopped(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	job = f.dryRun(t, job, suggested(job))
	if _, err := f.imp.Start(context.Background(), importer(), job.ID); err != nil {
		t.Fatal(err)
	}
	f.runs.runs = nil
	for i := 0; i < leadimport.MaxAttempts; i++ {
		if _, err := f.store.Claim(context.Background(), job.ID, fmt.Sprintf("w-%d", i), f.clock); err != nil {
			t.Fatal(err)
		}
		f.clock = f.clock.Add(leadimport.StaleAfter + time.Second)
	}
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.store.job(job.ID); got.Status != leadimport.StatusFailed || got.FailureCode != leadimport.FailureStalled {
		t.Fatalf("job = %+v", got)
	}
}

func TestExpiredImportsAreErased(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	owned := f.upload(t, schoolFile)
	f.files.files[impWorkspace+"/lib-1"] = []byte(schoolFile)
	library, err := f.imp.Upload(context.Background(), importer(), UploadInput{MediaID: "lib-1"})
	if err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(leadimport.Retention + time.Minute)
	if _, err := f.imp.Get(context.Background(), importer(), owned.ID); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("an expired import was still readable: %v", err)
	}
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.store.jobs) != 0 || len(f.files.erased) != 1 || f.files.erased[0] != owned.File.MediaID {
		t.Fatalf("jobs = %d, erased = %v", len(f.store.jobs), f.files.erased)
	}
	if _, ok := f.files.files[impWorkspace+"/"+library.File.MediaID]; !ok {
		t.Fatal("a library file the import did not own was erased")
	}
}

func TestSeedingKeepsItsGates(t *testing.T) {
	script := &unofficial_whatsapp.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4}
	cases := []struct {
		name        string
		perms       func(p fakePermissions)
		admin       bool
		seederErr   error
		wantQueued  int
		wantScript  bool
		wantError   string
		wantSError  string
		wantPublish int
	}{
		{"a platform admin seeds the script", nil, true, nil, 5, true, "", "", 1},
		{"a member seeds plain conversations without the script", nil, false, nil, 5, false, "", leadimport.SeedScriptForbidden, 1},
		{"without the channel permission nothing is seeded", func(p fakePermissions) { delete(p, "unofficial_whatsapp_instances:send") }, true, nil, 0, false, leadimport.SeedForbidden, "", 0},
		{"a queue failure is reported", nil, true, errors.New("broker down"), 0, true, leadimport.SeedFailed, "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			perms := allImportPermissions()
			if tc.perms != nil {
				tc.perms(perms)
			}
			f := newImportFixture(t, perms)
			f.seeder.err = tc.seederErr
			a := importer()
			a.IsAdmin = tc.admin
			job, err := f.imp.Upload(context.Background(), a, UploadInput{Data: []byte(schoolFile)})
			if err != nil {
				t.Fatal(err)
			}
			s := suggested(job)
			s.SeedInbox, s.Script = true, script
			if _, err := f.imp.DryRun(context.Background(), a, job.ID, s); err != nil {
				t.Fatal(err)
			}
			f.runs.drain()
			if _, err := f.imp.Start(context.Background(), a, job.ID); err != nil {
				t.Fatal(err)
			}
			f.runs.drain()
			got := f.store.job(job.ID)
			if got.Status != leadimport.StatusDone || got.Seed == nil {
				t.Fatalf("job = %+v", got)
			}
			if got.Seed.Queued != tc.wantQueued || got.Seed.Error != tc.wantError || got.Seed.ScriptError != tc.wantSError || len(f.seeder.published) != tc.wantPublish {
				t.Fatalf("seed = %+v, published = %d", got.Seed, len(f.seeder.published))
			}
			if tc.wantPublish > 0 && (f.seeder.published[0].Script != nil) != tc.wantScript {
				t.Fatalf("script sent = %v, want %v", f.seeder.published[0].Script != nil, tc.wantScript)
			}
		})
	}
}

func TestCatalogTellsWhichColumnsTheImporterMayMap(t *testing.T) {
	f := newImportFixture(t, fakePermissions{"leads:create": true, "leads:read_addresses": true})
	got, err := f.imp.Catalog(context.Background(), importer())
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, field := range got.Fields {
		allowed[field.Key] = field.Allowed
	}
	if !allowed[leadimport.FieldNumber] || !allowed[leadimport.FieldCity] || allowed[leadimport.FieldOwnerEmail] ||
		allowed[lead.ImportCustomField("classificacao")] || !allowed[lead.ImportCustomField("interesse")] || allowed[leadimport.FieldRelativeNumber] {
		t.Fatalf("allowed = %+v", allowed)
	}
	if got.FillEmpty || got.SeedInbox || got.SeedScript {
		t.Fatalf("catalog = %+v, want no fill_empty and no seeding", got)
	}
	admin := importer()
	admin.IsAdmin = true
	full, _ := newImportFixture(t, allImportPermissions()).imp.Catalog(context.Background(), admin)
	if !full.FillEmpty || !full.SeedInbox || !full.SeedScript {
		t.Fatalf("catalog = %+v", full)
	}
}

func TestAPassingWorkerErrorHandsTheImportBackToTheSweeper(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, bigFile(1200))
	job = f.dryRun(t, job, suggested(job))
	f.writer.failAt = 2
	got := f.start(t, job)
	if got.Status != leadimport.StatusImporting || got.Claim != "" || got.HeartbeatAt != nil || got.Processed != leadimport.BatchRows || got.FailureCode != "" {
		t.Fatalf("job = %+v, want it released at its checkpoint", got)
	}
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	got = f.store.job(job.ID)
	if got.Status != leadimport.StatusDone || got.Processed != 1200 || got.Result.Created != 1200 || len(f.base.leads) != 1200 {
		t.Fatalf("job = %+v, leads = %d", got, len(f.base.leads))
	}
	if first := f.writer.rows[1][0]; first != leadimport.BatchRows+2 {
		t.Fatalf("the next worker started at line %d, want %d", first, leadimport.BatchRows+2)
	}
}

func TestAWorkerErrorThatKeepsComingBackStopsTheImport(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	job = f.dryRun(t, job, suggested(job))
	f.base.failRows = true
	f.start(t, job)
	for i := 0; i < leadimport.MaxAttempts; i++ {
		if err := f.imp.Sweep(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.runs.drain()
	}
	if got := f.store.job(job.ID); got.Status != leadimport.StatusFailed || got.Attempts != leadimport.MaxAttempts {
		t.Fatalf("job = %+v, want it failed after %d attempts", got, leadimport.MaxAttempts)
	}
}

func TestUploadFromTheLibraryNeedsMediaAccessAndALibraryFile(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	f.files.files[impWorkspace+"/lib-1"] = []byte(schoolFile)
	private := f.upload(t, schoolFile)
	if _, err := f.imp.Upload(context.Background(), importer(), UploadInput{MediaID: private.File.MediaID}); !errors.Is(err, leadimport.ErrFileUnavailable) {
		t.Fatalf("another import's sheet read through the library = %v", err)
	}
	delete(f.perms, "media:read")
	_, err := f.imp.Upload(context.Background(), importer(), UploadInput{MediaID: "lib-1"})
	var refusal *leadimport.PermissionError
	if !errors.As(err, &refusal) || refusal.Permission() != "media:read" {
		t.Fatalf("library upload without media access = %v", err)
	}
	if _, err := f.imp.Upload(context.Background(), importer(), UploadInput{Data: []byte(schoolFile)}); err != nil {
		t.Fatalf("a file sent with the request needs no media access, got %v", err)
	}
}

func TestUnusedUploadsMakeRoomForANewOne(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	var jobs []*leadimport.Job
	for i := 0; i < leadimport.MaxUnusedUploads; i++ {
		jobs = append(jobs, f.upload(t, schoolFile))
		f.clock = f.clock.Add(time.Minute)
	}
	latest := f.upload(t, schoolFile)
	if _, err := f.store.Get(context.Background(), impWorkspace, jobs[0].ID); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("the oldest unused upload is erased, got %v", err)
	}
	if len(f.files.erased) != 1 || f.files.erased[0] != jobs[0].File.MediaID {
		t.Fatalf("erased files = %v", f.files.erased)
	}
	unused, _ := f.store.Unused(context.Background(), impWorkspace, impUser)
	if len(unused) != leadimport.MaxUnusedUploads || f.store.job(latest.ID) == nil {
		t.Fatalf("unused uploads = %d", len(unused))
	}
}

func TestContactPhonesHeldByTooManyLeadsAreNotGuessed(t *testing.T) {
	var leads []*lead.Lead
	for i, name := range []string{"Ana Lima", "Ana Lima", "Rui Lima"} {
		leads = append(leads, &lead.Lead{ID: fmt.Sprintf("lead-shared-%d", i), WorkspaceID: impWorkspace, Name: name, NameSource: lead.SourceManual, Version: 1,
			Phones: []lead.ContactPhone{{Number: "551133330000", Label: lead.PhoneLandline}}})
	}
	leads = append(leads, &lead.Lead{ID: "lead-bia", WorkspaceID: impWorkspace, Name: "Bia Reis", NameSource: lead.SourceManual, Version: 1,
		Phones: []lead.ContactPhone{{Number: "551144440000", Label: lead.PhoneLandline}}})
	f := newImportFixture(t, allImportPermissions(), leads...)
	f.base.holderCap = 2
	job := f.upload(t, "nome;fone casa;email\nAna Lima;1133330000;ana@x.com\nBia Reis;1144440000;bia@x.com\n")
	got := f.dryRun(t, job, suggested(job))
	if got.DryRun.Rejected != 1 || got.DryRun.Enriched != 1 || got.DryRun.Issues[string(lead.ReasonContactAmbiguous)] != 1 {
		t.Fatalf("dry run = %+v", got.DryRun)
	}
	if !reflect.DeepEqual(f.base.loaded, [][]string{{"551144440000"}}) {
		t.Fatalf("holders loaded for %v, want only the phone few leads hold, once", f.base.loaded)
	}
	done := f.start(t, got)
	if done.Status != leadimport.StatusDone || f.base.leads["lead-bia"].Email != "bia@x.com" || len(f.base.leads) != 4 {
		t.Fatalf("job = %+v, leads = %d", done.Result, len(f.base.leads))
	}
}

func TestAnImportLeftByTheRemovedSynchronousRouteIsReportedAsInterrupted(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	settings := leadimport.Settings{Columns: []leadimport.Column{{Index: 0, Field: leadimport.FieldNumber}}, Policy: lead.PolicySkip}
	beat := f.clock
	job := &leadimport.Job{ID: "imp-sync", WorkspaceID: impWorkspace, RequestedBy: impUser, Status: leadimport.StatusImporting, Stage: leadimport.StageRows,
		File: leadimport.File{Name: "rows.json"}, TotalRows: 3, Settings: &settings, Result: &leadimport.Counts{}, Claim: "gone", Attempts: 1,
		HeartbeatAt: &beat, StartedAt: &beat, CreatedAt: beat, UpdatedAt: beat, ExpiresAt: beat.Add(leadimport.Retention)}
	if err := f.store.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(leadimport.StaleAfter + time.Second)
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	if got := f.store.job(job.ID); got.Status != leadimport.StatusFailed || got.FailureCode != leadimport.FailureInterrupted {
		t.Fatalf("job = %+v", got)
	}
}

func TestAStorageErrorWhileReadingTheFileHandsTheImportBack(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, bigFile(1200))
	job = f.dryRun(t, job, suggested(job))
	f.files.failReads = 1
	got := f.start(t, job)
	if got.Status != leadimport.StatusImporting || got.Claim != "" || got.FailureCode != "" {
		t.Fatalf("job = %+v, want it released for the sweeper", got)
	}
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	if got = f.store.job(job.ID); got.Status != leadimport.StatusDone || got.Result.Created != 1200 {
		t.Fatalf("job = %+v", got)
	}
}

func TestListShowsOnlyTheImportersLiveImports(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	first := f.upload(t, schoolFile)
	f.clock = f.clock.Add(time.Minute)
	second := f.upload(t, schoolFile)
	other := &leadimport.Job{ID: "imp-other", WorkspaceID: impWorkspace, RequestedBy: "user-2", Status: leadimport.StatusUploaded,
		CreatedAt: f.clock, ExpiresAt: f.clock.Add(leadimport.Retention)}
	if err := f.store.Create(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	got, err := f.imp.List(context.Background(), importer())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("listed = %+v", got)
	}
	f.clock = f.clock.Add(leadimport.Retention)
	if got, _ := f.imp.List(context.Background(), importer()); len(got) != 1 || got[0].ID != second.ID {
		t.Fatalf("after the first expired: %+v", got)
	}
	if _, err := newImportFixture(t, fakePermissions{"leads:read": true}).imp.List(context.Background(), importer()); !errors.Is(err, leadimport.ErrForbidden) {
		t.Fatalf("list without leads:create = %v", err)
	}
}

func TestDetailTellsWhereTheAddressesLandedOnceTheImportIsDone(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.upload(t, schoolFile)
	analyzed := f.dryRun(t, job, suggested(job))
	detail, err := f.imp.Detail(context.Background(), importer(), job.ID)
	if err != nil || detail.Job.ID != job.ID || detail.Placement != nil || len(f.store.placed) != 0 {
		t.Fatalf("detail before the import = %+v, %v; placements read = %v", detail, err, f.store.placed)
	}
	f.store.placements = map[string]leadimport.Placement{job.ID: {OnMap: 1, Pending: 2}}
	f.start(t, analyzed)
	detail, err = f.imp.Detail(context.Background(), importer(), job.ID)
	if err != nil || detail.Placement == nil || *detail.Placement != (leadimport.Placement{OnMap: 1, Pending: 2}) {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	if !reflect.DeepEqual(f.store.placed, []string{impWorkspace + "/" + job.ID}) {
		t.Fatalf("placements read = %v", f.store.placed)
	}
	if _, err := f.imp.Detail(context.Background(), Actor{UserID: "user-2", WorkspaceID: impWorkspace}, job.ID); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("another member read the detail: %v", err)
	}
}

func TestDetailLeavesThePlacementUnknownForAnImportFromBeforeItWasMeasured(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, schoolFile)
	done := f.store.job(job.ID)
	finished := f.clock
	done.Status, done.Result, done.StartedAt, done.FinishedAt = leadimport.StatusDone, &leadimport.Counts{Rows: 6, Created: 4, AddressesAdded: 1}, &finished, &finished
	f.store.jobs[job.ID] = done
	f.store.placements = map[string]leadimport.Placement{job.ID: {}}
	detail, err := f.imp.Detail(context.Background(), importer(), job.ID)
	if err != nil || detail.Placement != nil || len(f.store.placed) != 0 {
		t.Fatalf("detail = %+v, %v; placements read = %v; want no placement for an import whose addresses were never tagged", detail, err, f.store.placed)
	}
}

func TestTheResultCountsTheRowsThatBroughtNoAddress(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.upload(t, schoolFile)
	analyzed := f.dryRun(t, job, suggested(job))
	if analyzed.DryRun.NoAddress == nil || *analyzed.DryRun.NoAddress != 4 {
		t.Fatalf("dry run = %+v, want the four rows without an address", analyzed.DryRun)
	}
	if done := f.start(t, analyzed); done.Result.NoAddress == nil || *done.Result.NoAddress != 4 {
		t.Fatalf("result = %+v", done.Result)
	}
}

func seededImport(t *testing.T, f *importFixture, rows int) *leadimport.Job {
	t.Helper()
	job := f.upload(t, bigFile(rows))
	s := suggested(job)
	s.SeedInbox = true
	return f.start(t, f.dryRun(t, job, s))
}

func TestSeedingPublishesBatchByBatch(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	got := seededImport(t, f, 1200)
	if got.Status != leadimport.StatusDone || got.Seed == nil || got.Seed.Queued != 1200 || got.Seed.Unconfirmed != 0 || got.Seed.Error != "" {
		t.Fatalf("job = %+v, seed = %+v", got, got.Seed)
	}
	if len(f.seeder.published) != 3 || len(f.seeder.published[0].Targets) != unofficial_whatsapp.SeedBatchSize || len(f.seeder.published[2].Targets) != 200 {
		t.Fatalf("published %d batches", len(f.seeder.published))
	}
}

func TestEverySeedBatchIsRecordedBeforeItIsPublished(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	var stored []leadimport.SeedOutcome
	f.seeder.onPublish = func() {
		f.store.mu.Lock()
		defer f.store.mu.Unlock()
		for _, job := range f.store.jobs {
			if job.Seed != nil {
				stored = append(stored, *job.Seed)
			}
		}
	}
	got := seededImport(t, f, 1200)
	if got.Status != leadimport.StatusDone || len(stored) != 3 {
		t.Fatalf("job = %+v, stored seeds seen = %+v", got, stored)
	}
	for n, seed := range stored {
		if seed.Sending != n+1 || seed.Batches != n {
			t.Fatalf("publishing batch %d with the stored seed %+v; a crash now must leave the batch marked as maybe sent", n+1, seed)
		}
		resumed := seed
		if resumed.Resume(f.seeder.published); resumed.Batches != n+1 || resumed.Unconfirmed != len(f.seeder.published[n].Targets) {
			t.Fatalf("resuming from %+v = %+v, want the batch counted as unconfirmed and never sent again", seed, resumed)
		}
	}
}

func TestAResumedSeedNeverRepublishesABatchItMayHaveSent(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := seededImport(t, f, 1200)
	f.seeder.published = nil
	crashed := f.store.job(job.ID)
	crashed.Status, crashed.Stage, crashed.Claim, crashed.HeartbeatAt, crashed.FinishedAt, crashed.Attempts = leadimport.StatusImporting, leadimport.StageSeed, "", nil, nil, 1
	crashed.Seed = &leadimport.SeedOutcome{Queued: unofficial_whatsapp.SeedBatchSize, Batches: 1, Sending: 2}
	f.store.jobs[job.ID] = crashed
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runs.drain()
	got := f.store.job(job.ID)
	if got.Status != leadimport.StatusDone || got.Seed.Queued != unofficial_whatsapp.SeedBatchSize+200 || got.Seed.Unconfirmed != unofficial_whatsapp.SeedBatchSize {
		t.Fatalf("seed = %+v", got.Seed)
	}
	if len(f.seeder.published) != 1 || len(f.seeder.published[0].Targets) != 200 {
		t.Fatalf("republished %d batches", len(f.seeder.published))
	}
}

func TestAQueueFailureStopsTheSeedAndKeepsWhatWasQueued(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	f.seeder.failAt = 2
	got := seededImport(t, f, 1200)
	if got.Status != leadimport.StatusDone || got.Seed.Error != leadimport.SeedFailed || got.Seed.Queued != unofficial_whatsapp.SeedBatchSize || len(f.seeder.published) != 2 {
		t.Fatalf("seed = %+v, published = %d", got.Seed, len(f.seeder.published))
	}
}

func TestImportMetricsCountRowsAndFinishedImports(t *testing.T) {
	f := newImportFixture(t, allImportPermissions(), storedMaria())
	job := f.upload(t, schoolFile)
	f.start(t, f.dryRun(t, job, suggested(job)))
	want := map[string]int{"created": 4, "enriched": 1, "rejected": 1}
	if !reflect.DeepEqual(f.metrics.rows, want) {
		t.Fatalf("rows = %v, want %v (the dry run counts nothing)", f.metrics.rows, want)
	}
	if f.metrics.runs["done"] != 1 || len(f.metrics.durations) != 1 {
		t.Fatalf("runs = %v, durations = %v", f.metrics.runs, f.metrics.durations)
	}
	failing := newImportFixture(t, allImportPermissions())
	other := failing.upload(t, schoolFile)
	delete(failing.files.files, impWorkspace+"/"+other.File.MediaID)
	if got := failing.dryRun(t, other, suggested(other)); got.FailureCode != leadimport.FailureFileUnavailable || len(failing.metrics.runs) != 0 {
		t.Fatalf("job = %+v, runs = %v, want a failed dry run left out of the import runs", got, failing.metrics.runs)
	}
	lost := newImportFixture(t, allImportPermissions())
	uploaded := lost.upload(t, schoolFile)
	analyzed := lost.dryRun(t, uploaded, suggested(uploaded))
	delete(lost.files.files, impWorkspace+"/"+analyzed.File.MediaID)
	if got := lost.start(t, analyzed); got.FailureCode != leadimport.FailureFileUnavailable || !reflect.DeepEqual(lost.metrics.runs, map[string]int{"file_unavailable": 1}) {
		t.Fatalf("job = %+v, runs = %v", got, lost.metrics.runs)
	}
}

func TestOnlyStalledImportsCountAsStalledRuns(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	uploaded := f.upload(t, schoolFile)
	importing := f.dryRun(t, uploaded, suggested(uploaded))
	if _, err := f.imp.Start(context.Background(), importer(), importing.ID); err != nil {
		t.Fatal(err)
	}
	beat := f.clock
	analyzing := &leadimport.Job{ID: "imp-dry", WorkspaceID: "ws-other", RequestedBy: impUser, Status: leadimport.StatusAnalyzing, Attempts: leadimport.MaxAttempts,
		HeartbeatAt: &beat, CreatedAt: beat, UpdatedAt: beat, ExpiresAt: beat.Add(leadimport.Retention)}
	if err := f.store.Create(context.Background(), analyzing); err != nil {
		t.Fatal(err)
	}
	f.runs.runs = nil
	for i := 0; i < leadimport.MaxAttempts; i++ {
		if _, err := f.store.Claim(context.Background(), importing.ID, fmt.Sprintf("w-%d", i), f.clock); err != nil {
			t.Fatal(err)
		}
		f.clock = f.clock.Add(leadimport.StaleAfter + time.Second)
	}
	if err := f.imp.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{importing.ID, analyzing.ID} {
		if got := f.store.job(id); got.Status != leadimport.StatusFailed || got.FailureCode != leadimport.FailureStalled {
			t.Fatalf("job %s = %+v", id, got)
		}
	}
	if !reflect.DeepEqual(f.metrics.runs, map[string]int{"stalled": 1}) {
		t.Fatalf("runs = %v, want only the stalled import, never the stalled dry run", f.metrics.runs)
	}
}
