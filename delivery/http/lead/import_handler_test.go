package lead

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/unofficial_whatsapp"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	lead_usecase "vozko/usecases/lead"
)

const importJobID = "7a1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a02"

type stubImports struct {
	err       error
	job       *leadimport.Job
	upload    lead_usecase.UploadInput
	settings  leadimport.Settings
	placement *leadimport.Placement
	listed    []leadimport.Job
	asked     lead_usecase.Actor
	issues    []leadimport.IssueRow
	afters    []int64
	catalog   lead_usecase.ImportCatalog
	startedID string
}

func (s *stubImports) reply() (*leadimport.Job, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.job, nil
}

func (s *stubImports) Upload(_ context.Context, a lead_usecase.Actor, in lead_usecase.UploadInput) (*leadimport.Job, error) {
	s.asked, s.upload = a, in
	return s.reply()
}

func (s *stubImports) Detail(_ context.Context, a lead_usecase.Actor, _ string) (lead_usecase.ImportDetail, error) {
	s.asked = a
	job, err := s.reply()
	if err != nil {
		return lead_usecase.ImportDetail{}, err
	}
	return lead_usecase.ImportDetail{Job: job, Placement: s.placement}, nil
}

func (s *stubImports) List(_ context.Context, a lead_usecase.Actor) ([]leadimport.Job, error) {
	s.asked = a
	return s.listed, s.err
}

func (s *stubImports) Catalog(context.Context, lead_usecase.Actor) (lead_usecase.ImportCatalog, error) {
	return s.catalog, nil
}

func (s *stubImports) DryRun(_ context.Context, _ lead_usecase.Actor, _ string, settings leadimport.Settings) (*leadimport.Job, error) {
	s.settings = settings
	return s.reply()
}

func (s *stubImports) Start(_ context.Context, _ lead_usecase.Actor, id string) (*leadimport.Job, error) {
	s.startedID = id
	return s.reply()
}

func (s *stubImports) Rejections(_ context.Context, _ lead_usecase.Actor, _ string, after int64, limit int) ([]leadimport.IssueRow, error) {
	s.afters = append(s.afters, after)
	var out []leadimport.IssueRow
	for _, row := range s.issues {
		if row.Seq > after && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func importRouter(imports Imports) http.Handler {
	h := NewLeadHandler(HandlerDeps{Imports: imports})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	RegisterImportRoutes(router, h, passThrough, openLimiter{})
	return router
}

type openLimiter struct{}

func (openLimiter) Validate(next http.Handler) http.Handler { return next }

type countingLimiter struct{ seen *[]string }

func (c countingLimiter) Validate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*c.seen = append(*c.seen, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusTooManyRequests)
	})
}

func TestUploadRoutesGoThroughTheUploadLimiter(t *testing.T) {
	var limited []string
	router := mux.NewRouter()
	h := NewLeadHandler(HandlerDeps{})
	RegisterRoutes(router, h, passThrough)
	RegisterImportRoutes(router, h, passThrough, countingLimiter{seen: &limited})
	for _, call := range []struct {
		method, path string
		limited      bool
	}{
		{http.MethodPost, "/leads/imports", true},
		{http.MethodGet, "/leads/imports", false},
		{http.MethodGet, "/leads/imports/" + importJobID, false},
		{http.MethodPost, "/leads/imports/" + importJobID + "/start", false},
	} {
		rec := send(t, router, call.method, call.path, nil, nil)
		if got := rec.Code == http.StatusTooManyRequests; got != call.limited {
			t.Errorf("%s %s limited = %v, want %v", call.method, call.path, got, call.limited)
		}
	}
	if len(limited) != 1 {
		t.Fatalf("limited = %v", limited)
	}
}

