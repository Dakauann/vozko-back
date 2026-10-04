package imagegenhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	"vozko/domain/balance"
	"vozko/domain/imagegen"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	aichat_usecase "vozko/usecases/aichat"
)

var stamp = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type fakeService struct {
	models      []imagegen.Model
	modelsErr   error
	requested   imagegen.Request
	requestedBy string
	requestErr  error
	getErr      error
	job         *imagegen.Job
	gotScope    string
}

func (s *fakeService) Request(_ context.Context, req imagegen.Request, requestedBy string) (*imagegen.Job, error) {
	s.requested, s.requestedBy = req, requestedBy
	if s.requestErr != nil {
		return nil, s.requestErr
	}
	return s.job, nil
}

func (s *fakeService) Get(_ context.Context, workspaceID, _ string) (*imagegen.Job, error) {
	s.gotScope = workspaceID
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.job, nil
}

func (s *fakeService) Models(context.Context) ([]imagegen.Model, error) {
	return s.models, s.modelsErr
}

func queuedJob() *imagegen.Job {
	return &imagegen.Job{ID: "job-1", WorkspaceID: "ws-1", Prompt: "pizza", Aspect: imagegen.AspectSquare, Status: imagegen.StatusQueued, CreatedAt: stamp, UpdatedAt: stamp}
}

