package emailsending

import (
	"encoding/json"
	"testing"
	"time"

	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

func TestSetupCarriesThePausedReplies(t *testing.T) {
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	view := esuc.StatusView{Provisioned: true, RepliesPaused: true, Domains: []esuc.DomainView{
		{Domain: es.SendingDomain{ID: "d-1", Kind: es.DomainOwn, ConnectMethod: es.ConnectManual, RepliesPausedAt: &at}},
		{Domain: es.SendingDomain{ID: "d-2", Kind: es.DomainSharedTrusted, ConnectMethod: es.ConnectAutomatic}},
	}}
	raw, err := json.Marshal(presentSetup(view))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		RepliesPaused *bool `json:"replies_paused"`
		Domains       []struct {
			RepliesPaused   *bool      `json:"replies_paused"`
			RepliesPausedAt *time.Time `json:"replies_paused_at"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.RepliesPaused == nil || !*body.RepliesPaused || len(body.Domains) != 2 {
		t.Fatalf("body = %s", raw)
	}
	if !*body.Domains[0].RepliesPaused || !body.Domains[0].RepliesPausedAt.Equal(at) || *body.Domains[1].RepliesPaused || body.Domains[1].RepliesPausedAt != nil {
		t.Fatalf("domains = %s", raw)
	}
}