func sampleJob() *leadimport.Job {
	at := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	return &leadimport.Job{
		ID: importJobID, Status: leadimport.StatusAnalyzed, File: leadimport.File{Name: "escola.csv", SizeBytes: 120}, TotalRows: 2,
		Preview: leadimport.Preview{Headers: []string{"telefone", "bairro"}, Sample: [][]string{{"11987654321", "Centro"}},
			Columns: []leadimport.Column{{Index: 0, Header: "telefone", Field: leadimport.FieldNumber}, {Index: 1, Header: "bairro", Field: leadimport.FieldDistrict}}},
		Settings:  &leadimport.Settings{Columns: []leadimport.Column{{Index: 1, Field: leadimport.FieldDistrict}}, Policy: leaddomain.PolicySkip},
		DryRun:    &leadimport.Counts{Rows: 2, Created: 1, Rejected: 1, Issues: map[string]int{"invalid": 1}},
		CreatedAt: at, ExpiresAt: at.Add(leadimport.Retention),
	}
}

func TestImportRoutesAreGatedByLeadCreation(t *testing.T) {
	gates := map[string]gatedRoute{}
	router := mux.NewRouter()
	RegisterImportRoutes(router, NewLeadHandler(HandlerDeps{}), func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	}, openLimiter{})
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/leads/imports"}, {http.MethodGet, "/leads/imports/" + importJobID}, {http.MethodPost, "/leads/imports/" + importJobID + "/dry-run"},
		{http.MethodPost, "/leads/imports/" + importJobID + "/start"}, {http.MethodGet, "/leads/imports/" + importJobID + "/rejections"}, {http.MethodGet, "/leads/imports"},
	} {
		send(t, router, call.method, call.path, nil, nil)
		path := strings.Replace(call.path, importJobID, leadIDPath[1:], 1)
		if got := gates[call.method+" "+path]; got != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionCreate}) {
			t.Errorf("%s %s is gated by %+v", call.method, call.path, got)
		}
	}
}

