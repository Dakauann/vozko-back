package smartcampaignhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const projectionPolicy = `{"preset":"standard","firstWaveSize":200,"minWaveSize":50,"maxWaveSize":5000,"growth":2,"shrink":0.5,
"observeWaitMinutes":120,"maxObserveWaitMinutes":300,"maxWavesPerDay":0,"maxSlowWavesAtFloor":3,"minSample":50,
"failureRateSlow":0.08,"failureRateStop":0.15,"engagementRateSlow":0.03,"onTemplateYellow":"pause",
"sendHours":{"timezone":"America/Sao_Paulo","days":{"fri":[{"start":"08:00","end":"20:00"}],"sat":[{"start":"08:00","end":"20:00"}]}}}`

func project(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	(&Handler{}).Projection(rec, httptest.NewRequest(http.MethodPost, "/whatsapp/campaign-waves/projection", strings.NewReader(body)))
	return rec
}

func TestTheProjectionListsEachWaveAndWhenItCanGoOut(t *testing.T) {
	rec := project(t, `{"contacts":1000,"startAt":"2027-10-15T14:00:00-03:00","policy":`+projectionPolicy+`}`)

	var body ProjectionResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(body.Waves) != 3 || body.Truncated {
		t.Fatalf("waves %+v", body.Waves)
	}
	first, _ := time.Parse(time.RFC3339, "2027-10-15T14:00:00-03:00")
	if body.Waves[0].Number != 1 || body.Waves[0].Size != 200 || !body.Waves[0].At.Equal(first) || body.Waves[2].Size != 400 {
		t.Fatalf("waves %+v", body.Waves)
	}
}

func TestTheProjectionRefusesAnInvalidPolicyOrNoContacts(t *testing.T) {
	if rec := project(t, `{"contacts":1000,"policy":{"preset":"standard"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy: status %d", rec.Code)
	}
	if rec := project(t, `{"contacts":0,"policy":`+projectionPolicy+`}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no contacts: status %d", rec.Code)
	}
}
