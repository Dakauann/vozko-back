package customfield

import (
	"errors"
	"testing"
)

func def(t FieldType, opts ...string) *Definition {
	return &Definition{
		WorkspaceID: "ws1",
		ObjectType:  "opportunity",
		Key:         "segmento",
		Label:       "Segmento",
		Type:        t,
		Options:     opts,
	}
}

func TestDefinitionValidate(t *testing.T) {
	if err := def(TypeText).Validate(); err != nil {
		t.Fatalf("expected valid text field, got %v", err)
	}
	if err := def(TypeSelect, "a", "b").Validate(); err != nil {
		t.Fatalf("expected valid select field, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Definition)
		want   error
	}{
		{"no workspace", func(d *Definition) { d.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"no key", func(d *Definition) { d.Key = "" }, ErrKeyRequired},
		{"no label", func(d *Definition) { d.Label = "" }, ErrLabelRequired},
		{"bad type", func(d *Definition) { d.Type = "colour" }, ErrInvalidType},
		{"select no options", func(d *Definition) { d.Type = TypeSelect; d.Options = nil }, ErrOptionsRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := def(TypeText)
			tc.mutate(d)
			if err := d.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	if err := def(TypeText).ValidateValue("hello"); err != nil {
		t.Fatalf("text string should pass: %v", err)
	}
	if err := def(TypeText).ValidateValue(42); !errors.Is(err, ErrValueType) {
		t.Fatalf("text got number should fail: %v", err)
	}

	// Numbers arrive from JSON as float64, and strings should also parse.
	if err := def(TypeNumber).ValidateValue(float64(10)); err != nil {
		t.Fatalf("number float64 should pass: %v", err)
	}
	if err := def(TypeNumber).ValidateValue("12.5"); err != nil {
		t.Fatalf("number string should pass: %v", err)
	}
	if err := def(TypeNumber).ValidateValue("abc"); !errors.Is(err, ErrValueType) {
		t.Fatalf("number bad string should fail: %v", err)
	}

	if err := def(TypeDate).ValidateValue("2026-07-12"); err != nil {
		t.Fatalf("iso date should pass: %v", err)
	}
	if err := def(TypeDate).ValidateValue("12/07/2026"); !errors.Is(err, ErrValueType) {
		t.Fatalf("non-iso date should fail: %v", err)
	}

	if err := def(TypeBoolean).ValidateValue(true); err != nil {
		t.Fatalf("bool should pass: %v", err)
	}

	sel := def(TypeSelect, "enterprise", "smb")
	if err := sel.ValidateValue("enterprise"); err != nil {
		t.Fatalf("select in options should pass: %v", err)
	}
	if err := sel.ValidateValue("startup"); !errors.Is(err, ErrValueNotInOptions) {
		t.Fatalf("select out of options should fail: %v", err)
	}

	multi := def(TypeMultiSelect, "a", "b", "c")
	if err := multi.ValidateValue([]any{"a", "c"}); err != nil {
		t.Fatalf("multiselect in options should pass: %v", err)
	}
	if err := multi.ValidateValue([]any{"a", "z"}); !errors.Is(err, ErrValueNotInOptions) {
		t.Fatalf("multiselect out of options should fail: %v", err)
	}
}

func TestValidateValue_RequiredAndNil(t *testing.T) {
	d := def(TypeText)
	d.Required = true
	if err := d.ValidateValue(nil); !errors.Is(err, ErrValueRequired) {
		t.Fatalf("required nil should fail: %v", err)
	}
	d.Required = false
	if err := d.ValidateValue(nil); err != nil {
		t.Fatalf("optional nil should pass: %v", err)
	}
}