func serve(t *testing.T, svc *fakeService, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	allow := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return next
	}
	RegisterProtectedRoutes(router, NewHandler(svc), allow)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestRequestingAnImageAnswersAcceptedWithTheJob(t *testing.T) {
	svc := &fakeService{job: queuedJob()}
	rec := serve(t, svc, http.MethodPost, "/images/generations", `{"model":"openai/gpt-image-2","prompt":"pizza","aspect":"square"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(svc.requested, imagegen.Request{WorkspaceID: "ws-1", Model: "openai/gpt-image-2", Prompt: "pizza", Aspect: imagegen.AspectSquare}) || svc.requestedBy != "u-1" {
		t.Fatalf("requested %+v by %q", svc.requested, svc.requestedBy)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "job-1" || got["status"] != "queued" || got["aspect"] != "square" || got["createdAt"] == nil {
		t.Fatalf("body %v", got)
	}
	if refs, ok := got["referenceMediaIds"].([]any); !ok || len(refs) != 0 {
		t.Fatalf("a job without references must list none, got %v", got["referenceMediaIds"])
	}
	for _, absent := range []string{"mediaId", "mediaUrl", "model", "failureCode", "workspaceId", "requestedBy", "fingerprint"} {
		if _, ok := got[absent]; ok {
			t.Fatalf("%s present in %v", absent, got)
		}
	}
}

func TestReferenceImagesAreRequestedAndReturned(t *testing.T) {
	job := queuedJob()
	job.ReferenceMediaIDs = []string{"m-2", "m-1"}
	svc := &fakeService{job: job}
	rec := serve(t, svc, http.MethodPost, "/images/generations", `{"prompt":"pizza","aspect":"square","referenceMediaIds":["m-2","m-1"]}`)
	if rec.Code != http.StatusAccepted || !reflect.DeepEqual(svc.requested.ReferenceMediaIDs, []string{"m-2", "m-1"}) {
		t.Fatalf("status %d requested %+v", rec.Code, svc.requested)
	}
	var got JobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !reflect.DeepEqual(got.ReferenceMediaIDs, []string{"m-2", "m-1"}) {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAnUnusableReferenceIsAFieldError(t *testing.T) {
	issue := &imagegen.ValidationError{Issues: []imagegen.FieldIssue{{Field: imagegen.FieldReferences, Code: imagegen.CodeNotImage}}}
	rec := serve(t, &fakeService{requestErr: issue}, http.MethodPost, "/images/generations", `{"prompt":"pizza","aspect":"square","referenceMediaIds":["clip"]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"referenceMediaIds":"not_image"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAFinishedJobCarriesItsMedia(t *testing.T) {
	job := queuedJob()
	job.Status, job.MediaID, job.MediaURL, job.Model = imagegen.StatusDone, "m-1", "https://cdn/x.jpg", "model-x"
	svc := &fakeService{job: job}
	rec := serve(t, svc, http.MethodGet, "/images/generations/job-1", "")
	var got JobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d err %v", rec.Code, err)
	}
	if got.MediaID != "m-1" || got.MediaURL != "https://cdn/x.jpg" || got.Model != "model-x" || svc.gotScope != "ws-1" {
		t.Fatalf("got %+v scope %q", got, svc.gotScope)
	}
}

func TestAFailedJobCarriesItsFailureCode(t *testing.T) {
	job := queuedJob()
	job.Status, job.FailureCode = imagegen.StatusFailed, imagegen.FailureGeneration
	rec := serve(t, &fakeService{job: job}, http.MethodGet, "/images/generations/job-1", "")
	var got JobResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "failed" || got.FailureCode != "generation_failed" {
		t.Fatalf("got %+v", got)
	}
}

func TestErrorsMapToStableStatuses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", &imagegen.ValidationError{Issues: []imagegen.FieldIssue{{Field: "prompt", Code: "required"}}}, http.StatusUnprocessableEntity, "invalid_request"},
		{"no balance", aichat_usecase.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance"},
		{"no subscription", aichat_usecase.ErrNoSubscription, http.StatusPaymentRequired, "no_subscription"},
		{"ledger balance", balance.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance"},
		{"no requester", imagegen.ErrRequesterRequired, http.StatusUnauthorized, "unauthenticated"},
		{"race lost", imagegen.ErrDuplicateActiveJob, http.StatusConflict, "already_generating"},
		{"anything else", errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		rec := serve(t, &fakeService{requestErr: c.err}, http.MethodPost, "/images/generations", `{"prompt":"x","aspect":"square"}`)
		var body response.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != c.status || body.Code != c.code {
			t.Fatalf("%s: status %d code %q", c.name, rec.Code, body.Code)
		}
	}
}

func TestValidationNamesEveryField(t *testing.T) {
	invalid := &imagegen.ValidationError{Issues: []imagegen.FieldIssue{{Field: "prompt", Code: "too_long"}, {Field: "aspect", Code: "unknown"}}}
	rec := serve(t, &fakeService{requestErr: invalid}, http.MethodPost, "/images/generations", `{"prompt":"x","aspect":"wide"}`)
	var body response.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Expected["prompt"] != "too_long" || body.Expected["aspect"] != "unknown" {
		t.Fatalf("expected %v", body.Expected)
	}
}

func TestAnUnknownJobIsNotFound(t *testing.T) {
	rec := serve(t, &fakeService{getErr: imagegen.ErrJobNotFound}, http.MethodGet, "/images/generations/job-9", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestAMalformedBodyIsRejected(t *testing.T) {
	svc := &fakeService{job: queuedJob()}
	rec := serve(t, svc, http.MethodPost, "/images/generations", `{"prompt":`)
	if rec.Code != http.StatusBadRequest || svc.requestedBy != "" {
		t.Fatalf("status %d", rec.Code)
	}
}

type recordingAC struct{ calls map[string]string }

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, _ http.HandlerFunc) http.HandlerFunc {
	return func(_ http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
	}
}

func TestRoutesAreGuardedByTheMediaResource(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)
	for _, c := range []struct{ method, path, want string }{
		{http.MethodPost, "/images/generations", "media:create"},
		{http.MethodGet, "/images/generations/job-1", "media:read"},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Fatalf("%s %s not registered", c.method, c.path)
		}
		match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Fatalf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestNoRoutesWithoutAHandler(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, nil, (&recordingAC{calls: map[string]string{}}).fn)
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodPost, "/images/generations", nil), &match) {
		t.Fatal("route registered without a handler")
	}
}

func TestImageModelsAreListedInCatalogOrder(t *testing.T) {
	svc := &fakeService{models: []imagegen.Model{{ID: "openai/gpt-image-2.5-sunburst", Name: "GPT Image 2.5 Sunburst"}, {ID: "google/gemini-3-pro-image", Name: "Gemini 3 Pro Image"}}}
	rec := serve(t, svc, http.MethodGet, "/images/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var got []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "openai/gpt-image-2.5-sunburst" || got[1].Name != "Gemini 3 Pro Image" {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestAnUnavailableModelListIsAServiceError(t *testing.T) {
	for _, err := range []error{imagegen.ErrModelsUnavailable, imagegen.ErrNoImageModels} {
		rec := serve(t, &fakeService{modelsErr: err}, http.MethodGet, "/images/models", "")
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "models_unavailable") {
			t.Fatalf("%v: status %d body %s", err, rec.Code, rec.Body)
		}
	}
}
