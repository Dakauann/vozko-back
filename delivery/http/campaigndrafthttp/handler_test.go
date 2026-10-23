package campaigndrafthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	"vozko/domain/campaigndraft"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	campaigndraft_usecase "vozko/usecases/campaigndraft"
	"vozko/usecases/campaignlaunch"
)

type memoryDrafts struct {
	rows map[string]campaigndraft.Draft
}

func (m *memoryDrafts) Create(_ context.Context, d *campaigndraft.Draft) error {
	m.rows[d.ID] = *d
	return nil
}

func (m *memoryDrafts) Update(_ context.Context, d *campaigndraft.Draft) error {
	if _, err := m.Find(context.Background(), d.WorkspaceID, d.Channel, d.ID); err != nil {
		return err
	}
	m.rows[d.ID] = *d
	return nil
}

func (m *memoryDrafts) Find(_ context.Context, workspaceID string, ch campaigndraft.Channel, id string) (*campaigndraft.Draft, error) {
	d, ok := m.rows[id]
	if !ok || d.WorkspaceID != workspaceID || d.Channel != ch {
		return nil, campaigndraft.ErrNotFound
	}
	return &d, nil
}

func (m *memoryDrafts) List(_ context.Context, workspaceID string, ch campaigndraft.Channel, _ int) ([]campaigndraft.Draft, error) {
	var out []campaigndraft.Draft
	for _, d := range m.rows {
		if d.WorkspaceID == workspaceID && d.Channel == ch {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memoryDrafts) Delete(_ context.Context, workspaceID string, ch campaigndraft.Channel, id string) error {
	if _, err := m.Find(context.Background(), workspaceID, ch, id); err != nil {
		return err
	}
	delete(m.rows, id)
	return nil
}

type stubFinisher struct {
	reviewed  []string
	launched  []campaign.Launch
	prints    []string
	refuse    error
	createdOK bool
}

func (s *stubFinisher) Review(w http.ResponseWriter, _ *http.Request, request json.RawMessage) {
	s.reviewed = append(s.reviewed, string(request))
	response.WriteSuccess(w, http.StatusOK, ReviewResponse{Check: CheckOf(launchcheck.Report{})})
}

func (s *stubFinisher) Launch(w http.ResponseWriter, _ *http.Request, _ json.RawMessage, launch campaign.Launch, fingerprint string) bool {
	s.launched = append(s.launched, launch)
	s.prints = append(s.prints, fingerprint)
	if s.refuse != nil {
		WriteLaunchRefusal(w, s.refuse)
		return false
	}
	WriteLaunched(w, launch, campaignlaunch.Outcome[string]{Campaign: "camp-1", Started: launch.StartsNow()}, func(c string) any { return map[string]string{"id": c} }, nil)
	return s.createdOK
}

type permissions struct {
	denied map[workspace_domain.Action]bool
}

func (p permissions) ac(_ workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.denied[action] {
			response.WriteError(w, http.StatusForbidden, "denied", nil)
			return
		}
		next(w, r)
	}
}

type harness struct {
	repo     *memoryDrafts
	finisher *stubFinisher
	router   *mux.Router
}

func newHarness(t *testing.T, denied ...workspace_domain.Action) harness {
	t.Helper()
	repo := &memoryDrafts{rows: map[string]campaigndraft.Draft{}}
	service, err := campaigndraft_usecase.New(repo)
	if err != nil {
		t.Fatal(err)
	}
	finisher := &stubFinisher{createdOK: true}
	router := mux.NewRouter()
	p := permissions{denied: map[workspace_domain.Action]bool{}}
	for _, action := range denied {
		p.denied[action] = true
	}
	RegisterProtectedRoutes(router, NewHandler(service, campaigndraft.ChannelWhatsApp, finisher), p.ac, workspace_domain.ResourceWhatsAppCampaigns)
	return harness{repo: repo, finisher: finisher, router: router}
}

func (h harness) do(t *testing.T, method, path, workspaceID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1", Role: "user"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, workspaceID)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func (h harness) seed(t *testing.T, workspaceID string, payload string) string {
	t.Helper()
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp", workspaceID, map[string]any{"name": "Natal", "payload": json.RawMessage(payload)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var d DraftResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	return d.ID
}

func TestDraftsRoundTripThroughTheRoutes(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, "ws", `{"request":{"name":"Natal"},"state":{"step":"config"}}`)

	rec := h.do(t, http.MethodGet, "/campaign-drafts/whatsapp/"+id, "ws", nil)
	var got DraftResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Name != "Natal" || got.Channel != "whatsapp" || got.CreatedBy != "u1" {
		t.Fatalf("get %d %s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, http.MethodPut, "/campaign-drafts/whatsapp/"+id, "ws", map[string]any{"name": "Natal 2", "payload": json.RawMessage(`{"state":{"step":"audience"}}`)})
	if rec.Code != http.StatusOK {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, http.MethodGet, "/campaign-drafts/whatsapp", "ws", nil)
	var list DraftListResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Items) != 1 || list.Items[0].Name != "Natal 2" {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}

	if rec = h.do(t, http.MethodDelete, "/campaign-drafts/whatsapp/"+id, "ws", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete %d", rec.Code)
	}
	if rec = h.do(t, http.MethodGet, "/campaign-drafts/whatsapp/"+id, "ws", nil); rec.Code != http.StatusNotFound || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_draft_not_found")) {
		t.Fatalf("after delete %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnotherWorkspaceNeverSeesTheDraft(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, "ws", `{}`)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if rec := h.do(t, method, "/campaign-drafts/whatsapp/"+id, "other", nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %d", method, rec.Code)
		}
	}
	rec := h.do(t, http.MethodGet, "/campaign-drafts/whatsapp", "other", nil)
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("list %s", rec.Body.String())
	}
}

