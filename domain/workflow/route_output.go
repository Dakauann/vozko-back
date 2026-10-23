package workflow

import (
	"errors"
	"fmt"
	"strings"
)

const defaultOutput = "default"

var ErrOutputNotConnected = errors.New("workflow: the output the node produced is not connected")

type Route struct {
	Target string
	Branch string
}

func RouteOutput(edges []Edge, output string, fallbacks ...string) (Route, error) {
	if len(edges) == 0 {
		return Route{}, nil
	}
	for _, label := range append([]string{output}, fallbacks...) {
		if target := edgeLeaving(edges, label); target != "" {
			return Route{Target: target, Branch: label}, nil
		}
	}
	if len(fallbacks) == 0 {
		return Route{}, fmt.Errorf("%w: %q", ErrOutputNotConnected, output)
	}
	return Route{}, fmt.Errorf("%w: %q, nor its fallback %s", ErrOutputNotConnected, output, strings.Join(quoteAll(fallbacks), ", "))
}

func edgeLeaving(edges []Edge, label string) string {
	if label == "" {
		return ""
	}
	for _, e := range edges {
		if e.Label == label || (label == defaultOutput && e.Label == "") {
			return e.Target
		}
	}
	return ""
}

func quoteAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprintf("%q", v)
	}
	return out
}
