package audience

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	ca "vozko/domain/audience"
)

type captureGetSettings struct{ sources []ca.Source }

func (c *captureGetSettings) Execute(_ context.Context, ws string, source ca.Source, accountID string) (*ca.Settings, error) {
	c.sources = append(c.sources, source)
	s := ca.NewSettings(ws, source, accountID, ca.VerticalServices)
	return &s, nil
}

type captureEstimate struct{ sources []ca.Source }

func (c *captureEstimate) Execute(_ context.Context, _ string, source ca.Source, _, _ string) (*ca.BackfillEstimate, error) {
	c.sources = append(c.sources, source)
	return &ca.BackfillEstimate{}, nil
}

type captureGetContainer struct{ refs []ca.ContainerRef }

func (c *captureGetContainer) Execute(_ context.Context, ws string, ref ca.ContainerRef) (*ca.ContainerSettings, error) {
	c.refs = append(c.refs, ref)
	eff := ca.NewSettings(ws, ref.Source, ref.AccountID, ca.VerticalServices)
	return &ca.ContainerSettings{Effective: eff}, nil
}

func TestAccountRoutesRefuseAnUnknownSource(t *testing.T) {
	settings, estimate, container := &captureGetSettings{}, &captureEstimate{}, &captureGetContainer{}
	h := NewHandler(Deps{GetSet: settings, Estimate: estimate, GetContainer: container})
	router := mux.NewRouter()
	router.HandleFunc("/audience/settings/{source}/{accountId}", h.GetSettings).Methods(http.MethodGet)
	router.HandleFunc("/audience/backfill/{source}/{accountId}/estimate", h.EstimateBackfill).Methods(http.MethodGet)
	router.HandleFunc("/audience/settings/{source}/{accountId}/containers/{containerId}", h.GetContainerSettings).Methods(http.MethodGet)

	for _, path := range []string{
		"/audience/settings/nope/acc-1",
		"/audience/settings/mercadolivre/acc-1",
		"/audience/backfill/nope/acc-1/estimate",
		"/audience/settings/nope/acc-1/containers/m-1",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, withWorkspace(httptest.NewRequest(http.MethodGet, path, nil), "ws-1"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, rec.Code)
		}
	}
	if len(settings.sources)+len(estimate.sources)+len(container.refs) != 0 {
		t.Fatalf("an unknown source must never reach a use case: %v %v %v", settings.sources, estimate.sources, container.refs)
	}

	for _, source := range []ca.Source{ca.SourceInstagram, ca.SourceFacebook} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/settings/"+string(source)+"/acc-1", nil), "ws-1"))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d body=%s", source, rec.Code, rec.Body.String())
		}
	}
	if len(settings.sources) != 2 || settings.sources[0] != ca.SourceInstagram || settings.sources[1] != ca.SourceFacebook {
		t.Fatalf("sources reaching the use case = %v", settings.sources)
	}
}

func TestAMissingSourceIsNeverInstagram(t *testing.T) {
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"accountId": "acc-1"})
	if _, _, err := accountRef(req); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v", err)
	}
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"source": " instagram ", "accountId": " acc-1 "})
	source, account, err := accountRef(req)
	if err != nil || source != ca.SourceInstagram || account != "acc-1" {
		t.Fatalf("source %q account %q err %v", source, account, err)
	}
}
