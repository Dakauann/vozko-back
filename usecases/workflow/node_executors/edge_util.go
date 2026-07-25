package node_executors

import "vozko/domain/workflow"

func resolveEdgeByLabel(edges []workflow.Edge, label string) string {
	if label != "" {
		for _, e := range edges {
			if e.Label == label {
				return e.Target
			}
		}
	}
	if len(edges) > 0 {
		return edges[0].Target
	}
	return ""
}

func resolveEdgeByLabelStrict(edges []workflow.Edge, label string) string {
	for _, e := range edges {
		if e.Label == label {
			return e.Target
		}
	}
	return ""
}
