package workflow

import (
	"errors"
	"testing"
)

func TestRouteOutputNeverTakesAnArbitraryEdge(t *testing.T) {
	for _, c := range []struct {
		name      string
		edges     []Edge
		output    string
		fallbacks []string
		want      string
		wantErr   error
	}{
		{"the produced output", []Edge{{Target: "ok", Label: "sucesso"}, {Target: "bad", Label: "erro"}}, "erro", nil, "bad", nil},
		{"the declared fallback when the output is not connected", []Edge{{Target: "a", Label: "1"}, {Target: "nm", Label: "no_match"}}, "7", []string{"no_match"}, "nm", nil},
		{"the first connected fallback in declared order", []Edge{{Target: "d", Label: "default"}, {Target: "nm", Label: "no_match"}}, "7", []string{"no_match", "default"}, "nm", nil},
		{"an unlabeled edge is the default output, as the editor draws it", []Edge{{Target: "plain", Label: ""}}, "default", nil, "plain", nil},
		{"an unlabeled edge is not any other output", []Edge{{Target: "plain", Label: ""}}, "sucesso", nil, "", ErrOutputNotConnected},
		{"the first edge is never a fallback", []Edge{{Target: "ok", Label: "sucesso"}}, "erro", nil, "", ErrOutputNotConnected},
		{"a fallback that is not connected does not help", []Edge{{Target: "a", Label: "1"}}, "7", []string{"no_match"}, "", ErrOutputNotConnected},
		{"a node without edges ends its path", nil, "sucesso", nil, "", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := RouteOutput(c.edges, c.output, c.fallbacks...)

			if !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("target = %q, want %q", got, c.want)
			}
		})
	}
}
