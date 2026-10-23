package node_executors

import "vozko/domain/workflow"

func customerTextIn(ctx *workflow.NodeContext) func(string) string {
	return func(text string) string { return workflow.CustomerText(ctx, text) }
}

func customerSegments(ctx *workflow.NodeContext, segments []string) []string {
	out := make([]string, len(segments))
	for i, segment := range segments {
		out[i] = workflow.CustomerText(ctx, segment)
	}
	return out
}
