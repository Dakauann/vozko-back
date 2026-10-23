package googlesheet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	gsuc "vozko/usecases/googlesheet"
)

type stubSheets struct {
	err   error
	sheet *googlesheet.Sheet
}

func (s *stubSheets) ListRows(context.Context, string, string, shared.Pagination) (*shared.PaginatedResult[*googlesheet.Row], error) {
	return &shared.PaginatedResult[*googlesheet.Row]{Items: []*googlesheet.Row{}}, s.err
}

func (s *stubSheets) Create(context.Context, gsuc.CreateInput) (*googlesheet.Sheet, error) {
	return s.sheet, s.err
}
func (s *stubSheets) Get(context.Context, string, string) (*googlesheet.Sheet, error) {
	return s.sheet, s.err
}
func (s *stubSheets) List(context.Context, googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error) {
	return &shared.PaginatedResult[*googlesheet.Sheet]{Items: []*googlesheet.Sheet{s.sheet}, Page: 1, PageSize: 20, TotalItems: 1, TotalPages: 1}, s.err
}
func (s *stubSheets) UpdateDetails(context.Context, gsuc.DetailsInput) (*googlesheet.Sheet, error) {
	return s.sheet, s.err
}
func (s *stubSheets) EditColumns(context.Context, gsuc.ColumnsInput) (*googlesheet.Sheet, error) {
	return s.sheet, s.err
}
func (s *stubSheets) Delete(context.Context, string, string) error { return s.err }
func (s *stubSheets) Health(context.Context, string, string) (*gsuc.HealthView, error) {
	return &gsuc.HealthView{Sheet: s.sheet}, s.err
}
func (s *stubSheets) RetryProblems(context.Context, string, string) (int, error) { return 0, s.err }
func (s *stubSheets) Reactivate(context.Context, gsuc.ReactivateInput) (*googlesheet.Sheet, error) {
	return s.sheet, s.err
}

type stubRows struct {
	err error
	got gsuc.EnqueueInput
}

func (s *stubRows) Enqueue(_ context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error) {
	s.got = in
	if s.err != nil {
		return nil, s.err
	}
	return &gsuc.EnqueueResult{RowID: "row-1"}, nil
}

func serve(h *Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.HandleFunc("/google-sheets", h.Create).Methods(http.MethodPost)
	router.HandleFunc("/google-sheets/{id}", h.Get).Methods(http.MethodGet)
	router.HandleFunc("/google-sheets/{id}/rows", h.AddRow).Methods(http.MethodPost)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	var body struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return body.Code, body.Details
}

func TestAValueRefusalNamesTheColumn(t *testing.T) {
	rows := &stubRows{err: &googlesheet.ValueError{Column: "valor", Header: "Valor", Reason: googlesheet.ErrInvalidValue, Detail: "número \"abc\" não reconhecido"}}
	rec := serve(NewHandler(&stubSheets{}, rows), http.MethodPost, "/google-sheets/s1/rows", `{"values":{"valor":"abc"}}`, map[string]string{"Idempotency-Key": "k1"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	code, details := codeOf(t, rec)
	if code != googlesheet.CodeInvalidValue || details["column"] != "valor" {
		t.Fatalf("code = %s, details = %v", code, details)
	}
}

func TestManualRowsNeedAnIdempotencyKeyAndAreScopedToTheUser(t *testing.T) {
	rows := &stubRows{}
	rec := serve(NewHandler(&stubSheets{}, rows), http.MethodPost, "/google-sheets/s1/rows", `{"values":{"nome":"Ana"}}`, nil)
	if code, _ := codeOf(t, rec); rec.Code != http.StatusBadRequest || code != googlesheet.CodeIdempotencyKeyRequired {
		t.Fatalf("status = %d code = %s", rec.Code, code)
	}
	rec = serve(NewHandler(&stubSheets{}, rows), http.MethodPost, "/google-sheets/s1/rows", `{"values":{"nome":"Ana"}}`, map[string]string{"Idempotency-Key": "k1"})
	if rec.Code != http.StatusAccepted || rows.got.Origin.Kind != googlesheet.OriginManual || !strings.HasPrefix(rows.got.IdempotencyKey, "manual:") {
		t.Fatalf("status = %d, input = %+v", rec.Code, rows.got)
	}
}

func TestCreateWithoutGoogleIsAPreconditionFailure(t *testing.T) {
	rec := serve(NewHandler(&stubSheets{err: googleaccount.ErrNeedsReconnect}, &stubRows{}), http.MethodPost, "/google-sheets", `{"title":"x","columns":[]}`, nil)
	if code, _ := codeOf(t, rec); rec.Code != http.StatusPreconditionFailed || code != googleaccount.CodeNotConnected {
		t.Fatalf("status = %d code = %s", rec.Code, code)
	}
}

func TestGoogleRefusalsKeepTheirFailureKind(t *testing.T) {
	err := &gsuc.GoogleRefusal{Failure: googlesheet.Failure{Kind: googlesheet.FailurePermission, Detail: "no access"}, Err: http.ErrAbortHandler}
	rec := serve(NewHandler(&stubSheets{err: err}, &stubRows{}), http.MethodPost, "/google-sheets", `{"title":"x"}`, nil)
	code, details := codeOf(t, rec)
	if rec.Code != http.StatusBadGateway || code != googlesheet.CodeWriteAccessLost || details["failure"] != "permission" {
		t.Fatalf("status = %d code = %s details = %v", rec.Code, code, details)
	}
}

func TestUnknownErrorsAreInternalWithACode(t *testing.T) {
	rec := serve(NewHandler(&stubSheets{err: context.DeadlineExceeded}, &stubRows{}), http.MethodGet, "/google-sheets/s1", "", nil)
	if code, _ := codeOf(t, rec); rec.Code != http.StatusInternalServerError || code != googlesheet.CodeInternal {
		t.Fatalf("status = %d code = %s", rec.Code, code)
	}
}

func TestEveryDomainCodeCanReachTheFrontend(t *testing.T) {
	listed := map[string]bool{}
	for _, code := range RefusalCodes() {
		listed[code] = true
	}
	for _, code := range googlesheet.Codes() {
		if !listed[code] && !valueCodes[code] {
			t.Errorf("code %s is neither a refusal nor a value refusal; the frontend cannot translate it", code)
		}
	}
}

var valueCodes = map[string]bool{
	googlesheet.CodeUnknownColumn: true, googlesheet.CodeColumnArchived: true, googlesheet.CodeSystemColumn: true,
	googlesheet.CodeRequiredColumnMissing: true, googlesheet.CodeInvalidValue: true, googlesheet.CodeValueNotInOptions: true,
	googlesheet.CodeValueTooLong: true, googlesheet.CodeUnresolvedVariable: true, googlesheet.CodeOriginRequired: true,
	googlesheet.CodeRowNotFound: true,
}
