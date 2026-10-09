package mediagenhttp

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
	"vozko/domain/mediagen"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	aichat_usecase "vozko/usecases/aichat"
)

var stamp = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type fakeService struct {
	models      []mediagen.Model
	modelsErr   error
	requested   mediagen.Request
	requestedBy string
	requestErr  error
	getErr      error
	job         *mediagen.Job
	gotScope    string
	modelsKind  mediagen.Kind
}

func (s *fakeService) Request(_ context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error) {
	s.requested, s.requestedBy = req, requestedBy
	if s.requestErr != nil {
		return nil, s.requestErr
	}
	return s.job, nil
}

func (s *fakeService) Get(_ context.Context, workspaceID, _ string) (*mediagen.Job, error) {
	s.gotScope = workspaceID
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.job, nil
}

func (s *fakeService) Models(_ context.Context, kind mediagen.Kind) ([]mediagen.Model, error) {
	s.modelsKind = kind
	return s.models, s.modelsErr
}

func queuedJob() *mediagen.Job {
	return &mediagen.Job{ID: "job-1", WorkspaceID: "ws-1", Kind: mediagen.KindImage, Prompt: "pizza", Aspect: mediagen.AspectSquare, Status: mediagen.StatusQueued, CreatedAt: stamp, UpdatedAt: stamp}
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
	rec := serve(t, svc, http.MethodPost, "/media/generations", `{"kind":"image","model":"openai/gpt-image-2","prompt":"pizza","aspect":"square"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(svc.requested, mediagen.Request{WorkspaceID: "ws-1", Kind: mediagen.KindImage, Model: "openai/gpt-image-2", Prompt: "pizza", Aspect: mediagen.AspectSquare}) || svc.requestedBy != "u-1" {
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
	rec := serve(t, svc, http.MethodPost, "/media/generations", `{"kind":"image","prompt":"pizza","aspect":"square","referenceMediaIds":["m-2","m-1"]}`)
	if rec.Code != http.StatusAccepted || !reflect.DeepEqual(svc.requested.ReferenceMediaIDs, []string{"m-2", "m-1"}) {
		t.Fatalf("status %d requested %+v", rec.Code, svc.requested)
	}
	var got JobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !reflect.DeepEqual(got.ReferenceMediaIDs, []string{"m-2", "m-1"}) {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAnUnusableReferenceIsAFieldError(t *testing.T) {
	issue := &mediagen.ValidationError{Issues: []mediagen.FieldIssue{{Field: mediagen.FieldReferences, Code: mediagen.CodeNotImage}}}
	rec := serve(t, &fakeService{requestErr: issue}, http.MethodPost, "/media/generations", `{"kind":"image","prompt":"pizza","aspect":"square","referenceMediaIds":["clip"]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"referenceMediaIds":"not_image"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAFinishedJobCarriesItsMedia(t *testing.T) {
	job := queuedJob()
	job.Status, job.MediaID, job.MediaURL, job.Model = mediagen.StatusDone, "m-1", "https://cdn/x.jpg", "model-x"
	svc := &fakeService{job: job}
	rec := serve(t, svc, http.MethodGet, "/media/generations/job-1", "")
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
	job.Status, job.FailureCode = mediagen.StatusFailed, mediagen.FailureGeneration
	rec := serve(t, &fakeService{job: job}, http.MethodGet, "/media/generations/job-1", "")
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
		{"validation", &mediagen.ValidationError{Issues: []mediagen.FieldIssue{{Field: "prompt", Code: "required"}}}, http.StatusUnprocessableEntity, "invalid_request"},
		{"no balance", aichat_usecase.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance"},
		{"no subscription", aichat_usecase.ErrNoSubscription, http.StatusPaymentRequired, "no_subscription"},
		{"ledger balance", balance.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance"},
		{"no requester", mediagen.ErrRequesterRequired, http.StatusUnauthorized, "unauthenticated"},
		{"race lost", mediagen.ErrDuplicateActiveJob, http.StatusConflict, "already_generating"},
		{"anything else", errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		rec := serve(t, &fakeService{requestErr: c.err}, http.MethodPost, "/media/generations", `{"kind":"image","prompt":"x","aspect":"square"}`)
		var body response.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != c.status || body.Code != c.code {
			t.Fatalf("%s: status %d code %q", c.name, rec.Code, body.Code)
		}
	}
}

func TestValidationNamesEveryField(t *testing.T) {
	invalid := &mediagen.ValidationError{Issues: []mediagen.FieldIssue{{Field: "prompt", Code: "too_long"}, {Field: "aspect", Code: "unknown"}}}
	rec := serve(t, &fakeService{requestErr: invalid}, http.MethodPost, "/media/generations", `{"kind":"image","prompt":"x","aspect":"wide"}`)
	var body response.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Expected["prompt"] != "too_long" || body.Expected["aspect"] != "unknown" {
		t.Fatalf("expected %v", body.Expected)
	}
}

func TestAnUnknownJobIsNotFound(t *testing.T) {
	rec := serve(t, &fakeService{getErr: mediagen.ErrJobNotFound}, http.MethodGet, "/media/generations/job-9", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestAMalformedBodyIsRejected(t *testing.T) {
	svc := &fakeService{job: queuedJob()}
	rec := serve(t, svc, http.MethodPost, "/media/generations", `{"kind":"image","prompt":`)
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
		{http.MethodPost, "/media/generations", "media:create"},
		{http.MethodGet, "/media/generations/job-1", "media:read"},
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
	if router.Match(httptest.NewRequest(http.MethodPost, "/media/generations", nil), &match) {
		t.Fatal("route registered without a handler")
	}
}

func TestImageModelsAreListedInCatalogOrder(t *testing.T) {
	svc := &fakeService{models: []mediagen.Model{{ID: "openai/gpt-image-2.5-sunburst", Name: "GPT Image 2.5 Sunburst"}, {ID: "google/gemini-3-pro-image", Name: "Gemini 3 Pro Image"}}}
	rec := serve(t, svc, http.MethodGet, "/media/models?kind=image", "")
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
	for _, err := range []error{mediagen.ErrModelsUnavailable, mediagen.ErrNoModels} {
		rec := serve(t, &fakeService{modelsErr: err}, http.MethodGet, "/media/models?kind=image", "")
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "models_unavailable") {
			t.Fatalf("%v: status %d body %s", err, rec.Code, rec.Body)
		}
	}
}

func TestAVideoIsRequestedWithItsTimeline(t *testing.T) {
	svc := &fakeService{job: queuedJob()}
	body := `{"kind":"video","aspect":"story","video":{"durationMs":4500,"background":"#000000",` +
		`"visual":[{"clips":[{"mediaId":"img-1","startMs":0,"durationMs":4500,"fit":"cover","transform":{"x":0.5,"y":0.5,"w":1,"h":1,"opacity":1}}]}],` +
		`"audio":[{"clips":[{"mediaId":"song","startMs":0,"durationMs":4500,"volume":0.25,"fadeOutMs":1500}]}]}}`
	rec := serve(t, svc, http.MethodPost, "/media/generations", body)
	video := svc.requested.Video
	if rec.Code != http.StatusAccepted || svc.requested.Kind != mediagen.KindVideo || video.DurationMS != 4_500 || video.Visual[0].Clips[0].Fit != mediagen.FitCover ||
		video.Audio[0].Clips[0].Volume != 0.25 || video.Audio[0].Clips[0].FadeOutMS != 1_500 {
		t.Fatalf("status %d requested %+v", rec.Code, svc.requested)
	}
}

func TestASettlingJobDoesNotHandOutItsMedia(t *testing.T) {
	job := queuedJob()
	job.Kind, job.Status, job.MediaID, job.MediaURL = mediagen.KindMusic, mediagen.StatusSettling, "m-1", "https://cdn/x.m4a"
	rec := serve(t, &fakeService{job: job}, http.MethodGet, "/media/generations/job-1", "")
	var got JobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Status != "settling" || got.MediaID != "" || got.MediaURL != "" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestModelsAreListedForTheAskedKindWithTheDefaultMarked(t *testing.T) {
	svc := &fakeService{models: []mediagen.Model{{ID: "google/lyria-3-pro-preview"}, {ID: "google/lyria-3-clip-preview"}}}
	rec := serve(t, svc, http.MethodGet, "/media/models?kind=music", "")
	var got []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || svc.modelsKind != mediagen.KindMusic || len(got) != 2 || !got[0].Default || got[1].Default {
		t.Fatalf("got %+v kind %s err %v", got, svc.modelsKind, err)
	}
}

func TestAProcessingJobTakesItsSourceAndTheCapIsTooManyRequests(t *testing.T) {
	svc := &fakeService{job: queuedJob()}
	rec := serve(t, svc, http.MethodPost, "/media/generations", `{"kind":"cutout","sourceMediaId":"m-1"}`)
	if rec.Code != http.StatusAccepted || svc.requested.Kind != mediagen.KindCutout || svc.requested.SourceMediaID != "m-1" {
		t.Fatalf("status %d requested %+v", rec.Code, svc.requested)
	}
	capped := serve(t, &fakeService{requestErr: mediagen.ErrTooManyActive}, http.MethodPost, "/media/generations", `{"kind":"cutout","sourceMediaId":"m-1"}`)
	if capped.Code != http.StatusTooManyRequests || !strings.Contains(capped.Body.String(), "too_many_jobs") {
		t.Fatalf("status %d body %s", capped.Code, capped.Body)
	}
}
