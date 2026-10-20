package workflow

type PathHop struct {
	NodeID string
	Next   string
	Branch string
}

func Divergence(g *Graph, hops []PathHop, targetNodeID, stopNodeID string) (PathHop, Edge, bool) {
	reaches := g.AncestorsAvoiding(targetNodeID, stopNodeID)
	reaches[targetNodeID] = true
	delete(reaches, stopNodeID)
	for i := len(hops) - 1; i >= 0; i-- {
		hop := hops[i]
		if reaches[hop.Next] {
			continue
		}
		for _, e := range g.OutgoingEdges(hop.NodeID) {
			if e.Target != hop.Next && reaches[e.Target] {
				return hop, e, true
			}
		}
	}
	return PathHop{}, Edge{}, false
}
