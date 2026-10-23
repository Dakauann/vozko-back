package workflow

import "testing"

func TestAncestorDistancesCountTheShortestHopsBackToEachAncestor(t *testing.T) {
	g := &Graph{Edges: []Edge{
		{Source: "t", Target: "a"}, {Source: "a", Target: "b"}, {Source: "t", Target: "b"},
		{Source: "b", Target: "c"}, {Source: "c", Target: "b"}, {Source: "x", Target: "a"},
	}}
	got := g.AncestorDistances("c", "")
	want := map[string]int{"c": 0, "b": 1, "a": 2, "t": 2, "x": 3}
	if len(got) != len(want) {
		t.Fatalf("distances = %v, want %v", got, want)
	}
	for id, d := range want {
		if got[id] != d {
			t.Fatalf("distances = %v, want %v", got, want)
		}
	}
	if avoided := g.AncestorsAvoiding("c", "a"); avoided["x"] || !avoided["t"] || avoided["c"] {
		t.Fatalf("ancestors avoiding a = %v", avoided)
	}
}
