package workflow

import "testing"

func agentDefaultEdgeGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerMessageReceived},
			{ID: "n2", Type: NodeTypeActionAIAgent},
			{ID: "n3", Type: NodeTypeWaitForReply},
			{ID: "n5", Type: NodeTypeActionHTTPRequest},
			{ID: "n7", Type: NodeTypeActionSendText},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{Source: "t", Target: "n2"},
			{Source: "n2", Target: "n3"},
			{Source: "n2", Target: "n5", Label: "buscar_cep"},
			{Source: "n3", Target: "n2", Label: "replied"},
			{Source: "n3", Target: "end", Label: "timeout"},
			{Source: "n5", Target: "n7", Label: "sucesso"},
			{Source: "n7", Target: "end"},
		},
	}
}

func TestDivergenceNamesTheLastNodeThatCouldStillHaveReachedTheTarget(t *testing.T) {
	g := agentDefaultEdgeGraph()
	cases := []struct {
		name     string
		hops     []PathHop
		stop     string
		found    bool
		at       string
		taken    string
		expected string
	}{
		{
			name:     "the agent took its default edge into a wait instead of the tool edge",
			hops:     []PathHop{{NodeID: "t", Next: "n2"}, {NodeID: "n2", Next: "n3"}},
			stop:     "n3",
			found:    true,
			at:       "n2",
			taken:    "",
			expected: "buscar_cep",
		},
		{
			name:  "a path that stopped on its only way to the target has no divergence",
			hops:  []PathHop{{NodeID: "t", Next: "n2"}, {NodeID: "n2", Next: "n5", Branch: "buscar_cep"}},
			stop:  "n5",
			found: false,
		},
		{
			name:  "no steps ran",
			stop:  "t",
			found: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hop, edge, ok := Divergence(&g, tc.hops, "n7", tc.stop)
			if ok != tc.found {
				t.Fatalf("found = %v, want %v", ok, tc.found)
			}
			if !ok {
				return
			}
			if hop.NodeID != tc.at || hop.Branch != tc.taken || edge.Label != tc.expected {
				t.Fatalf("divergence = %+v via %+v", hop, edge)
			}
		})
	}
}

func TestAncestorsAvoidingSkipsThePathsThroughTheStopNode(t *testing.T) {
	g := agentDefaultEdgeGraph()
	if all := g.AncestorsOf("n7"); !all["n3"] {
		t.Fatalf("n3 reaches n7 through the agent: %v", all)
	}
	avoiding := g.AncestorsAvoiding("n7", "n3")
	if avoiding["n3"] || !avoiding["n2"] || !avoiding["n5"] {
		t.Fatalf("ancestors avoiding n3 = %v", avoiding)
	}
}
