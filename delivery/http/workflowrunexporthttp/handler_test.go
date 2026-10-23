package workflowrunexporthttp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
	"vozko/pkg/codegolden"
	workflow_usecase "vozko/usecases/workflow"
)

type stubExports struct {
	doc        *workflow.RunExport
	err        error
	docs       []workflow.RunExport
	streamErr  error
	afterStart bool
	viewer     shared.Person
	scope      workflow.Scope
	runID      string
	workflowID string
	filter     workflow.RunExportFilter
}

func (s *stubExports) Run(viewer shared.Person, scope workflow.Scope, runID string) (*workflow.RunExport, error) {
	s.viewer, s.scope, s.runID = viewer, scope, runID
	return s.doc, s.err
}

func (s *stubExports) Stream(_ context.Context, viewer shared.Person, scope workflow.Scope, workflowID string, filter workflow.RunExportFilter, sink workflow_usecase.RunExportSink) error {
	s.viewer, s.scope, s.workflowID, s.filter = viewer, scope, workflowID, filter
	if s.streamErr != nil && !s.afterStart {
		return s.streamErr
	}
	if err := sink.Start("Matrícula: 2º semestre/2026"); err != nil {
		return err
	}
	for _, doc := range s.docs {
		if err := sink.Write(doc); err != nil {
			return err
		}
	}
	if err := sink.Flush(); err != nil {
		return err
	}
	return s.streamErr
}

