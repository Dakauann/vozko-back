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
		want      Route
		wantErr   error
	}{
		{"the produced output", []Edge{{Target: "ok", Label: "sucesso"}, {Target: "bad", Label: "erro"}}, "erro", nil, Route{Target: "bad", Branch: "erro"}, nil},
		{"the declared fallback when the output is not connected", []Edge{{Target: "a", Label: "1"}, {Target: "nm", Label: "no_match"}}, "7", []string{"no_match"}, Route{Target: "nm", Branch: "no_match"}, nil},
		{"the first connected fallback in declared order", []Edge{{Target: "d", Label: "default"}, {Target: "nm", Label: "no_match"}}, "7", []string{"no_match", "default"}, Route{Target: "nm", Branch: "no_match"}, nil},
		{"an unlabeled edge is the default output, as the editor draws it", []Edge{{Target: "plain", Label: ""}}, "default", nil, Route{Target: "plain", Branch: "default"}, nil},
		{"an unlabeled edge is not any other output", []Edge{{Target: "plain", Label: ""}}, "sucesso", nil, Route{}, ErrOutputNotConnected},
		{"the first edge is never a fallback", []Edge{{Target: "ok", Label: "sucesso"}}, "erro", nil, Route{}, ErrOutputNotConnected},
		{"a fallback that is not connected does not help", []Edge{{Target: "a", Label: "1"}}, "7", []string{"no_match"}, Route{}, ErrOutputNotConnected},
		{"a node without edges ends its path", nil, "sucesso", nil, Route{}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := RouteOutput(c.edges, c.output, c.fallbacks...)

			if !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("route = %+v, want %+v", got, c.want)
			}
		})
	}
}
