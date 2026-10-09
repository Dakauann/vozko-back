package copilottools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/address"
	"vozko/domain/copilot"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	leadaction_usecase "vozko/usecases/leadaction"
)

var leakedSecrets = []string{"5584994409624", "994409624", "84994409624", "5584988887777", "88887777", "Positivo", "Rua das Flores", "59010000"}

func privateLead() *lead.Lead {
	return &lead.Lead{
		ID: knownLead, Name: "Maria", Number: "5584994409624",
		Phones:       []lead.ContactPhone{{Number: "5584988887777", Label: lead.PhoneLandline}},
		CustomFields: map[string]any{"posicao": "Positivo", "interesse": "alto"},
		Addresses: []lead.Address{{Primary: true, Postal: address.Postal{
			ZipCode: "59010000", Street: "Rua das Flores", Number: "123", District: "Centro", City: "Natal", State: "RN",
		}}},
	}
}

type privateLeads struct{ fakeLeads }

func (privateLeads) Get(string, string) (*lead.Lead, error) { return privateLead(), nil }

func whatTheModelReads(t *testing.T, res copilot.Result) string {
	t.Helper()
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	b, err := json.Marshal(res.Data)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + res.Message
}

func TestTheModelNeverReceivesNumbersOrSensitiveValues(t *testing.T) {
	deps, _ := filterDeps()
	deps.Leads = &privateLeads{}
	deps.Memories = &fakeMemories{}
	deps.Pages = &fakePages{items: []*lead.LeadWithSummary{{Lead: privateLead(), Summary: &lead.LeadSummary{}, OwnerName: "Ana"}}}
	deps.Sections = &fakeLeadSections{summary: &lead.SummarySection{Total: 1, WithAddress: 1}, places: &lead.PlacesSection{
		Cities:    []lead.CityCount{{CityKey: "rn:natal", City: "Natal", State: "RN", Count: 1}},
		Districts: []lead.DistrictCount{{Pair: "rn:natal/centro", District: "Centro", City: "Natal", State: "RN", Count: 1}},
	}}

	h := newActionHarness(counted(1, 1, nil))
	h.deps.Leads = deps
	h.actions.outcome = leadaction_usecase.Outcome{Run: &leadaction.Run{
		ID: "run-1", Action: leadaction.ActionClassify, Status: leadaction.StatusQueued,
		Params: leadaction.Params{Key: "posicao", Value: json.RawMessage(`"Positivo"`)},
	}}
	h.sends.review = sendReview()
	prepare := NewPrepareLeadActionTool(h.deps)
	prepareArgs := map[string]interface{}{"action": "classify", "lead_ids": []interface{}{knownLead}, "field_key": "interesse", "value": "alto"}
	propose(t, prepare, manager(), prepareArgs)
	sendArgs := map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}}

	reads := map[string]copilot.Result{
		"search_leads":        NewSearchLeadsTool(deps).Execute(context.Background(), member(), map[string]interface{}{"query": "maria"}),
		"get_lead":            NewGetLeadTool(deps).Execute(context.Background(), member(), map[string]interface{}{"lead_id": knownLead}),
		"lead_geo_summary":    NewLeadGeoSummaryTool(deps).Execute(context.Background(), member(), nil),
		"prepare_lead_action": prepare.Execute(context.Background(), manager(), prepareArgs),
		"start_lead_send":     NewStartLeadSendTool(h.deps).Execute(context.Background(), manager(), sendArgs),
	}
	for name, res := range reads {
		seen := whatTheModelReads(t, res)
		for _, secret := range leakedSecrets {
			if strings.Contains(seen, secret) {
				t.Errorf("%s hands the model %q: %s", name, secret, seen)
			}
		}
	}
}

func TestTheModelCannotTurnASensitiveFieldIntoAnAction(t *testing.T) {
	h := newActionHarness(counted(1, 1, nil))
	err := NewPrepareLeadActionTool(h.deps).(copilot.Validator).Validate(context.Background(), manager(), map[string]interface{}{
		"action": "classify", "lead_ids": []interface{}{knownLead}, "field_key": "posicao", "value": "Positivo",
	})
	if err == nil || strings.Contains(err.Error(), "Positivo") || len(h.actions.previews) != 0 {
		t.Fatalf("Validate = %v, previews %d", err, len(h.actions.previews))
	}
}

func TestLeadActionsTakeLeadsNeverPhoneNumbers(t *testing.T) {
	for _, tool := range []copilot.Tool{NewPrepareLeadActionTool(LeadActionDeps{}), NewStartLeadSendTool(LeadActionDeps{}), NewCancelLeadSendTool(LeadActionDeps{})} {
		for name, param := range tool.Definition().Parameters {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "phone") && !strings.HasSuffix(lower, "_id") || strings.Contains(lower, "numbers") || lower == "number" || lower == "numero" {
				t.Errorf("%s takes %q (%s): actions on leads target lead ids or a filter, never phone numbers", tool.Definition().Name, name, param.Type)
			}
		}
	}
}
