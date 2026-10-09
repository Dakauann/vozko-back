package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/tools"
	lead_usecase "vozko/usecases/lead"
)

type fakeLeadProfiles struct {
	updates []lead_usecase.ProfileUpdate
	result  lead_usecase.ProfileResult
	err     error
	fields  []*customfield.Definition
}

func (f *fakeLeadProfiles) Update(_ context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error) {
	f.updates = append(f.updates, in)
	return f.result, f.err
}

func (f *fakeLeadProfiles) WritableFields(string) ([]*customfield.Definition, error) {
	return f.fields, nil
}

func profileToolConfig() map[string]interface{} {
	return map[string]interface{}{"__workspace_id": "ws-1", "__lead_id": "lead-1", "__agent_id": "agent-1"}
}

func runProfileTool(t *testing.T, profiles *fakeLeadProfiles, config, params map[string]interface{}) tools.ExecutionResult {
	t.Helper()
	handler := NewUpdateLeadProfileTool(profiles)
	if handler == nil {
		t.Fatal("the tool must be built with its use case")
	}
	result, err := handler.ExecuteWithConfig(context.Background(), config, params)
	if err != nil {
		t.Fatalf("the tool answers refusals as results, got %v", err)
	}
	return result
}

func TestUpdateLeadProfileToolIsAMessagingTool(t *testing.T) {
	def := NewUpdateLeadProfileTool(&fakeLeadProfiles{}).Definition()
	if UpdateLeadProfileToolName != "update_lead_profile" || def.Name != UpdateLeadProfileToolName || !def.IsVisibleIn(tools.VisibilityMessaging) || def.IsVisibleIn(tools.VisibilityAnalysis) {
		t.Fatalf("definition = %+v", def)
	}
	for _, param := range []string{"cep", "logradouro", "numero", "complemento", "bairro", "cidade", "uf", "data_nascimento", "campos"} {
		if _, ok := def.Parameters[param]; !ok {
			t.Errorf("parameter %q is missing", param)
		}
	}
	if NewUpdateLeadProfileTool(nil) != nil {
		t.Fatal("the tool is not offered without its use case")
	}
}

func TestUpdateLeadProfileToolWritesWithTheAgentAsAConversationSource(t *testing.T) {
	profiles := &fakeLeadProfiles{result: lead_usecase.ProfileResult{LeadID: "lead-1", Version: 4, Changed: []string{lead.FieldAddresses, lead.FieldBirthDate, lead.CustomFieldName("interesse")}}}
	result := runProfileTool(t, profiles, profileToolConfig(), map[string]interface{}{
		"cep": "01310-100", "numero": "1000", "complemento": "ap 12", "bairro": "Bela Vista",
		"data_nascimento": "15/03/1980",
		"campos":          map[string]interface{}{"interesse": "alto", "orcamento": 1500.5, "cliente": true},
	})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	want := lead_usecase.ProfileUpdate{
		WorkspaceID: "ws-1", LeadID: "lead-1", Actor: "ai:agent-1", Source: lead.ProfileFromConversation,
		Profile: lead.Profile{
			Address:      address.Postal{ZipCode: "01310-100", Number: "1000", Complement: "ap 12", District: "Bela Vista"},
			BirthDate:    "15/03/1980",
			CustomFields: map[string]string{"interesse": "alto", "orcamento": "1500.5", "cliente": "true"},
		},
	}
	if len(profiles.updates) != 1 || !reflect.DeepEqual(profiles.updates[0], want) {
		t.Fatalf("updates = %+v", profiles.updates)
	}
	text := fmt.Sprint(result.Result)
	for _, part := range []string{"endereço", "data de nascimento", "interesse"} {
		if !strings.Contains(text, part) {
			t.Errorf("the answer must name %q: %s", part, text)
		}
	}
	if result.ContextUpdateText == "" {
		t.Error("a written profile is shown in the conversation timeline")
	}
}

func TestUpdateLeadProfileToolExplainsAConflictWithoutTheStoredValue(t *testing.T) {
	profiles := &fakeLeadProfiles{result: lead_usecase.ProfileResult{LeadID: "lead-1", Version: 3, Conflicts: []string{lead.FieldAddresses}}}
	result := runProfileTool(t, profiles, profileToolConfig(), map[string]interface{}{"logradouro": "Rua Augusta", "cidade": "São Paulo", "uf": "SP"})
	text := fmt.Sprint(result.Result)
	if result.IsError || !strings.Contains(text, "endereço") || !strings.Contains(text, "atendente") || result.ContextUpdateText != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestUpdateLeadProfileToolRefusesWithoutAConversationLead(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"no workspace":  {"__lead_id": "lead-1"},
		"no lead":       {"__workspace_id": "ws-1"},
		"no config":     nil,
		"blank lead id": {"__workspace_id": "ws-1", "__lead_id": "  "},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			profiles := &fakeLeadProfiles{}
			result := runProfileTool(t, profiles, config, map[string]interface{}{"bairro": "Centro"})
			if !result.IsError || len(profiles.updates) != 0 {
				t.Fatalf("result = %+v, updates %d", result, len(profiles.updates))
			}
		})
	}
}

func TestUpdateLeadProfileToolRefusesAnEmptyCall(t *testing.T) {
	profiles := &fakeLeadProfiles{}
	result := runProfileTool(t, profiles, profileToolConfig(), map[string]interface{}{"bairro": " ", "campos": map[string]interface{}{}})
	if !result.IsError || len(profiles.updates) != 0 {
		t.Fatalf("result = %+v, updates %d", result, len(profiles.updates))
	}
}

