package lead_usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

func TestExplainProfileRefusal(t *testing.T) {
	cases := []struct {
		err  error
		says string
	}{
		{lead.ErrProfileEmpty, "Nenhum dado"},
		{fmt.Errorf("%w: %w", lead.ErrProfileCEPUnchecked, errors.New("timeout")), "indisponível"},
		{lead.ErrProfileCEPUnknown, "CEP não encontrado"},
		{address.ErrCEPMismatch, "não pertence"},
		{address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleZipOrCityRequired}, "cidade e UF"},
		{address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleFormat}, "8 dígitos"},
		{address.InvalidFieldError{Field: address.FieldState, Rule: address.RuleUnknownState}, "UF"},
		{address.InvalidFieldError{Field: address.FieldStreet, Rule: address.RuleTooLong}, "longo demais"},
		{lead.ErrLeadBirthDateInvalid, "data de nascimento"},
		{&customfield.ValueError{Key: "time", Err: customfield.ErrUnknownKey}, `"time" não existe`},
		{&customfield.ValueError{Key: "classificacao", Err: customfield.ErrValueForbidden}, "sensível"},
		{&customfield.ValueError{Key: "cpf", Err: customfield.ErrValueRequired}, "obrigatório"},
		{&customfield.ValueError{Key: "interesse", Err: customfield.ErrValueNotInOptions}, `"interesse"`},
		{lead.ErrLeadNotFound, "lead"},
		{fmt.Errorf("still racing: %w", shared.ErrVersionConflict), "ao mesmo tempo"},
	}
	for _, tc := range cases {
		got, known := ExplainProfileRefusal(tc.err)
		if !known || !strings.Contains(got, tc.says) {
			t.Errorf("ExplainProfileRefusal(%v) = %q, %v; want it to say %q", tc.err, got, known, tc.says)
		}
	}
	if got, known := ExplainProfileRefusal(errors.New("database down")); known || got != "" {
		t.Fatalf("an internal failure is not a refusal, got %q", got)
	}
}
