package workflow

import (
	"errors"
	"testing"
)

func updateLeadGraph(config map[string]interface{}) *Graph {
	return &Graph{Nodes: []Node{
		{ID: "n0", Type: NodeTypeActionSendText, Config: map[string]interface{}{}},
		{ID: "n1", Type: NodeTypeActionUpdateLead, Config: config},
	}}
}

func TestUpdateLeadIsAnActionNode(t *testing.T) {
	if NodeTypeActionUpdateLead != "action_update_lead" || !NodeTypeActionUpdateLead.Valid() || NodeTypeActionUpdateLead.Category() != NodeCategoryAction {
		t.Fatalf("node type %q must be a valid action", NodeTypeActionUpdateLead)
	}
}

func TestValidateUpdateLeadConfig(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		ok     bool
	}{
		{"nothing to write", map[string]interface{}{}, false},
		{"only blank values", map[string]interface{}{"district": "  ", "custom_fields": map[string]interface{}{}}, false},
		{"custom fields without a key", map[string]interface{}{"custom_fields": map[string]interface{}{" ": "alto"}}, false},
		{"custom fields without a value", map[string]interface{}{"custom_fields": map[string]interface{}{"interesse": " "}}, false},
		{"a bairro from a variable", map[string]interface{}{"district": "{{node.extrair.bairro}}"}, true},
		{"a birth date", map[string]interface{}{"birth_date": "{{last.nascimento}}"}, true},
		{"only a name", map[string]interface{}{"name": "{{node.extrair.nome}}"}, true},
		{"a custom field", map[string]interface{}{"custom_fields": map[string]interface{}{"interesse": "{{last.interesse}}"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUpdateLeadConfig(updateLeadGraph(tc.config))
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrNodeMissingRequiredField) {
				t.Fatalf("err = %v, want a missing field", err)
			}
		})
	}
}

func TestActivationRefusesAnUpdateLeadNodeWithNothingToWrite(t *testing.T) {
	if err := RunPureGraphRules(updateLeadGraph(map[string]interface{}{})); !errors.Is(err, ErrNodeMissingRequiredField) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateUpdateLeadAddress(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		ok     bool
	}{
		{"no address", map[string]interface{}{"name": "{{last.nome}}"}, true},
		{"a phone typed in the address number", map[string]interface{}{"number": "{{telefone}}"}, false},
		{"a bairro without city and UF", map[string]interface{}{"district": "Centro", "city": "Recife"}, false},
		{"a number with the CEP", map[string]interface{}{"zip_code": "{{last.cep}}", "number": "{{last.numero}}"}, true},
		{"a bairro with city and UF", map[string]interface{}{"district": "Centro", "city": "Recife", "state": "PE"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUpdateLeadAddress(updateLeadGraph(tc.config))
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want ok %v", err, tc.ok)
			}
		})
	}
}

func TestAnUnknownCustomFieldIssueNamesTheNodeTheFieldAndTheKey(t *testing.T) {
	issue := IssueOf(&CustomFieldRefError{NodeID: "n29", Key: "Advogado Escolhido", Err: ErrNodeUnknownCustomField})

	if issue.Code != LintUnknownCustomField || issue.Severity != SeverityBlocking || issue.NodeID != "n29" ||
		issue.Field != UpdateLeadCustomFields || issue.Path != "Advogado Escolhido" {
		t.Fatalf("issue = %+v", issue)
	}
}
