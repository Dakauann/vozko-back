package emailsending

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

type lifecycleBody struct {
	Domains []struct {
		ID        string `json:"id"`
		Lifecycle string `json:"lifecycle"`
		Sendable  bool   `json:"sendable"`
		Actions   *struct {
			Recheck *bool `json:"recheck"`
			Remove  *bool `json:"remove"`
		} `json:"actions"`
	} `json:"domains"`
	Senders []struct {
		ID              string `json:"id"`
		Address         string `json:"address"`
		DomainLifecycle string `json:"domain_lifecycle"`
	} `json:"senders"`
}

func TestSetupMarksAReleasingDomainAndItsSenders(t *testing.T) {
	own := es.SendingDomain{ID: "d-own", Domain: "loja.com.br", Kind: es.DomainOwn, Status: es.DomainVerified, ConnectMethod: es.ConnectManual}
	releasing := es.SendingDomain{ID: "d-old", Domain: "antiga.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic}
	releasing.Release()
	released := es.SendingDomain{ID: "d-gone", Domain: "velha.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic}
	released.Release()
	released.Released()
	view := esuc.StatusView{Provisioned: true,
		Domains: []esuc.DomainView{{Domain: own}, {Domain: releasing}},
		Senders: []esuc.SenderView{
			{Sender: es.Sender{ID: "s-own", DomainID: own.ID, LocalPart: "contato"}, Address: "contato@loja.com.br", Domain: own},
			{Sender: es.Sender{ID: "s-old", DomainID: releasing.ID, LocalPart: "vendas"}, Address: "vendas@antiga.envios.test", Domain: releasing},
			{Sender: es.Sender{ID: "s-gone", DomainID: released.ID, LocalPart: "oi"}, Address: "oi@velha.envios.test", Domain: released},
		},
	}
	raw, err := json.Marshal(presentSetup(view))
	if err != nil {
		t.Fatal(err)
	}
	var body lifecycleBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Domains) != 2 || len(body.Senders) != 3 {
		t.Fatalf("body = %s", raw)
	}
	active, gone := body.Domains[0], body.Domains[1]
	if active.Lifecycle != "active" || active.Actions == nil || active.Actions.Recheck == nil || !*active.Actions.Recheck || active.Actions.Remove == nil || !*active.Actions.Remove {
		t.Fatalf("active domain = %s", raw)
	}
	if gone.Lifecycle != "releasing" || gone.Sendable || gone.Actions == nil || *gone.Actions.Recheck || *gone.Actions.Remove {
		t.Fatalf("releasing domain = %s", raw)
	}
	want := map[string]string{"s-own": "active", "s-old": "releasing", "s-gone": "retired"}
	for _, s := range body.Senders {
		if s.DomainLifecycle != want[s.ID] || s.Address == "" {
			t.Fatalf("sender %s = %s", s.ID, raw)
		}
	}
}

func TestSenderRefusalNamesTheReleasingDomain(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, es.ErrDomainReleasing, "create sender")
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || body.Code != "email_domain_releasing" {
		t.Fatalf("refusal = %d %s", rec.Code, rec.Body.String())
	}
}
