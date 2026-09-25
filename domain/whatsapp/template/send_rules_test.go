package template

import (
	"errors"
	"testing"
)

func approvedWithVariables() *Template {
	return &Template{
		ID:       "tpl-1",
		Name:     "follow_up",
		WABAId:   "waba-1",
		Category: TemplateCategoryUtility,
		Status:   TemplateStatusApproved,
		Components: []TemplateComponent{
			{Type: "HEADER", Format: "TEXT", Text: "Pedido {{1}}"},
			{Type: "BODY", Text: "Oi {{1}}, seu pedido chega {{2}}."},
		},
	}
}

func TestEnsureSendable(t *testing.T) {
	if err := approvedWithVariables().EnsureSendable(); err != nil {
		t.Fatalf("an approved template is sendable, got %v", err)
	}

	paused := approvedWithVariables()
	paused.Status = TemplateStatusPaused
	if err := paused.EnsureSendable(); !errors.Is(err, ErrTemplateNotSendable) {
		t.Fatalf("err = %v, want a paused template refused", err)
	}
}

func TestBelongsToWABA(t *testing.T) {
	tmpl := approvedWithVariables()

	if !tmpl.BelongsToWABA("waba-1") || !tmpl.BelongsToWABA(" WABA-1 ") {
		t.Error("the template's own account must match, ignoring case and spaces")
	}
	if tmpl.BelongsToWABA("waba-2") {
		t.Error("another account's number must not send this template")
	}
}

func TestValidateParams(t *testing.T) {
	cases := []struct {
		name    string
		body    []string
		header  []string
		wantErr error
	}{
		{"every slot filled", []string{"Ana", "amanhã"}, []string{"42"}, nil},
		{"a body slot missing", []string{"Ana"}, []string{"42"}, ErrTemplateParamsMismatch},
		{"a blank body value", []string{"Ana", "  "}, []string{"42"}, ErrTemplateParamsMismatch},
		{"the header left out", []string{"Ana", "amanhã"}, nil, ErrTemplateParamsMismatch},
		{"a value too many", []string{"Ana", "amanhã", "extra"}, []string{"42"}, ErrTemplateParamsMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := approvedWithVariables().ValidateParams(tc.body, tc.header)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestATemplateWithoutVariablesTakesNoValues(t *testing.T) {
	tmpl := &Template{Components: []TemplateComponent{{Type: "BODY", Text: "Olá!"}}}

	if err := tmpl.ValidateParams(nil, nil); err != nil {
		t.Fatalf("err = %v, want no values required", err)
	}
}
