package pipeline

import (
	"errors"
	"testing"
)

func TestPipelineNormalizeDefaultsObjectType(t *testing.T) {
	p := &Pipeline{WorkspaceID: "ws1", Name: "  Vendas  "}
	p.Normalize()
	if p.Name != "Vendas" {
		t.Fatalf("expected trimmed name %q, got %q", "Vendas", p.Name)
	}
	if p.ObjectType != ObjectConversation {
		t.Fatalf("expected default object type %q, got %q", ObjectConversation, p.ObjectType)
	}
}

func TestPipelineValidate(t *testing.T) {
	cases := []struct {
		name    string
		p       Pipeline
		wantErr error
	}{
		{
			name:    "missing workspace",
			p:       Pipeline{Name: "Vendas", ObjectType: ObjectConversation},
			wantErr: ErrWorkspaceRequired,
		},
		{
			name:    "missing name",
			p:       Pipeline{WorkspaceID: "ws1", ObjectType: ObjectConversation},
			wantErr: ErrNameRequired,
		},
		{
			name:    "invalid object type",
			p:       Pipeline{WorkspaceID: "ws1", Name: "Vendas", ObjectType: "lead"},
			wantErr: ErrInvalidObject,
		},
		{
			name:    "valid conversation",
			p:       Pipeline{WorkspaceID: "ws1", Name: "Atendimento", ObjectType: ObjectConversation},
			wantErr: nil,
		},
		{
			name:    "valid opportunity",
			p:       Pipeline{WorkspaceID: "ws1", Name: "Vendas", ObjectType: ObjectOpportunity},
			wantErr: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
