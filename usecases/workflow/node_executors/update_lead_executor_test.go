package node_executors

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	lead_usecase "vozko/usecases/lead"
)

type leadProfilesStub struct {
	refs    []shared.EntryRef
	updates []lead_usecase.ProfileUpdate
	result  lead_usecase.ProfileResult
	err     error
}

func (s *leadProfilesStub) UpdateOfEntry(_ context.Context, ref shared.EntryRef, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error) {
	s.refs = append(s.refs, ref)
	s.updates = append(s.updates, in)
	return s.result, s.err
}

func updateLead(t *testing.T, profiles LeadProfileWriter, config map[string]interface{}) *workflow.NodeResult {
	t.Helper()
	ctx := stageNodeCtx(workflow.NodeTypeActionUpdateLead, config, moveEdges())
	ctx.State.Set("_last_cep", "01310-100")
	ctx.State.Set("_last_interesse", "alto")
	result, err := NewUpdateLeadExecutor(profiles).Execute(ctx)
	require.NoError(t, err)
	return result
}

func TestUpdateLeadNodeWritesTheConversationLeadAsTheWorkflow(t *testing.T) {
	stub := &leadProfilesStub{result: lead_usecase.ProfileResult{LeadID: "l-1", Version: 4, Changed: []string{lead.FieldAddresses, lead.CustomFieldName("interesse")}, Conflicts: []string{lead.FieldBirthDate}}}
	result := updateLead(t, stub, map[string]interface{}{
		"zip_code": "{{last.cep}}", "number": " 1000 ", "district": "Bela Vista", "birth_date": "15/03/1980",
		"custom_fields": map[string]interface{}{"interesse": "{{last.interesse}}", "cor": " "},
	})

	require.Equal(t, "ok", result.NextNodeID)
	require.Equal(t, []shared.EntryRef{{EntryID: "e-1", EntryType: shared.EntryType("unofficial_whatsapp")}}, stub.refs)
	require.Equal(t, lead_usecase.ProfileUpdate{
		WorkspaceID: "ws1", Actor: "workflow:wf-1", Source: lead.ProfileFromWorkflow,
		Profile: lead.Profile{
			Address:      address.Postal{ZipCode: "01310-100", Number: "1000", District: "Bela Vista"},
			BirthDate:    "15/03/1980",
			CustomFields: map[string]string{"interesse": "alto"},
		},
	}, stub.updates[0])
	require.Equal(t, true, result.Output["success"])
	require.Equal(t, "l-1", result.Output["lead_id"])
	require.Equal(t, []string{"addresses", "customFields.interesse"}, result.Output["changed"])
	require.Equal(t, []string{"birthDate"}, result.Output["conflicts"])
}

func TestUpdateLeadNodeWithNothingToWriteFollowsTheErrorOutput(t *testing.T) {
	stub := &leadProfilesStub{err: lead.ErrProfileEmpty}
	result := updateLead(t, stub, map[string]interface{}{"district": "{{last.nada}}"})
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, false, result.Output["success"])
	require.Contains(t, result.Output["error"], "Nenhum dado")
}

func TestUpdateLeadNodeExplainsRefusalsOnTheErrorOutput(t *testing.T) {
	cases := []struct {
		name string
		err  error
		says string
	}{
		{"a conversation without a lead", lead.ErrLeadNotFound, "lead"},
		{"an unknown CEP", lead.ErrProfileCEPUnknown, "CEP não encontrado"},
		{"a CEP that cannot be checked now", lead.ErrProfileCEPUnchecked, "indisponível"},
		{"a city that is not the CEP's", address.ErrCEPMismatch, "não pertence"},
		{"an incomplete address", address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleZipOrCityRequired}, "cidade e UF"},
		{"a bad birth date", lead.ErrLeadBirthDateInvalid, "data de nascimento"},
		{"an unknown field", &customfield.ValueError{Key: "time", Err: customfield.ErrUnknownKey}, "time"},
		{"a sensitive field", &customfield.ValueError{Key: "classificacao", Err: customfield.ErrValueForbidden}, "sensível"},
		{"a race that kept losing", shared.ErrVersionConflict, "ao mesmo tempo"},
		{"anything else", errors.New("database down"), "falha ao atualizar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := updateLead(t, &leadProfilesStub{err: tc.err}, map[string]interface{}{"district": "Centro"})
			require.Equal(t, "fail", result.NextNodeID)
			require.Equal(t, false, result.Output["success"])
			require.Contains(t, result.Output["error"], tc.says)
			require.NotContains(t, result.Output["error"], "database down")
		})
	}
}

func TestUpdateLeadNodeWithoutTheLeadServiceRefuses(t *testing.T) {
	result := updateLead(t, nil, map[string]interface{}{"district": "Centro"})
	require.Equal(t, "fail", result.NextNodeID)
	require.Equal(t, false, result.Output["success"])
	require.Contains(t, result.Output["error"], "indisponível")
}

func TestUpdateLeadNodeDefinitionDocumentsItsFields(t *testing.T) {
	def := NewUpdateLeadExecutor(&leadProfilesStub{}).Definition()
	require.Equal(t, workflow.NodeTypeActionUpdateLead, def.Type)
	require.Equal(t, workflow.NodeCategoryAction, def.Category)
	require.True(t, def.Type.Valid())
	require.NotEmpty(t, def.Guidance.When)
	require.NotEmpty(t, def.Guidance.Behavior)
	keys := map[string]string{}
	for _, field := range def.ConfigSchema {
		keys[field.Key] = field.Type
		require.False(t, field.Required, "%s is optional; the node needs at least one field, checked on activation", field.Key)
	}
	for _, key := range workflow.UpdateLeadTextFields {
		require.Equal(t, "text", keys[key], key)
	}
	require.Equal(t, "keyvalue", keys[workflow.UpdateLeadCustomFields])
	outputs := map[string]bool{}
	for _, o := range def.Outputs {
		outputs[o.ID] = o.Optional
	}
	require.Equal(t, map[string]bool{"sucesso": false, "erro": true}, outputs)
}
