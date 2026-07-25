package template

import (
	"errors"
	"testing"
)

func TestValidateComponent_CallPermissionRequest_Accepted(t *testing.T) {
	if err := ValidateComponent(TemplateComponent{Type: ComponentTypeCallPermissionRequest}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Lowercase input should be normalized and accepted too.
	if err := ValidateComponent(TemplateComponent{Type: "call_permission_request"}); err != nil {
		t.Fatalf("unexpected error for lowercase type: %v", err)
	}
}

func TestValidateComponents_CallPermissionRequest_Valid(t *testing.T) {
	components := []TemplateComponent{
		{Type: "BODY", Text: "Olá, gostaríamos de ligar para confirmar sua consulta. Você autoriza?"},
		{Type: "FOOTER", Text: "Você pode revogar a permissão a qualquer momento."},
		{Type: ComponentTypeCallPermissionRequest},
	}
	if err := ValidateComponents(components); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateComponents_CallPermissionRequest_RequiresBody(t *testing.T) {
	components := []TemplateComponent{
		{Type: ComponentTypeCallPermissionRequest},
	}
	if err := ValidateComponents(components); err == nil {
		t.Fatal("expected error when BODY is missing, got nil")
	}
}

func TestValidateComponents_CallPermissionRequest_RejectsButtons(t *testing.T) {
	components := []TemplateComponent{
		{Type: "BODY", Text: "Podemos te ligar?"},
		{Type: ComponentTypeCallPermissionRequest},
		{Type: "BUTTONS", Buttons: []TemplateButton{{Type: "QUICK_REPLY", Text: "Sim"}}},
	}
	if err := ValidateComponents(components); !errors.Is(err, ErrCallPermissionWithButtons) {
		t.Fatalf("got %v, want ErrCallPermissionWithButtons", err)
	}
}

func TestValidateComponents_CallPermissionRequest_RejectsMultiple(t *testing.T) {
	components := []TemplateComponent{
		{Type: "BODY", Text: "Podemos te ligar?"},
		{Type: ComponentTypeCallPermissionRequest},
		{Type: ComponentTypeCallPermissionRequest},
	}
	if err := ValidateComponents(components); !errors.Is(err, ErrMultipleCallPermissionRequests) {
		t.Fatalf("got %v, want ErrMultipleCallPermissionRequests", err)
	}
}
