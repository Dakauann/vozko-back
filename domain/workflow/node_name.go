package workflow

func NodeNameOf(n *Node, steps StepNamer) string {
	if n == nil {
		return ""
	}
	if name := n.DisplayName(); name != "" {
		return name
	}
	return steps.StepName(n.Type)
}
