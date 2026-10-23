package carriercampaign_usecase

import (
	"context"
	"sort"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/sheet"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaignaudience/audiencetest"
	"vozko/usecases/emailhygiene/hygienetest"
)

func (f *fakeSelections) Resolve(_ context.Context, scope selection.Scope, _ selection.Selection, after string, limit int) ([]selection.Ref, error) {
	f.scopes = append(f.scopes, scope)
	ids := append([]string(nil), f.ids...)
	sort.Strings(ids)
	var out []selection.Ref
	for _, id := range ids {
		if id > after && len(out) < limit {
			out = append(out, selection.Ref{ID: id, Type: lead.SelectionRefType})
		}
	}
	return out, nil
}

type audienceSheets struct{}

func (audienceSheets) Read(data []byte) ([]sheet.Row, error) { return sheet.Parse(data), nil }

type audienceLookups struct{}

func (audienceLookups) IdentityIDs(context.Context, string, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (audienceLookups) IDsByEmails(context.Context, string, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (audienceLookups) FindOrCreateManyByEmail(context.Context, string, []lead.BulkEmailInput) (map[string]*lead.Lead, error) {
	return map[string]*lead.Lead{}, nil
}

func (audienceLookups) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return nil, nil
}

type audienceGate struct{}

func (audienceGate) Acquire(context.Context) (func(), error) { return func() {}, nil }

func (h *harness) wireAudience(t interface{ Fatal(...any) }) {
	aud, err := campaignaudience_usecase.New(campaignaudience_usecase.Deps{
		Selections: h.selections, Lists: audiencetest.NewLists(), Sheets: audienceSheets{}, Phones: audienceLookups{}, Emails: audienceLookups{},
		EmailLeads: audienceLookups{}, EmailsSeen: &hygienetest.SeenEmails{}, Definitions: audienceLookups{}, Gate: audienceGate{}, Flight: audiencetest.Flight(audiencetest.NewState()), Now: func() time.Time { return h.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	aud.AddChannel(h.svc.AudienceChannel())
	h.svc.SetAudience(aud)
}
