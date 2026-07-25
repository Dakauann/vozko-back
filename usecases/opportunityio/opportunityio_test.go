package opportunityio

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
	opportunity_usecase "vozko/usecases/opportunity"
)

// --- in-memory fakes implementing the domain ports the usecase needs ---

type fakeOppRepo struct {
	store map[string]*opportunity.Opportunity
}

func newFakeOppRepo() *fakeOppRepo {
	return &fakeOppRepo{store: map[string]*opportunity.Opportunity{}}
}

func (r *fakeOppRepo) Create(o *opportunity.Opportunity) error {
	cp := *o
	r.store[o.ID] = &cp
	return nil
}
func (r *fakeOppRepo) Update(o *opportunity.Opportunity) error { return nil }
func (r *fakeOppRepo) Delete(workspaceID, id string) error     { return nil }
func (r *fakeOppRepo) GetByID(workspaceID, id string) (*opportunity.Opportunity, error) {
	o, ok := r.store[id]
	if !ok || o.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("not found")
	}
	cp := *o
	return &cp, nil
}
func (r *fakeOppRepo) ListByPipeline(workspaceID, pipelineID string) ([]*opportunity.Opportunity, error) {
	var out []*opportunity.Opportunity
	for _, o := range r.store {
		if o.WorkspaceID == workspaceID && o.PipelineID == pipelineID {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeOppRepo) ListByPipelineScoped(workspaceID, pipelineID string, _ []string, _ bool, _ string) ([]*opportunity.Opportunity, error) {
	return r.ListByPipeline(workspaceID, pipelineID)
}

// SearchByFilter / SumValueByFilter back the deal board/list; the io tests don't
// exercise the compiled predicate, so a minimal workspace-scoped version suffices.
func (r *fakeOppRepo) SearchByFilter(input opportunity.SearchByFilterInput) ([]*opportunity.Opportunity, int64, error) {
	var out []*opportunity.Opportunity
	for _, o := range r.store {
		if o.WorkspaceID == input.WorkspaceID {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeOppRepo) SumValueByFilter(input opportunity.SearchByFilterInput) (int64, error) {
	var sum int64
	for _, o := range r.store {
		if o.WorkspaceID == input.WorkspaceID {
			sum += o.ValueCents
		}
	}
	return sum, nil
}

type fakeFieldRepo struct {
	defs []*customfield.Definition
}

func (r *fakeFieldRepo) Create(d *customfield.Definition) error { return nil }
func (r *fakeFieldRepo) Update(d *customfield.Definition) error { return nil }
func (r *fakeFieldRepo) Delete(workspaceID, id string) error    { return nil }
func (r *fakeFieldRepo) GetByID(workspaceID, id string) (*customfield.Definition, error) {
	return nil, fmt.Errorf("not found")
}
func (r *fakeFieldRepo) ListByObject(workspaceID, objectType string) ([]*customfield.Definition, error) {
	return r.defs, nil
}

func newFields() *fakeFieldRepo {
	return &fakeFieldRepo{defs: []*customfield.Definition{
		{WorkspaceID: "ws1", ObjectType: "opportunity", Key: "segmento", Label: "Segmento",
			Type: customfield.TypeSelect, Options: []string{"enterprise", "smb"}, Required: false, Position: 0},
		{WorkspaceID: "ws1", ObjectType: "opportunity", Key: "score", Label: "Score",
			Type: customfield.TypeNumber, Position: 1},
	}}
}

// newIO builds the io Service backed by a real opportunity usecase (with fakes),
// so tests exercise the true creation + validation path.
func newIO() (*Service, *fakeOppRepo) {
	oppRepo := newFakeOppRepo()
	fields := newFields()
	oppSvc := opportunity_usecase.NewService(oppRepo, nil, fields)
	return NewService(oppSvc, fields), oppRepo
}

// --- parseMajorToCents ---

func TestParseMajorToCents(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"4900.00", 490000, false},
		{"4900", 490000, false},
		{"0", 0, false},
		{"", 0, false},
		{"19.99", 1999, false},
		{"19.9", 1990, false},
		{"19.999", 2000, false}, // rounds half-up
		{"-12.34", -1234, false},
		{"1,234.56", 123456, false}, // US thousands
		{"1.234,56", 123456, false}, // BR thousands
		{"1234,5", 123450, false},   // comma decimal
		{"abc", 0, true},
		{"12.3x", 0, true},
	}
	for _, tc := range cases {
		got, err := parseMajorToCents(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseMajorToCents(%q): expected error, got %d", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMajorToCents(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMajorToCents(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// --- Export format ---

func TestExport_Format(t *testing.T) {
	io, repo := newIO()
	// Seed one opportunity through the usecase so custom fields are typed correctly.
	svc := opportunity_usecase.NewService(repo, nil, newFields())
	if _, err := svc.Create("ws1", opportunity_usecase.CreateInput{
		PipelineID:   "pipe1",
		StageID:      "stage1",
		Title:        "Big Deal",
		ValueCents:   490000,
		CustomFields: map[string]any{"segmento": "enterprise", "score": float64(87)},
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	var buf bytes.Buffer
	n, err := io.Export("ws1", "pipe1", nil, false, "", &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 exported row, got %d", n)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + 1 row, got %d records", len(records))
	}

	header := records[0]
	wantPrefix := []string{"id", "title", "value", "currency", "status", "stage_id", "owner_id", "lead_id", "source", "close_date", "created_at"}
	for i, col := range wantPrefix {
		if header[i] != col {
			t.Fatalf("header[%d] = %q, want %q", i, header[i], col)
		}
	}
	if got := strings.Join(header[len(wantPrefix):], ","); got != "custom_field:segmento,custom_field:score" {
		t.Fatalf("custom field columns = %q", got)
	}

	row := records[1]
	col := func(name string) string {
		for i, h := range header {
			if h == name {
				return row[i]
			}
		}
		return ""
	}
	if col("title") != "Big Deal" {
		t.Errorf("title = %q", col("title"))
	}
	if col("value") != "4900.00" {
		t.Errorf("value = %q, want 4900.00", col("value"))
	}
	if col("currency") != "BRL" {
		t.Errorf("currency = %q, want BRL", col("currency"))
	}
	if col("status") != "open" {
		t.Errorf("status = %q, want open", col("status"))
	}
	if col("custom_field:segmento") != "enterprise" {
		t.Errorf("segmento = %q", col("custom_field:segmento"))
	}
	if col("custom_field:score") != "87" {
		t.Errorf("score = %q, want 87", col("custom_field:score"))
	}
}

func TestExport_RequiresPipeline(t *testing.T) {
	io, _ := newIO()
	var buf bytes.Buffer
	if _, err := io.Export("ws1", "  ", nil, false, "", &buf); err != ErrPipelineRequired {
		t.Fatalf("expected ErrPipelineRequired, got %v", err)
	}
}

// --- Import: valid + invalid rows ---

func TestImport_ValidAndInvalid(t *testing.T) {
	io, repo := newIO()

	csvData := strings.Join([]string{
		"title,value,currency,stage_id,pipeline_id,status",
		"Good Deal,4900.00,BRL,stage1,pipe1,open",   // valid -> created (row 2)
		"No Stage,100.00,BRL,,pipe1,open",           // missing stage -> ErrStageRequired (row 3)
		",,BRL,stage1,pipe1,open",                   // no title and no lead -> ErrTitleOrLead (row 4)
		"Bad Value,not-money,BRL,stage1,pipe1,open", // unparseable value (row 5)
	}, "\n")

	report, err := io.Import("ws1", strings.NewReader(csvData), ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Total != 4 {
		t.Fatalf("total = %d, want 4", report.Total)
	}
	if report.Created != 1 {
		t.Fatalf("created = %d, want 1", report.Created)
	}
	if report.Skipped != 3 {
		t.Fatalf("skipped = %d, want 3", report.Skipped)
	}
	if len(report.Errors) != 3 {
		t.Fatalf("errors = %d, want 3: %+v", len(report.Errors), report.Errors)
	}
	// Every rejected row must be reported with its 1-based line number.
	wantRows := map[int]bool{3: true, 4: true, 5: true}
	for _, e := range report.Errors {
		if !wantRows[e.Row] {
			t.Errorf("unexpected error row %d (%s)", e.Row, e.Message)
		}
	}
	// The one valid row must be persisted.
	if len(repo.store) != 1 {
		t.Fatalf("expected 1 persisted opportunity, got %d", len(repo.store))
	}
	for _, o := range repo.store {
		if o.ValueCents != 490000 {
			t.Errorf("persisted value = %d, want 490000", o.ValueCents)
		}
	}
}

// --- Import: custom fields typed + rejected ---

func TestImport_CustomFields(t *testing.T) {
	io, repo := newIO()

	csvData := strings.Join([]string{
		"title,stage_id,pipeline_id,custom_field:segmento,custom_field:score",
		"Typed,stage1,pipe1,enterprise,87",        // valid select + number
		"BadOption,stage1,pipe1,startup,10",       // segmento not in options (row 3)
		"BadNumber,stage1,pipe1,smb,not-a-number", // score not a number (row 4)
		"Unknown,stage1,pipe1,,",                  // no custom fields -> valid (row 5)
	}, "\n")

	report, err := io.Import("ws1", strings.NewReader(csvData), ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Created != 2 {
		t.Fatalf("created = %d, want 2 (rows 2 and 5): %+v", report.Created, report.Errors)
	}
	if report.Skipped != 2 {
		t.Fatalf("skipped = %d, want 2", report.Skipped)
	}

	// The typed row must carry a string select and a float64 number.
	var typed *opportunity.Opportunity
	for _, o := range repo.store {
		if o.Title == "Typed" {
			typed = o
		}
	}
	if typed == nil {
		t.Fatal("typed opportunity not persisted")
	}
	if got, ok := typed.CustomFields["segmento"].(string); !ok || got != "enterprise" {
		t.Errorf("segmento = %#v, want string enterprise", typed.CustomFields["segmento"])
	}
	if got, ok := typed.CustomFields["score"].(float64); !ok || got != 87 {
		t.Errorf("score = %#v, want float64 87", typed.CustomFields["score"])
	}
}

// --- Import: dry run mutates nothing ---

func TestImport_DryRun(t *testing.T) {
	io, repo := newIO()

	csvData := strings.Join([]string{
		"title,stage_id,pipeline_id",
		"Would Create,stage1,pipe1", // valid
		"No Stage,,pipe1",           // invalid
	}, "\n")

	report, err := io.Import("ws1", strings.NewReader(csvData), ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Import dry-run: %v", err)
	}
	if report.Total != 2 || report.Created != 1 || report.Skipped != 1 {
		t.Fatalf("dry-run report = %+v, want total 2 / created 1 / skipped 1", report)
	}
	if len(repo.store) != 0 {
		t.Fatalf("dry-run must not persist; got %d rows", len(repo.store))
	}
}

// --- Import: default pipeline fallback + round-trip with export ---

func TestImport_RoundTripWithDefaultPipeline(t *testing.T) {
	// Seed and export from one workspace.
	src, srcRepo := newIO()
	seed := opportunity_usecase.NewService(srcRepo, nil, newFields())
	for i, title := range []string{"A", "B"} {
		if _, err := seed.Create("ws1", opportunity_usecase.CreateInput{
			PipelineID:   "pipe1",
			StageID:      "stage1",
			Title:        title,
			ValueCents:   int64((i + 1) * 10000),
			CustomFields: map[string]any{"segmento": "smb", "score": float64(i)},
		}); err != nil {
			t.Fatalf("seed %s: %v", title, err)
		}
	}
	var buf bytes.Buffer
	if _, err := src.Export("ws1", "pipe1", nil, false, "", &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Re-import into a fresh workspace. The export omits pipeline_id, so a default
	// must be supplied.
	dst, dstRepo := newIO()
	report, err := dst.Import("ws2", bytes.NewReader(buf.Bytes()), ImportOptions{DefaultPipelineID: "pipe1"})
	if err != nil {
		t.Fatalf("Import round-trip: %v", err)
	}
	if report.Created != 2 || report.Skipped != 0 {
		t.Fatalf("round-trip report = %+v, want created 2 / skipped 0", report)
	}
	if len(dstRepo.store) != 2 {
		t.Fatalf("expected 2 re-imported opportunities, got %d", len(dstRepo.store))
	}
	for _, o := range dstRepo.store {
		if o.WorkspaceID != "ws2" {
			t.Errorf("workspace = %q, want ws2", o.WorkspaceID)
		}
		if _, ok := o.CustomFields["segmento"]; !ok {
			t.Errorf("segmento lost in round-trip for %q", o.Title)
		}
	}
}

// --- Import: over-cap is reported, never silently truncated ---

func TestImport_OverCap(t *testing.T) {
	io, repo := newIO()

	var b strings.Builder
	b.WriteString("title,stage_id,pipeline_id\n")
	total := MaxImportRows + 5
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, "Deal %d,stage1,pipe1\n", i)
	}

	report, err := io.Import("ws1", strings.NewReader(b.String()), ImportOptions{})
	if err != nil {
		t.Fatalf("Import over-cap: %v", err)
	}
	if !report.Truncated {
		t.Fatal("expected Truncated=true for over-cap file")
	}
	if report.Total != MaxImportRows {
		t.Fatalf("total = %d, want cap %d", report.Total, MaxImportRows)
	}
	if report.Created != MaxImportRows {
		t.Fatalf("created = %d, want %d", report.Created, MaxImportRows)
	}
	if len(repo.store) != MaxImportRows {
		t.Fatalf("persisted = %d, want cap %d", len(repo.store), MaxImportRows)
	}
}

// --- Import: empty input yields an empty report, not an error ---

func TestImport_Empty(t *testing.T) {
	io, _ := newIO()
	report, err := io.Import("ws1", strings.NewReader(""), ImportOptions{})
	if err != nil {
		t.Fatalf("Import empty: %v", err)
	}
	if report.Total != 0 || report.Created != 0 || len(report.Errors) != 0 {
		t.Fatalf("empty report = %+v", report)
	}
}
