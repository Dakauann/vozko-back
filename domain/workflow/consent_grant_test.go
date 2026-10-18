package workflow

import (
	"slices"
	"testing"
)

func consentNode(id, mode, purpose string) Node {
	return Node{ID: id, Type: NodeTypeActionConsent, Config: map[string]interface{}{ConsentModeKey: mode, ConsentPurposeKey: purpose, ConsentNoteKey: ""}}
}

func graphOf(nodes ...Node) *Graph {
	return &Graph{Nodes: nodes}
}

func TestConsentGrantNodesListsOnlyRegistrarNodes(t *testing.T) {
	g := graphOf(
		Node{ID: "trigger", Type: NodeTypeTriggerMessageReceived},
		consentNode("grant", ConsentModeGrant, "ofertas"),
		consentNode("spaced", " registrar ", ""),
		consentNode("withdraw", ConsentModeWithdraw, ""),
		consentNode("undecided", "", ""),
	)
	got := ConsentGrantNodes(g)
	if !slices.Equal(got, []string{"grant", "spaced"}) {
		t.Fatalf("grant nodes = %v", got)
	}
	if !GrantsConsent(g) {
		t.Fatal("a graph with a registrar node grants consent")
	}
	if GrantsConsent(graphOf(consentNode("withdraw", ConsentModeWithdraw, ""))) || GrantsConsent(nil) {
		t.Fatal("a graph without a registrar node grants nothing")
	}
}

func TestChangedConsentGrantsComparesEachRegistrarNodeWithTheStoredGraph(t *testing.T) {
	stored := graphOf(
		consentNode("kept", ConsentModeGrant, "ofertas"),
		consentNode("repurposed", ConsentModeGrant, "ofertas"),
		consentNode("was_withdraw", ConsentModeWithdraw, ""),
		consentNode("removed", ConsentModeGrant, "ofertas"),
	)
	moved := consentNode("kept", ConsentModeGrant, "ofertas")
	moved.Position = Position{X: 400, Y: 80}
	cases := []struct {
		name     string
		stored   *Graph
		proposed *Graph
		want     []string
	}{
		{"nothing stored counts every grant", nil, graphOf(consentNode("a", ConsentModeGrant, ""), consentNode("b", ConsentModeWithdraw, "")), []string{"a"}},
		{"an unchanged grant, even moved on the canvas", stored, graphOf(moved), nil},
		{"a new purpose", stored, graphOf(consentNode("repurposed", ConsentModeGrant, "cobranca")), []string{"repurposed"}},
		{"a withdrawal turned into a grant", stored, graphOf(consentNode("was_withdraw", ConsentModeGrant, "")), []string{"was_withdraw"}},
		{"a new node", stored, graphOf(consentNode("fresh", ConsentModeGrant, "ofertas")), []string{"fresh"}},
		{"a removed grant and a new withdrawal", stored, graphOf(consentNode("other", ConsentModeWithdraw, "")), nil},
		{"a config key added to a grant", stored, graphOf(Node{ID: "kept", Type: NodeTypeActionConsent, Config: map[string]interface{}{
			ConsentModeKey: ConsentModeGrant, ConsentPurposeKey: "ofertas", ConsentNoteKey: "", "extra": "x"}}), []string{"kept"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ChangedConsentGrants(tc.stored, tc.proposed)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("changed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChangedConsentGrantsTreatsAnotherNodeTypeWithTheSameIDAsNew(t *testing.T) {
	stored := graphOf(Node{ID: "n1", Type: NodeTypeEnd, Config: map[string]interface{}{ConsentModeKey: ConsentModeGrant}})
	if got := ChangedConsentGrants(stored, graphOf(consentNode("n1", ConsentModeGrant, ""))); !slices.Equal(got, []string{"n1"}) {
		t.Fatalf("changed = %v", got)
	}
}
