package unofficial_whatsapp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/http/middleware"
)

type capturingCreate struct{ got *uwc.Campaign }

func (c *capturingCreate) Execute(_ context.Context, in *uwc.Campaign, _ uw.DepartmentScope) (*uwc.Campaign, error) {
	c.got = in
	return &uwc.Campaign{ID: "c-1", WorkspaceID: in.WorkspaceID}, nil
}

func postCampaign(t *testing.T, role, body string) *uwc.Campaign {
	t.Helper()
	create := &capturingCreate{}
	h := NewCampaignHandler(CampaignHandlerDeps{Create: create, Departments: openScope{}})

	r := httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/campaigns",
		bytes.NewBufferString(body))
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey,
		&auth.Claims{UserID: "u-1", Role: role})

	w := httptest.NewRecorder()
	h.Create(w, r.WithContext(ctx))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	return create.got
}

const seededBody = `{"name":"demo","instanceId":"inst-1",
	"message":{"kind":"text","bodies":["oi"]},
	"targets":[{"number":"5584999990001"}],
	"seedOutcome":{"sentPercent":40,"failedPercent":10}}`

func TestCreateCarriesSeedOutcomeForAnAdmin(t *testing.T) {
	got := postCampaign(t, "admin", seededBody)

	if got.SeedOutcome == nil {
		t.Fatal("seed outcome was dropped for an admin")
	}
	want := campaign.SeededOutcome{SentPercent: 40, FailedPercent: 10}
	if *got.SeedOutcome != want {
		t.Fatalf("seed outcome = %+v, want %+v", *got.SeedOutcome, want)
	}
}

func TestCreateDropsSeedOutcomeForEveryoneElse(t *testing.T) {
	for _, role := range []string{"user", ""} {
		got := postCampaign(t, role, seededBody)
		if got.SeedOutcome != nil {
			t.Fatalf("role %q kept the seed outcome: %+v", role, *got.SeedOutcome)
		}
		if len(got.Targets) != 1 {
			t.Fatalf("role %q lost its targets", role)
		}
	}
}
