package workflow

import "testing"

func TestANamedNodeIsNotFlaggedForAnUnknownConfigKey(t *testing.T) {
	defs := NodeCatalogMap([]NodeDefinition{{
		Type:         NodeTypeActionSendText,
		ConfigSchema: []ConfigField{{Key: "text", Type: "textarea"}},
	}})
	g := &Graph{Nodes: []Node{{ID: "n1", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "Oi", "display_name": "Boas-vindas"}}}}
	var issues []LintIssue
	lintNodeShape(g, defs, func(i LintIssue) { issues = append(issues, i) })

	for _, i := range issues {
		if i.Code == LintUnknownConfigKey {
			t.Fatalf("the node name is common node metadata, got %+v", i)
		}
	}
}

func TestAnUndeclaredKeyBesideTheNodeNameIsStillFlagged(t *testing.T) {
	defs := NodeCatalogMap([]NodeDefinition{{
		Type:         NodeTypeActionSendText,
		ConfigSchema: []ConfigField{{Key: "text", Type: "textarea"}},
	}})
	g := &Graph{Nodes: []Node{{ID: "n1", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "Oi", "display_name": "Boas-vindas", "txt": "x"}}}}
	var flagged []string
	lintNodeShape(g, defs, func(i LintIssue) {
		if i.Code == LintUnknownConfigKey {
			flagged = append(flagged, i.Field)
		}
	})

	if len(flagged) != 1 || flagged[0] != "txt" {
		t.Fatalf("flagged = %v, want only the typo key", flagged)
	}
}

func TestTheCommonNodeConfigKeysAreOnlyRealNodeMetadata(t *testing.T) {
	cases := []struct {
		key    string
		common bool
	}{
		{"display_name", true},
		{"text", false},
		{"color", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsCommonNodeConfigKey(c.key); got != c.common {
			t.Errorf("IsCommonNodeConfigKey(%q) = %v, want %v", c.key, got, c.common)
		}
	}
}

func TestTheNodeNameReadsTheSharedDisplayNameKey(t *testing.T) {
	n := Node{Config: map[string]interface{}{CommonNodeConfigKeys()[0]: "  Bia chama matrícula "}}
	if got := n.DisplayName(); got != "Bia chama matrícula" {
		t.Fatalf("DisplayName() = %q", got)
	}
}
