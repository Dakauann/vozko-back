package node_executors

import (
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestRouteOutputFollowsTheOutputOrTheDeclaredFallbackOrFailsTheStep(t *testing.T) {
	edges := []workflow.Edge{{Source: "n", Target: "ok", Label: "sucesso"}, {Source: "n", Target: "nm", Label: "no_match"}}
	for _, c := range []struct {
		name      string
		output    string
		fallbacks []string
		next      string
		failed    bool
	}{
		{"matched", "sucesso", nil, "ok", false},
		{"unmatched with a connected fallback", "7", []string{"no_match"}, "nm", false},
		{"unmatched without a fallback", "erro", nil, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			result := routeOutput(&workflow.NodeResult{Output: map[string]interface{}{"error": "a causa"}}, edges, c.output, c.fallbacks...)

			if result.NextNodeID != c.next {
				t.Fatalf("next = %q, want %q", result.NextNodeID, c.next)
			}
			if !c.failed {
				if result.Error != "" {
					t.Fatalf("error = %q, want none", result.Error)
				}
				return
			}
			if result.ErrorKind != workflow.ErrorKindConfig || !strings.Contains(result.Error, workflow.ErrOutputNotConnected.Error()) || !strings.Contains(result.Error, "a causa") {
				t.Fatalf("result = %+v, want a config failure naming the unconnected output and the node's own error", result)
			}
		})
	}
}