func TestAnInvalidPayloadIsCoded(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp", "ws", map[string]any{"name": "x", "payload": []int{1}})
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_draft_invalid")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestReviewNeedsACampaignRequest(t *testing.T) {
	h := newHarness(t)
	empty := h.seed(t, "ws", `{"state":{}}`)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+empty+"/review", "ws", nil)
	if rec.Code != http.StatusUnprocessableEntity || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_draft_incomplete")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	full := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	rec = h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+full+"/review", "ws", nil)
	if rec.Code != http.StatusOK || len(h.finisher.reviewed) != 1 || h.finisher.reviewed[0] != `{"name":"Natal"}` {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), h.finisher.reviewed)
	}
}

func TestLaunchReadyCreatesAndDeletesTheDraft(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+id+"/launch", "ws", map[string]any{"mode": "ready"})
	var body LaunchResponse
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Mode != "ready" || body.Started {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if _, ok := h.repo.rows[id]; ok {
		t.Fatal("the draft survived its campaign")
	}
}

func TestLaunchScheduleCarriesTheTime(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+id+"/launch", "ws", map[string]any{"mode": "schedule", "scheduledStart": at})
	if rec.Code != http.StatusCreated || len(h.finisher.launched) != 1 || !h.finisher.launched[0].At.Equal(at) {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body.String(), h.finisher.launched)
	}
	missing := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	rec = h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+missing+"/launch", "ws", map[string]any{"mode": "schedule"})
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_launch_schedule_missing")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestLaunchRefusesAnUnknownMode(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+id+"/launch", "ws", map[string]any{"mode": "later"})
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_launch_mode_invalid")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(h.finisher.launched) != 0 {
		t.Fatal("an unknown mode reached the channel")
	}
}

func TestLaunchNowNeedsTheStartPermission(t *testing.T) {
	h := newHarness(t, workspace_domain.ActionStart)
	id := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+id+"/launch", "ws", map[string]any{"mode": "now"})
	if rec.Code != http.StatusForbidden || len(h.finisher.launched) != 0 {
		t.Fatalf("%d %v", rec.Code, h.finisher.launched)
	}
	rec = h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+id+"/launch", "ws", map[string]any{"mode": "ready"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("ready without start permission %d", rec.Code)
	}
}

func TestLaunchNowBlockedKeepsTheDraftAndReturnsTheIssues(t *testing.T) {
	h := newHarness(t)
	report := launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodeTemplateNotReady, Level: launchcheck.Block}, {Code: launchcheck.CodeNoExit, Level: launchcheck.Confirm}}}
	h.finisher.refuse = report.Admit("")
	id := h.seed(t, "ws", `{"request":{"name":"Natal"}}`)
	rec := h.do(t, http.MethodPost, "/campaign-drafts/whatsapp/"+id+"/launch", "ws", map[string]any{"mode": "now", "confirmedFingerprint": "abc"})
	var body struct {
		Code    string         `json:"code"`
		Details RefusalDetails `json:"details"`
	}
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != "campaign_launch_blocked" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(body.Details.Blocks) != 1 || body.Details.Blocks[0] != "template_not_ready" || len(body.Details.Confirms) != 1 || body.Details.Fingerprint != report.Fingerprint() {
		t.Fatalf("details %+v", body.Details)
	}
	if h.finisher.prints[0] != "abc" {
		t.Fatalf("fingerprint %v", h.finisher.prints)
	}
	if _, ok := h.repo.rows[id]; !ok {
		t.Fatal("a refused launch deleted the draft")
	}
}

func TestStartRefusalCodes(t *testing.T) {
	blocked := &launchcheck.RefusedError{Report: launchcheck.Blocked(launchcheck.CodeNothingPending, 0)}
	if got := startRefusalCode(blocked, nil); got != "campaign_launch_blocked" {
		t.Fatal(got)
	}
	if got := startRefusalCode(campaign.ErrSettingsNotApplied, nil); got != "campaign_settings_not_applied" {
		t.Fatal(got)
	}
	if got := startRefusalCode(context.Canceled, func(error) string { return "number_restricted" }); got != "number_restricted" {
		t.Fatal(got)
	}
	if got := startRefusalCode(context.Canceled, nil); got != "campaign_start_failed" {
		t.Fatal(got)
	}
}