func TestImportRoutesAnswer503WithoutTheUseCase(t *testing.T) {
	rec := send(t, importRouter(nil), http.MethodGet, "/leads/imports/"+importJobID, nil, nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "lead_import_unavailable") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func multipartUpload(t *testing.T, name string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", name)
	_, _ = part.Write(data)
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/leads/imports", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	return req.WithContext(ctx)
}

func TestCreateImportTakesAnUploadOrAMediaID(t *testing.T) {
	imports := &stubImports{job: sampleJob(), catalog: lead_usecase.ImportCatalog{
		Fields:    []lead_usecase.ImportField{{Field: leadimport.Field{Key: leadimport.FieldDistrict, Group: leadimport.GroupAddress, Requires: workspace_domain.ActionReadAddresses}, Allowed: false}},
		FillEmpty: true,
	}}
	rec := httptest.NewRecorder()
	importRouter(imports).ServeHTTP(rec, multipartUpload(t, "escola.csv", []byte("telefone\n1\n")))
	if rec.Code != http.StatusCreated || string(imports.upload.Data) != "telefone\n1\n" || imports.upload.FileName != "escola.csv" || imports.asked.UserID != "user-1" {
		t.Fatalf("status = %d, upload = %+v", rec.Code, imports.upload)
	}
	var out LeadImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != importJobID || out.Status != "analyzed" || len(out.Preview.Columns) != 2 || out.Preview.Columns[1].Field != leadimport.FieldDistrict {
		t.Fatalf("response = %+v", out)
	}
	if len(out.Fields) != 1 || out.Fields[0].Requires != "leads:read_addresses" || out.Fields[0].Allowed || !out.Options.FillEmpty {
		t.Fatalf("fields = %+v, options = %+v", out.Fields, out.Options)
	}
	if out.Settings == nil || out.Settings.Columns[0].Header != "bairro" || out.DryRun == nil || out.DryRun.Issues["invalid"] != 1 {
		t.Fatalf("settings = %+v, dry run = %+v", out.Settings, out.DryRun)
	}

	rec = send(t, importRouter(imports), http.MethodPost, "/leads/imports", nil, map[string]any{"mediaId": "m-1"})
	if rec.Code != http.StatusCreated || imports.upload.MediaID != "m-1" || imports.upload.Data != nil {
		t.Fatalf("media id: status = %d, upload = %+v", rec.Code, imports.upload)
	}
	for _, body := range []map[string]any{{}, {"mediaId": "m-1", "rows": []any{}}} {
		if rec := send(t, importRouter(imports), http.MethodPost, "/leads/imports", nil, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("body %v: status = %d", body, rec.Code)
		}
	}
}

func TestCreateImportRefusesAFileAboveTheCap(t *testing.T) {
	imports := &stubImports{job: sampleJob()}
	rec := httptest.NewRecorder()
	importRouter(imports).ServeHTTP(rec, multipartUpload(t, "big.csv", make([]byte, leadimport.MaxFileBytes+1)))
	if rec.Code != http.StatusRequestEntityTooLarge || imports.upload.Data != nil {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestImportErrorsMapToTheirStatusAndCode(t *testing.T) {
	cases := []struct {
		err      error
		status   int
		code     string
		expected map[string]string
	}{
		{&leadimport.PermissionError{Action: workspace_domain.ActionAssign}, http.StatusForbidden, "lead_import_forbidden", map[string]string{"permission": "leads:assign"}},
		{&leadimport.PermissionError{Resource: workspace_domain.ResourceMedia, Action: workspace_domain.ActionRead}, http.StatusForbidden, "lead_import_forbidden", map[string]string{"permission": "media:read"}},
		{&leadimport.MappingError{Column: 3, Field: "cpf", Rule: leadimport.RuleUnknownField}, http.StatusBadRequest, "lead_import_mapping_invalid", map[string]string{"column": "3", "field": "cpf", "rule": "unknown_field"}},
		{leadimport.ErrNotFound, http.StatusNotFound, "lead_import_not_found", nil},
		{leadimport.ErrFileUnavailable, http.StatusNotFound, "lead_import_file_unavailable", nil},
		{leadimport.ErrRunning, http.StatusConflict, "lead_import_running", nil},
		{leadimport.ErrNotReady, http.StatusConflict, "lead_import_not_ready", nil},
		{leadimport.ErrTooManyRows, http.StatusRequestEntityTooLarge, "lead_import_too_many_rows", nil},
		{leadimport.ErrUnsupportedFile, http.StatusBadRequest, "lead_import_unsupported_file", nil},
		{errors.New("database down"), http.StatusInternalServerError, "", nil},
	}
	for _, tc := range cases {
		rec := send(t, importRouter(&stubImports{err: tc.err}), http.MethodPost, "/leads/imports/"+importJobID+"/start", nil, nil)
		var body struct {
			Code     string            `json:"code"`
			Expected map[string]string `json:"expected"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tc.status || body.Code != tc.code {
			t.Errorf("%v: status = %d code = %q, want %d %q", tc.err, rec.Code, body.Code, tc.status, tc.code)
		}
		for k, v := range tc.expected {
			if body.Expected[k] != v {
				t.Errorf("%v: expected[%s] = %q, want %q", tc.err, k, body.Expected[k], v)
			}
		}
	}
}

func TestDryRunPassesTheMappingAndRefusesUnknownKeys(t *testing.T) {
	imports := &stubImports{job: sampleJob()}
	rec := send(t, importRouter(imports), http.MethodPost, "/leads/imports/"+importJobID+"/dry-run", nil, map[string]any{
		"columns": []map[string]any{{"index": 0, "field": "number"}, {"index": 3, "field": "custom_field:interesse"}}, "onExisting": "skip", "seedInbox": true,
		"seedConversations": map[string]any{"bodies": []string{"Oi {{1}}"}, "maxMessages": 4},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	s := imports.settings
	if len(s.Columns) != 2 || s.Columns[1].Index != 3 || s.Columns[1].Field != "custom_field:interesse" || s.Policy != leaddomain.PolicySkip || !s.SeedInbox || s.Script == nil {
		t.Fatalf("settings = %+v", s)
	}
	if rec := send(t, importRouter(imports), http.MethodPost, "/leads/imports/"+importJobID+"/dry-run", nil, map[string]any{"columns": []any{}, "mapping": map[string]any{}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown key: status = %d", rec.Code)
	}
}

func TestStartNamesTheImport(t *testing.T) {
	imports := &stubImports{job: sampleJob()}
	if rec := send(t, importRouter(imports), http.MethodPost, "/leads/imports/"+importJobID+"/start", nil, nil); rec.Code != http.StatusAccepted || imports.startedID != importJobID {
		t.Fatalf("status = %d, started = %q", rec.Code, imports.startedID)
	}
}

func TestRejectionsDownloadAsALocalizedCSVPageByPage(t *testing.T) {
	imports := &stubImports{job: sampleJob()}
	for i := 0; i < rejectionsPage+2; i++ {
		imports.issues = append(imports.issues, leadimport.IssueRow{Seq: int64(i + 1), ImportIssue: leaddomain.ImportIssue{Line: i + 2, Reason: leaddomain.ReasonEmailInvalid, Field: leaddomain.ImportFieldEmail}})
	}
	imports.issues[0].Rejected, imports.issues[0].Reason, imports.issues[0].Field = true, leaddomain.ReasonDuplicate, leaddomain.ImportFieldNumber
	rec := send(t, importRouter(imports), http.MethodGet, "/leads/imports/"+importJobID+"/rejections?locale=en", nil, nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("status = %d, headers = %v", rec.Code, rec.Header())
	}
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "\xEF\xBB\xBF")), "\n")
	if len(lines) != rejectionsPage+3 || lines[0] != "line;reason;code;column;row imported" {
		t.Fatalf("%d lines, header %q", len(lines), lines[0])
	}
	if lines[1] != "2;Number repeated in the file;duplicate;number;no" || !strings.HasSuffix(strings.TrimSpace(lines[2]), ";email_invalid;email;yes") {
		t.Fatalf("rows = %q / %q", lines[1], lines[2])
	}
	if len(imports.afters) != 2 || imports.afters[1] != rejectionsPage {
		t.Fatalf("pages asked after %v", imports.afters)
	}
}

func TestTheSynchronousImportRouteIsGone(t *testing.T) {
	imports := &stubImports{job: sampleJob()}
	rec := send(t, importRouter(imports), http.MethodPost, "/leads/import", nil, map[string]any{"rows": []map[string]any{{"line": 2, "number": "11987654321"}}})
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /leads/import = %d, want it unrouted", rec.Code)
	}
	if imports.asked.UserID != "" {
		t.Fatal("the removed route reached the import use case")
	}
}

func TestListImportsAnswersTheImportersImportsAndTheCaps(t *testing.T) {
	done := *sampleJob()
	noAddress := 1
	done.Status, done.Result = leadimport.StatusDone, &leadimport.Counts{Rows: 2, Created: 1, AddressesFilled: 3, NoAddress: &noAddress}
	done.Seed = &leadimport.SeedOutcome{Queued: 1, Unconfirmed: 500, Batches: 2, Sending: 2}
	imports := &stubImports{listed: []leadimport.Job{done}}
	rec := send(t, importRouter(imports), http.MethodGet, "/leads/imports?mine=true", nil, nil)
	if rec.Code != http.StatusOK || imports.asked.UserID == "" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out LeadImportListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != importJobID || out.Items[0].Status != "done" || out.Items[0].Result.NoAddress == nil || *out.Items[0].Result.NoAddress != 1 || out.Items[0].Result.AddressesFilled != 3 || out.Items[0].Seed.Unconfirmed != 500 {
		t.Fatalf("items = %+v", out.Items)
	}
	want := LeadImportLimitsResponse{MaxBytes: leadimport.MaxFileBytes, MaxMegabytes: 20, MaxRows: leadimport.MaxRows, MaxSeededConversations: 200, RetentionDays: 7, MaxUnusedUploads: leadimport.MaxUnusedUploads}
	if out.Limits != want {
		t.Fatalf("limits = %+v, want %+v", out.Limits, want)
	}
	if strings.Contains(rec.Body.String(), "sending") || strings.Contains(rec.Body.String(), "preview") {
		t.Fatalf("the list leaks the seed bookkeeping or the sample rows: %s", rec.Body)
	}
	empty := send(t, importRouter(&stubImports{}), http.MethodGet, "/leads/imports", nil, nil)
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"items":[]`) {
		t.Fatalf("empty list = %d %s", empty.Code, empty.Body)
	}
	for _, query := range []string{"?mine", "?mine=", "?mine=true"} {
		if rec := send(t, importRouter(imports), http.MethodGet, "/leads/imports"+query, nil, nil); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200", query, rec.Code)
		}
	}
	for _, query := range []string{"?mine=false", "?mine=true&mine=true", "?mine=1"} {
		if rec := send(t, importRouter(imports), http.MethodGet, "/leads/imports"+query, nil, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400: only your own imports are listed", query, rec.Code)
		}
	}
	if rec := send(t, importRouter(&stubImports{err: &leadimport.PermissionError{Action: workspace_domain.ActionCreate}}), http.MethodGet, "/leads/imports", nil, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("refusal = %d", rec.Code)
	}
}

func TestCountsLeaveOutTheAddressCountOfAnImportThatNeverMeasuredIt(t *testing.T) {
	job := sampleJob()
	job.Status, job.Result = leadimport.StatusDone, &leadimport.Counts{Rows: 2, Created: 1}
	rec := send(t, importRouter(&stubImports{job: job}), http.MethodGet, "/leads/imports/"+importJobID, nil, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "noAddress") || !strings.Contains(rec.Body.String(), `"addressesFilled":0`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	measured := 0
	job.Result.NoAddress = &measured
	rec = send(t, importRouter(&stubImports{job: job}), http.MethodGet, "/leads/imports/"+importJobID, nil, nil)
	if !strings.Contains(rec.Body.String(), `"noAddress":0`) {
		t.Fatalf("a measured zero is sent: %s", rec.Body)
	}
}

func TestAnUnexpectedImportErrorIsLoggedWithTheImportAndTheWorkspace(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	defer slog.SetDefault(previous)
	rec := send(t, importRouter(&stubImports{err: errors.New("database down")}), http.MethodPost, "/leads/imports/"+importJobID+"/start", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var line map[string]any
	if err := json.Unmarshal(logged.Bytes(), &line); err != nil {
		t.Fatalf("log = %q: %v", logged.String(), err)
	}
	if line["import_id"] != importJobID || line["workspace_id"] != "ws-1" || line["error"] != "database down" {
		t.Fatalf("log = %v", line)
	}
	logged.Reset()
	send(t, importRouter(&stubImports{err: errors.New("database down")}), http.MethodGet, "/leads/imports", nil, nil)
	if !strings.Contains(logged.String(), `"workspace_id":"ws-1"`) || strings.Contains(logged.String(), "import_id") {
		t.Fatalf("list log = %s", logged.String())
	}
}

func TestGetImportCarriesThePlacementAndTheStoredScript(t *testing.T) {
	job := sampleJob()
	job.Status, job.Result = leadimport.StatusDone, &leadimport.Counts{Rows: 2, Created: 1, AddressesAdded: 1}
	job.Settings.SeedInbox = true
	job.Settings.Script = &unofficial_whatsapp.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4, Context: "Matrículas",
		Attachment: &unofficial_whatsapp.SeedAttachment{MediaID: "m-9", Kind: unofficial_whatsapp.MediaKind("image")}}
	imports := &stubImports{job: job, placement: &leadimport.Placement{OnMap: 3088, Approximate: 1920, Pending: 709, NotFound: 3}}
	rec := send(t, importRouter(imports), http.MethodGet, "/leads/imports/"+importJobID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out LeadImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Placement == nil || *out.Placement != (LeadImportPlacementResponse{OnMap: 3088, Approximate: 1920, Pending: 709, NotFound: 3}) {
		t.Fatalf("placement = %+v", out.Placement)
	}
	script := out.Settings.SeedScript
	if !out.Settings.SeedConversations || script == nil || script.Bodies[0] != "Oi {{1}}" || script.MaxMessages != 4 || script.Context != "Matrículas" ||
		script.Attachment == nil || script.Attachment.MediaID != "m-9" || script.Attachment.Kind != "image" {
		t.Fatalf("settings = %+v, script = %+v", out.Settings, script)
	}
	imports.placement = nil
	imports.job.Status = leadimport.StatusImporting
	rec = send(t, importRouter(imports), http.MethodGet, "/leads/imports/"+importJobID, nil, nil)
	if strings.Contains(rec.Body.String(), `"placement"`) {
		t.Fatalf("a running import answered a placement: %s", rec.Body)
	}
}
