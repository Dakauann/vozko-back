package workflow

import "testing"

type catalogNames map[NodeType]string

func (c catalogNames) StepName(t NodeType) string { return c[t] }

func TestANodeIsNamedByTheTeamFirstAndByTheCatalogOtherwise(t *testing.T) {
	steps := catalogNames{NodeTypeActionSendText: "Enviar texto"}
	cases := []struct {
		name string
		node *Node
		want string
	}{
		{"the name the team typed", &Node{ID: "n1", Type: NodeTypeActionSendText, Config: map[string]interface{}{"display_name": " Boas-vindas "}}, "Boas-vindas"},
		{"the catalog label of an unnamed node", &Node{ID: "n1", Type: NodeTypeActionSendText}, "Enviar texto"},
		{"no node, no name", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NodeNameOf(tc.node, steps); got != tc.want {
				t.Fatalf("name = %q, want %q", got, tc.want)
			}
		})
	}
}
