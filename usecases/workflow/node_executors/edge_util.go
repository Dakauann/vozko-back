package node_executors

import (
	"strings"

	"vozko/domain/workflow"
)

func resolveEdgeByLabelStrict(edges []workflow.Edge, label string) string {
	for _, e := range edges {
		if e.Label == label {
			return e.Target
		}
	}
	return ""
}

const unroutedCauseMaxLen = 500

const (
	outputFailure = "falha"
	outputTimeout = "timeout"
)

func timeoutFallback(output string) []string {
	if output == outputTimeout {
		return []string{outputFailure}
	}
	return nil
}

func outgoingEdges(ctx *workflow.NodeContext) []workflow.Edge {
	if ctx == nil || ctx.Graph == nil || ctx.Node == nil {
		return nil
	}
	return ctx.Graph.OutgoingEdges(ctx.Node.ID)
}

func routeOutput(result *workflow.NodeResult, edges []workflow.Edge, output string, fallbacks ...string) *workflow.NodeResult {
	next, err := workflow.RouteOutput(edges, output, fallbacks...)
	if err == nil {
		result.NextNodeID = next
		return result
	}
	result.NextNodeID = ""
	result.Error = err.Error()
	if cause, _ := result.Output["error"].(string); strings.TrimSpace(cause) != "" {
		result.Error += ": " + truncate(cause, unroutedCauseMaxLen)
	}
	result.ErrorKind = workflow.ErrorKindConfig
	return result
}