func TestUpdateLeadProfileToolRefusesCustomFieldsThatAreNotAnObject(t *testing.T) {
	profiles := &fakeLeadProfiles{}
	result := runProfileTool(t, profiles, profileToolConfig(), map[string]interface{}{"campos": "interesse=alto"})
	if !result.IsError || len(profiles.updates) != 0 {
		t.Fatalf("result = %+v, updates %d", result, len(profiles.updates))
	}
}

func TestUpdateLeadProfileToolExplainsEachRefusal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		says string
	}{
		{"an unknown CEP", lead.ErrProfileCEPUnknown, "CEP não encontrado"},
		{"a CEP that cannot be checked now", fmt.Errorf("%w: %w", lead.ErrProfileCEPUnchecked, errors.New("timeout")), "Tente novamente"},
		{"a city that is not the CEP's", address.ErrCEPMismatch, "não pertence"},
		{"an incomplete address", address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleZipOrCityRequired}, "cidade e UF"},
		{"a malformed CEP", address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleFormat}, "8 dígitos"},
		{"a bad birth date", lead.ErrLeadBirthDateInvalid, "data de nascimento"},
		{"a sensitive field", &customfield.ValueError{Key: "classificacao", Err: customfield.ErrValueForbidden}, "classificacao"},
		{"a value outside the options", &customfield.ValueError{Key: "interesse", Err: fmt.Errorf("%w: x", customfield.ErrValueNotInOptions)}, "alto, baixo"},
		{"a lead that is gone", lead.ErrLeadNotFound, "lead"},
		{"a race that kept losing", shared.ErrVersionConflict, "tente novamente"},
		{"anything else", errors.New("database down"), "Não foi possível"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profiles := &fakeLeadProfiles{err: tc.err, fields: []*customfield.Definition{
				{Key: "interesse", Label: "Interesse", Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
			}}
			result := runProfileTool(t, profiles, profileToolConfig(), map[string]interface{}{"bairro": "Centro"})
			if !result.IsError || !strings.Contains(fmt.Sprint(result.Result), tc.says) {
				t.Fatalf("result = %+v, want it to say %q", result, tc.says)
			}
			if strings.Contains(fmt.Sprint(result.Result), "database down") {
				t.Fatal("internal errors never reach the model")
			}
		})
	}
}

func TestUpdateLeadProfileToolListsTheFieldsItMayWriteForAnUnknownKey(t *testing.T) {
	profiles := &fakeLeadProfiles{
		err: &customfield.ValueError{Key: "time", Err: customfield.ErrUnknownKey},
		fields: []*customfield.Definition{
			{Key: "cor", Label: "Cor", Type: customfield.TypeText},
			{Key: "interesse", Label: "Interesse", Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
		},
	}
	result := runProfileTool(t, profiles, profileToolConfig(), map[string]interface{}{"campos": map[string]interface{}{"time": "Santos"}})
	text := fmt.Sprint(result.Result)
	if !result.IsError || !strings.Contains(text, "time") || !strings.Contains(text, "cor") || !strings.Contains(text, "interesse (alto, baixo)") {
		t.Fatalf("result = %+v", result)
	}
}

func birthDateOnlyConfig() map[string]interface{} {
	config := profileToolConfig()
	for key, value := range BirthDateOnlyProfileConfig() {
		config[key] = value
	}
	return config
}

func TestABirthDateOnlyProfileToolWritesTheBirthDate(t *testing.T) {
	profiles := &fakeLeadProfiles{result: lead_usecase.ProfileResult{Changed: []string{lead.FieldBirthDate}}}
	result := runProfileTool(t, profiles, birthDateOnlyConfig(), map[string]interface{}{"data_nascimento": "15/03/1980", "bairro": " "})
	if result.IsError || len(profiles.updates) != 1 || profiles.updates[0].Profile.BirthDate != "15/03/1980" {
		t.Fatalf("result = %+v, updates %+v", result, profiles.updates)
	}
}

func TestABirthDateOnlyProfileToolRefusesEveryOtherField(t *testing.T) {
	for name, params := range map[string]map[string]interface{}{
		"custom fields": {"data_nascimento": "15/03/1980", "campos": map[string]interface{}{"interesse": "alto"}},
		"an address":    {"cep": "01310-100"},
		"a district":    {"bairro": "Centro", "cidade": "Natal", "uf": "RN"},
	} {
		profiles := &fakeLeadProfiles{}
		result := runProfileTool(t, profiles, birthDateOnlyConfig(), params)
		if !result.IsError || len(profiles.updates) != 0 || !strings.Contains(fmt.Sprint(result.Result), "data de nascimento") {
			t.Fatalf("%s: result = %+v, updates %d", name, result, len(profiles.updates))
		}
	}
	profiles := &fakeLeadProfiles{}
	broken := profileToolConfig()
	broken[profileParamsConfigKey] = "data_nascimento"
	if result := runProfileTool(t, profiles, broken, map[string]interface{}{"data_nascimento": "15/03/1980"}); !result.IsError || len(profiles.updates) != 0 {
		t.Fatalf("a malformed scope wrote: %+v", result)
	}
}

func TestABirthDateOnlyDefinitionOffersOnlyTheBirthDate(t *testing.T) {
	def := BirthDateOnlyProfileDefinition(NewUpdateLeadProfileTool(&fakeLeadProfiles{}).Definition())
	if def.Name != UpdateLeadProfileToolName || len(def.Parameters) != 1 {
		t.Fatalf("definition = %+v", def.Parameters)
	}
	if _, ok := def.Parameters[ProfileBirthDateParam]; !ok || strings.Contains(def.Description, "campos personalizados") {
		t.Fatalf("definition = %+v", def)
	}
	if full := NewUpdateLeadProfileTool(&fakeLeadProfiles{}).Definition(); len(full.Parameters) != 9 {
		t.Fatalf("narrowing changed the full definition: %d parameters", len(full.Parameters))
	}
}
