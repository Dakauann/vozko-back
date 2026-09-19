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

// capturingCreate records the campaign the handler actually handed down, which
// is the only place the admin gate can be observed: everything below this layer
// trusts what it is given.
type capturingCreate struct{ got *uwc.Campaign }

func (c *capturingCreate) Execute(_ context.Context, in *uwc.Campaign, _ uw.DepartmentScope) (*uwc.Campaign, error) {
	c.got = in
	return &uwc.Campaign{ID: "c-1", WorkspaceID: in.WorkspaceID}, nil
}

func postCampaign(t *testing.T, role, body string) *uwc.Campaign {
	t.Helper()
	create := &capturingCreate{}
	h := NewCampaignHandler(CampaignHandlerDeps{Create: create})

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
	"seedOutcome":{"respondedPercent":40,"failedPercent":10}}`

// A platform administrator may create a campaign that already carries results.
func TestCreateCarriesSeedOutcomeForAnAdmin(t *testing.T) {
	got := postCampaign(t, "admin", seededBody)

	if got.SeedOutcome == nil {
		t.Fatal("seed outcome was dropped for an admin")
	}
	want := campaign.SeededOutcome{RespondedPercent: 40, FailedPercent: 10}
	if *got.SeedOutcome != want {
		t.Fatalf("seed outcome = %+v, want %+v", *got.SeedOutcome, want)
	}
}

// Everyone else gets an ordinary campaign. The workspace role does not matter:
// this is the platform role, and a workspace OWNER passes every gate the route
// applies and still must not fabricate results.
func TestCreateDropsSeedOutcomeForEveryoneElse(t *testing.T) {
	for _, role := range []string{"user", ""} {
		got := postCampaign(t, role, seededBody)
		if got.SeedOutcome != nil {
			t.Fatalf("role %q kept the seed outcome: %+v", role, *got.SeedOutcome)
		}
		// Dropped, not refused: they asked for a campaign and they get one.
		if len(got.Targets) != 1 {
			t.Fatalf("role %q lost its targets", role)
		}
	}
}