func request(path string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = mux.SetURLVars(r, vars)
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	return r.WithContext(context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"}))
}

func exportDoc(runID string) workflow.RunExport {
	return workflow.ExportOfRun("Matrícula: 2º semestre/2026", workflow.RunDetail{Run: &workflow.WorkflowRun{ID: runID, WorkflowID: "wf-1", Status: workflow.RunStatusCompleted, State: workflow.RunState{Vars: map[string]interface{}{"token": "••••"}}}}, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
}

func TestOneRunIsDownloadedAsAJSONAttachmentWithASafeName(t *testing.T) {
	doc := exportDoc("3f1c-run")
	exports := &stubExports{doc: &doc}
	rec := httptest.NewRecorder()
	NewHandler(exports).ExportRun(rec, request("/workflow-runs/3f1c-run/export", map[string]string{"runId": "3f1c-run"}))

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="execucao-Matr-cula-2-semestre-2026-3f1c-run.json"` {
		t.Fatalf("disposition = %q", got)
	}
	var decoded workflow.RunExport
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Run.ID != "3f1c-run" || decoded.SchemaVersion != workflow.RunExportSchemaVersion || decoded.FinalState["token"] != "••••" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if exports.viewer.UserID != "user-1" || exports.scope.WorkspaceID != "ws-1" || exports.runID != "3f1c-run" {
		t.Fatalf("asked as %+v in %+v for %q", exports.viewer, exports.scope, exports.runID)
	}
}

func TestOneRunExportAnswersTheDetailRefusals(t *testing.T) {
	cases := []struct {
		err  error
		code int
		want string
	}{
		{workflow.ErrRunNotFound, http.StatusNotFound, "run_not_found"},
		{workflow.ErrWorkflowAccessDenied, http.StatusForbidden, "workflow_access_denied"},
		{errors.New("db down"), http.StatusInternalServerError, refusalRunExportFailed.Code},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		NewHandler(&stubExports{err: tc.err}).ExportRun(rec, request("/workflow-runs/r/export", map[string]string{"runId": "r"}))
		if rec.Code != tc.code || responsetest.CodeOf(t, rec) != tc.want {
			t.Fatalf("%v: status %d body %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}

func TestManyRunsAreStreamedAsOneJSONDocumentPerLine(t *testing.T) {
	exports := &stubExports{docs: []workflow.RunExport{exportDoc("run-2"), exportDoc("run-1")}}
	rec := httptest.NewRecorder()
	path := "/workflows/wf-1/runs/export?from=2026-10-01T03:00:00Z&to=2026-10-08T03:00:00Z&status=error&entryId=e-1"
	NewHandler(exports).ExportRuns(rec, request(path, map[string]string{"id": "wf-1"}))

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("status %d type %q body %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="execucoes-Matr-cula-2-semestre-2026-2026-10-01-2026-10-08.ndjson"` {
		t.Fatalf("disposition = %q", got)
	}
	if !rec.Flushed {
		t.Fatal("pages must be flushed as they are written")
	}
	var ids []string
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for scanner.Scan() {
		var doc workflow.RunExport
		if err := json.Unmarshal(scanner.Bytes(), &doc); err != nil {
			t.Fatalf("line %q: %v", scanner.Text(), err)
		}
		ids = append(ids, doc.Run.ID)
	}
	if strings.Join(ids, ",") != "run-2,run-1" {
		t.Fatalf("lines = %v", ids)
	}
	f := exports.filter
	if exports.workflowID != "wf-1" || f.Runs.EntryID != "e-1" || f.Runs.Status == nil || *f.Runs.Status != workflow.RunStatusError ||
		!f.From.Equal(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)) || !f.To.Equal(time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("filter = %+v", f)
	}
}

func TestAManyRunsExportWithAnUnreadablePeriodIsRefusedBeforeTheUseCase(t *testing.T) {
	for _, query := range []string{"?to=2026-10-08T03:00:00Z", "?from=ontem&to=2026-10-08T03:00:00Z", "?from=2026-10-01T03:00:00Z&to=2026-10-08"} {
		exports := &stubExports{}
		rec := httptest.NewRecorder()
		NewHandler(exports).ExportRuns(rec, request("/workflows/wf-1/runs/export"+query, map[string]string{"id": "wf-1"}))
		if rec.Code != http.StatusBadRequest || responsetest.CodeOf(t, rec) != "export_period_invalid" || exports.workflowID != "" {
			t.Fatalf("%s: status %d body %s", query, rec.Code, rec.Body.String())
		}
	}
}

func TestAManyRunsExportRefusedBeforeItStartsAnswersItsCode(t *testing.T) {
	cases := []struct {
		err  error
		code int
		want string
	}{
		{workflow.ErrRunExportTooLarge, http.StatusUnprocessableEntity, "export_too_large"},
		{workflow.ErrRunExportFilterInvalid, http.StatusBadRequest, "export_period_invalid"},
		{workflow.ErrWorkflowNotFound, http.StatusNotFound, "workflow_not_found"},
		{errors.New("db down"), http.StatusInternalServerError, refusalRunExportFailed.Code},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		NewHandler(&stubExports{streamErr: tc.err}).ExportRuns(rec, request("/workflows/wf-1/runs/export?from=2026-10-01T03:00:00Z&to=2026-10-08T03:00:00Z", map[string]string{"id": "wf-1"}))
		if rec.Code != tc.code || responsetest.CodeOf(t, rec) != tc.want || rec.Header().Get("Content-Disposition") != "" {
			t.Fatalf("%v: status %d body %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}

func TestATooLargeExportSaysTheCapsSoTheScreenCanTellHowToNarrow(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(&stubExports{streamErr: workflow.ErrRunExportTooLarge}).ExportRuns(rec, request("/workflows/wf-1/runs/export?from=2026-10-01T03:00:00Z&to=2026-10-08T03:00:00Z", map[string]string{"id": "wf-1"}))
	var body struct {
		Details exportTooLargeDetails `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Details.MaxRuns != workflow.MaxRunsPerExport || body.Details.MaxDays != 31 {
		t.Fatalf("details = %+v", body.Details)
	}
}

func TestAnExportThatBreaksAfterItStartedAbortsInsteadOfEndingAsAComplete(t *testing.T) {
	exports := &stubExports{docs: []workflow.RunExport{exportDoc("run-1")}, streamErr: errors.New("db down"), afterStart: true}
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("recovered %v, the response must be aborted so the download fails", recovered)
		}
	}()
	NewHandler(exports).ExportRuns(httptest.NewRecorder(), request("/workflows/wf-1/runs/export?from=2026-10-01T03:00:00Z&to=2026-10-08T03:00:00Z", map[string]string{"id": "wf-1"}))
}

func TestAnExportWhoseClientLeftEndsQuietly(t *testing.T) {
	exports := &stubExports{docs: []workflow.RunExport{exportDoc("run-1")}, streamErr: context.Canceled, afterStart: true}
	r := request("/workflows/wf-1/runs/export?from=2026-10-01T03:00:00Z&to=2026-10-08T03:00:00Z", map[string]string{"id": "wf-1"})
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	NewHandler(exports).ExportRuns(httptest.NewRecorder(), r.WithContext(ctx))
}

func TestExportsNeedAnAuthenticatedViewer(t *testing.T) {
	exports := &stubExports{}
	rec := httptest.NewRecorder()
	r := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/workflow-runs/r/export", nil), map[string]string{"runId": "r"})
	NewHandler(exports).ExportRun(rec, r)
	if rec.Code != http.StatusUnauthorized || exports.runID != "" {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestEveryExportErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, exportRefusals, writeExportRefusal)
	responsetest.RequireStableCodes(t, []response.Refusal{refusalRunExportFailed, refusalInvalidPeriod})
}

func TestExportErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := response.RefusalCodes(response.ErrorRefusalsOf(exportRefusals), []response.Refusal{refusalRunExportFailed, refusalInvalidPeriod})
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, "update testdata/error_codes.golden, and also add the code to vozko-front src/lib/workflows/error-codes.ts and translate workflowsPage.errors.<code> in the pt, en, es and de catalogs")
}
