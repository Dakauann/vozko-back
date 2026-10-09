package customfield

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	customfielddomain "vozko/domain/customfield"
)

func TestCreateRequestCarriesTheNewProperties(t *testing.T) {
	sensitive := true
	in := createInputFrom(CreateCustomFieldRequest{
		ObjectType:  " lead ",
		Key:         " classificacao ",
		Label:       " Classificação ",
		Type:        " select ",
		Options:     []string{"Positivo"},
		OptionTones: map[string]string{"Positivo": "chart-1"},
		Sensitive:   &sensitive,
		LegalBasis:  "Consentimento",
		Role:        " classification ",
	})
	if in.ObjectType != customfielddomain.ObjectLead || in.Key != "classificacao" || in.Type != customfielddomain.TypeSelect {
		t.Fatalf("createInputFrom() = %+v", in)
	}
	if in.Sensitive == nil || !*in.Sensitive || in.LegalBasis != "Consentimento" || in.Role != customfielddomain.RoleClassification {
		t.Fatalf("createInputFrom() = %+v", in)
	}
	if in.OptionTones["Positivo"] != customfielddomain.ToneChart1 {
		t.Fatalf("OptionTones = %v", in.OptionTones)
	}
}

func TestCreateRequestLeavesAnUnansweredSensitivityUnanswered(t *testing.T) {
	if in := createInputFrom(CreateCustomFieldRequest{ObjectType: "lead"}); in.Sensitive != nil {
		t.Fatal("a missing sensitive choice must reach the use case as missing")
	}
}

func TestUpdateRequestKeepsAbsentPropertiesAbsent(t *testing.T) {
	in := updateInputFrom(UpdateCustomFieldRequest{})
	if in.Type != nil || in.Role != nil || in.OptionTones != nil || in.Sensitive != nil || in.LegalBasis != nil {
		t.Fatalf("updateInputFrom() = %+v", in)
	}

	role := " "
	cleared := updateInputFrom(UpdateCustomFieldRequest{Role: &role, OptionTones: map[string]string{}})
	if cleared.Role == nil || *cleared.Role != "" {
		t.Fatalf("an empty role must clear the role, got %+v", cleared.Role)
	}
	if cleared.OptionTones == nil || len(cleared.OptionTones) != 0 {
		t.Fatalf("an empty tone map must clear the tones, got %#v", cleared.OptionTones)
	}
}

func TestDomainErrorsMapToStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{customfielddomain.ErrNotFound, http.StatusNotFound, "custom_field_not_found"},
		{customfielddomain.ErrKeyExists, http.StatusConflict, "custom_field_key_exists"},
		{customfielddomain.ErrRoleTaken, http.StatusConflict, "custom_field_role_taken"},
		{customfielddomain.ErrInvalidObjectType, http.StatusBadRequest, "custom_field_invalid_object_type"},
		{customfielddomain.ErrSensitivityChoiceMissing, http.StatusBadRequest, "custom_field_sensitivity_choice_missing"},
		{customfielddomain.ErrLegalBasisRequired, http.StatusBadRequest, "custom_field_legal_basis_required"},
		{customfielddomain.ErrLegalBasisTooLong, http.StatusBadRequest, "custom_field_legal_basis_too_long"},
		{fmt.Errorf("%w: %q", customfielddomain.ErrInvalidTone, "green"), http.StatusBadRequest, "custom_field_invalid_tone"},
		{customfielddomain.ErrToneUnknownOption, http.StatusBadRequest, "custom_field_tone_unknown_option"},
		{customfielddomain.ErrInvalidRole, http.StatusBadRequest, "custom_field_invalid_role"},
		{customfielddomain.ErrRoleRequiresSelect, http.StatusBadRequest, "custom_field_role_requires_select"},
		{customfielddomain.ErrOptionsRequired, http.StatusBadRequest, "custom_field_options_required"},
		{customfielddomain.ErrSensitiveUnsupportedObject, http.StatusBadRequest, "custom_field_sensitive_unsupported_object"},
		{fmt.Errorf("%w: a future rule", customfielddomain.ErrInvalidDefinition), http.StatusBadRequest, "custom_field_invalid_definition"},
		{errors.New("db down"), http.StatusInternalServerError, ""},
	}
	h := &CustomFieldHandler{}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.handleDomainError(rec, tc.err)
		if rec.Code != tc.status {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.status)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != tc.code {
			t.Errorf("%v: code = %q, want %q (%v)", tc.err, body.Code, tc.code, err)
		}
	}
}
