package smartcampaignhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	"vozko/domain/working_hours"
	wd "vozko/domain/workspace/workspace_department"
	smartcampaign_usecase "vozko/usecases/smartcampaign"
)

type ownedCampaigns map[string]*wc.Campaign

func (o ownedCampaigns) Owned(workspaceID string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	c, ok := o[id]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, wc.ErrCampaignNotFound
	}
	return c, nil
}

type overviewStub struct{ view smartcampaign_usecase.View }

func (o overviewStub) Overview(string) (*smartcampaign_usecase.View, error) { return &o.view, nil }

type settingsStub struct {
	saved   *sc.Policy
	removed bool
	err     error
}

func (s *settingsStub) Save(_ *wc.Campaign, p sc.Policy) error {
	if s.err != nil {
		return s.err
	}
	s.saved = &p
	return nil
}

func (s *settingsStub) Remove(*wc.Campaign) error {
	s.removed = true
	return s.err
}

func (s *settingsStub) Defaults(context.Context, *wc.Campaign) (smartcampaign_usecase.Defaults, error) {
	hours := &working_hours.Spec{Timezone: "America/Sao_Paulo", Days: map[string][]working_hours.Window{"mon": {{Start: "08:00", End: "20:00"}}}}
	return smartcampaign_usecase.Defaults{SendHours: hours, Presets: map[sc.Preset]sc.Policy{sc.Standard: sc.PresetPolicy(sc.Standard, hours)}}, nil
}

func serve(t *testing.T, h *Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/whatsapp/campaigns/{id}/smart", h.Get).Methods(http.MethodGet)
	router.HandleFunc("/whatsapp/campaigns/{id}/smart", h.Save).Methods(http.MethodPut)
	router.HandleFunc("/whatsapp/campaigns/{id}/smart", h.Remove).Methods(http.MethodDelete)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Workspace-ID", "ws")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func newHandler(settings *settingsStub, view smartcampaign_usecase.View, status campaign.Status) *Handler {
	return NewHandler(ownedCampaigns{"c1": {ID: "c1", WorkspaceID: "ws", Status: status}}, overviewStub{view: view}, settings)
}

func TestGetShowsThePolicyInMinutesAndWhatHappensNext(t *testing.T) {
	policy := sc.PresetPolicy(sc.Standard, nil)
	view := smartcampaign_usecase.View{Policy: &policy, Progress: sc.Progress{Phase: sc.PhaseObserving, Wave: 2}}
	rec := serve(t, newHandler(&settingsStub{}, view, campaign.StatusRunning), http.MethodGet, "/whatsapp/campaigns/c1/smart", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got Response
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Policy == nil || got.Policy.ObserveWaitMinutes != 120 || got.Policy.MaxObserveWaitMinutes != 300 || got.Editable {
		t.Fatalf("policy = %+v, editable %v, want 120/300 minutes and locked while running", got.Policy, got.Editable)
	}
	if got.Progress.Phase != sc.PhaseObserving || got.Defaults.Presets["standard"].FirstWaveSize != 200 || got.Defaults.SendHours == nil {
		t.Fatalf("response = %+v", got)
	}
}

func TestACampaignOfAnotherWorkspaceIsNotFound(t *testing.T) {
	h := NewHandler(ownedCampaigns{"c1": {ID: "c1", WorkspaceID: "other"}}, overviewStub{}, &settingsStub{})
	if rec := serve(t, h, http.MethodGet, "/whatsapp/campaigns/c1/smart", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestSaveConvertsMinutesAndActions(t *testing.T) {
	settings := &settingsStub{}
	body := `{"preset":"aggressive","firstWaveSize":500,"minWaveSize":100,"maxWaveSize":20000,"growth":3,"shrink":0.5,
		"observeWaitMinutes":60,"maxObserveWaitMinutes":300,"maxWavesPerDay":3,"maxSlowWavesAtFloor":3,"minSample":50,
		"failureRateSlow":0.1,"failureRateStop":0.2,"engagementRateSlow":0.05,"onTemplateYellow":"slow_down",
		"sendHours":{"timezone":"America/Sao_Paulo","days":{"mon":[{"start":"08:00","end":"20:00"}]}}}`

	rec := serve(t, newHandler(settings, smartcampaign_usecase.View{}, campaign.StatusStopped), http.MethodPut, "/whatsapp/campaigns/c1/smart", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	p := settings.saved
	if p == nil || p.Preset != sc.Aggressive || p.ObserveWait.Minutes() != 60 || p.OnTemplateYellow != sc.SlowDown || p.MaxWavesPerDay != 3 || p.SendHours == nil {
		t.Fatalf("saved = %+v", p)
	}
}

func TestSaveRefusesAnUnknownPreset(t *testing.T) {
	settings := &settingsStub{}
	rec := serve(t, newHandler(settings, smartcampaign_usecase.View{}, campaign.StatusStopped), http.MethodPut, "/whatsapp/campaigns/c1/smart", `{"preset":"turbo"}`)
	if rec.Code != http.StatusBadRequest || settings.saved != nil {
		t.Fatalf("status %d, saved %v", rec.Code, settings.saved)
	}
}

func TestSaveMapsTheDomainRefusals(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"invalid policy":   {sc.ErrInvalidPolicy, http.StatusBadRequest},
		"locked while run": {sc.ErrPolicyLocked, http.StatusConflict},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, newHandler(&settingsStub{err: c.err}, smartcampaign_usecase.View{}, campaign.StatusStopped), http.MethodPut, "/whatsapp/campaigns/c1/smart", `{"preset":"standard"}`)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestRemoveTurnsWaveSendingOff(t *testing.T) {
	settings := &settingsStub{}
	rec := serve(t, newHandler(settings, smartcampaign_usecase.View{}, campaign.StatusStopped), http.MethodDelete, "/whatsapp/campaigns/c1/smart", "")
	if rec.Code != http.StatusOK || !settings.removed {
		t.Fatalf("status %d, removed %v", rec.Code, settings.removed)
	}
}

type recordingSettings struct {
	settingsStub
	asked *wc.Campaign
}

func (r *recordingSettings) Defaults(ctx context.Context, target *wc.Campaign) (smartcampaign_usecase.Defaults, error) {
	r.asked = target
	return r.settingsStub.Defaults(ctx, target)
}

func TestDefaultsForANewCampaignUseTheChosenDepartment(t *testing.T) {
	settings := &recordingSettings{}
	h := NewHandler(ownedCampaigns{}, overviewStub{}, settings)
	router := mux.NewRouter()
	router.HandleFunc("/whatsapp/campaign-waves/defaults", h.Defaults).Methods(http.MethodGet)
	req := httptest.NewRequest(http.MethodGet, "/whatsapp/campaign-waves/defaults?departmentId=sales", nil)
	req.Header.Set("X-Workspace-ID", "ws")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	var got DefaultsBody
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || settings.asked == nil || settings.asked.WorkspaceID != "ws" || settings.asked.DepartmentID != "sales" {
		t.Fatalf("status %d, asked %+v", rec.Code, settings.asked)
	}
	if got.SendHours == nil || got.Presets["standard"].FirstWaveSize != 200 {
		t.Fatalf("defaults = %+v", got)
	}
}
