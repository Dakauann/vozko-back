package workspace_repository

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

func TestAllowedWorkspaceResourceTables_RejectsInjectionAttempts(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",
		"users; DROP TABLE workspaces;--",
		"workspaces --",
		"(SELECT id FROM users)",
		"pg_sleep(5)",
		"agents OR 1=1",
		"agents; DELETE FROM agents",
		"random_unknown_table",
		" agents",
		"agents ",
		"AGENTS",
	}
	for _, name := range bad {
		if _, ok := allowedWorkspaceResourceTables[name]; ok {
			t.Errorf("allowlist unexpectedly contains %q — review workspace_repository.go", name)
		}
	}
}

func TestAllowedWorkspaceResourceTables_ContainsKnownGood(t *testing.T) {
	t.Parallel()

	required := []string{
		"agents",
		"campaigns",
		"whatsapp_campaigns",
		"whatsapp_templates",
		"whatsapp_business_phones",
		"sip_trunks",
		"support_inboxes",
		"knowledge_bases",
		"workflows",
		"stages",
		"labels",
		"departments",
		"message_shortcuts",
	}
	for _, name := range required {
		if _, ok := allowedWorkspaceResourceTables[name]; !ok {
			t.Errorf("allowlist missing required table %q", name)
		}
	}
}

func TestGetWorkspaceIDForResource_DisallowedTableFailsClosed(t *testing.T) {
	t.Parallel()

	r := &repository{db: nil}
	cases := []string{
		"users; DROP TABLE workspaces;--",
		"(SELECT 1)",
		"totally_unknown_table",
		"",
	}
	for _, name := range cases {
		_, err := r.GetWorkspaceIDForResource(name, "any-id")
		if !errors.Is(err, workspace.ErrWorkspaceNotFound) {
			t.Errorf("resourceTable=%q: expected ErrWorkspaceNotFound, got %v", name, err)
		}
	}
}
