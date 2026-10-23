package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/workflow"
)

type customFieldsStub struct {
	defs  []*customfield.Definition
	err   error
	asked string
}

func (s *customFieldsStub) ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	s.asked = workspaceID + "/" + string(objectType)
	return s.defs, s.err
}

func updateLeadNode(fields map[string]interface{}) *workflow.Node {
	return &workflow.Node{ID: "n29", Type: workflow.NodeTypeActionUpdateLead, Config: map[string]interface{}{workflow.UpdateLeadCustomFields: fields}}
}

func TestTheUpdateLeadNodeOnlyWritesFieldsTheWorkspaceHas(t *testing.T) {
	defs := []*customfield.Definition{
		{Key: "advogado_escolhido", ObjectType: customfield.ObjectLead},
		{Key: "cpf", ObjectType: customfield.ObjectLead, Sensitive: true},
	}
	for name, tc := range map[string]struct {
		fields map[string]interface{}
		want   error
		key    string
	}{
		"an existing field":         {map[string]interface{}{"advogado_escolhido": "{{advgesp}}"}, nil, ""},
		"the label typed as key":    {map[string]interface{}{"Advogado Escolhido": "{{advgesp}}"}, workflow.ErrNodeUnknownCustomField, "Advogado Escolhido"},
		"a sensitive field":         {map[string]interface{}{"cpf": "{{last.cpf}}"}, workflow.ErrNodeSensitiveCustomField, "cpf"},
		"no custom fields to write": {map[string]interface{}{}, nil, ""},
	} {
		stub := &customFieldsStub{defs: defs}
		err := (&customFieldValidator{repo: stub, workspaceID: "ws-1"}).Validate(updateLeadNode(tc.fields))

		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Fatalf("%s: err = %v, want %v", name, err, tc.want)
		}
		var ref *workflow.CustomFieldRefError
		if tc.want != nil && (!errors.As(err, &ref) || ref.Key != tc.key || ref.NodeID != "n29") {
			t.Fatalf("%s: ref = %+v", name, ref)
		}
	}
}

func TestACustomFieldLookupThatFailsIsNotAValidationVerdict(t *testing.T) {
	stub := &customFieldsStub{err: errors.New("db down")}

	err := (&customFieldValidator{repo: stub, workspaceID: "ws-1"}).Validate(updateLeadNode(map[string]interface{}{"x": "1"}))

	if err == nil || workflow.IsValidationError(err) || stub.asked != "ws-1/lead" {
		t.Fatalf("err = %v, asked %q; a failed lookup must stop the publish, not pass or blame the draft", err, stub.asked)
	}
}
