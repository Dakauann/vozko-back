package workflow

import (
	"fmt"
	"strconv"
	"strings"
)

func ValidateGraph(g *Graph, wfType WorkflowType, skipOutgoingCheck ...map[string]bool) error {
	var exempt map[string]bool
	if len(skipOutgoingCheck) > 0 {
		exempt = skipOutgoingCheck[0]
	}
	return firstProblem(GraphProblems(g, wfType, exempt))
}

func firstProblem(problems []error) error {
	if len(problems) == 0 {
		return nil
	}
	return problems[0]
}

func GraphProblems(g *Graph, wfType WorkflowType, exempt map[string]bool) []error {
	if len(g.Nodes) == 0 {
		return []error{ErrGraphEmpty}
	}
	if len(g.Nodes) > MaxNodesPerWorkflow {
		return []error{ErrGraphTooManyNodes}
	}
	if len(g.Edges) > MaxEdgesPerWorkflow {
		return []error{ErrGraphTooManyEdges}
	}
	if !wfType.Valid() {
		return []error{ErrInvalidWorkflowType}
	}

	nodeMap, problems := graphShapeProblems(g)
	if len(problems) > 0 {
		return problems
	}

	var triggers []*Node
	hasEnd := false
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type.Category() == NodeCategoryDecoration {
			continue
		}
		if n.Type.IsTrigger() {
			triggers = append(triggers, n)
		}
		if n.Type.IsEnd() {
			hasEnd = true
		}
	}
	if len(triggers) == 0 {
		problems = append(problems, ErrGraphNoTrigger)
	}
	if !hasEnd {
		problems = append(problems, ErrGraphNoEndNode)
	}
	if len(triggers) == 0 {
		return problems
	}

	problems = append(problems, triggerTypeProblems(triggers, wfType)...)

	outgoing := make(map[string][]string)
	incoming := make(map[string][]string)
	for _, e := range g.Edges {
		outgoing[e.Source] = append(outgoing[e.Source], e.Target)
		incoming[e.Target] = append(incoming[e.Target], e.Source)
	}
	problems = append(problems, connectionProblems(g, nodeMap, triggers, outgoing, incoming, exempt)...)

	if hasEnd {
		if err := validateCycles(nodeMap, outgoing); err != nil {
			problems = append(problems, err)
		}
	}
	return problems
}

func graphShapeProblems(g *Graph) (map[string]*Node, []error) {
	var problems []error
	nodeMap := make(map[string]*Node, len(g.Nodes))
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if _, exists := nodeMap[n.ID]; exists {
			problems = append(problems, fmt.Errorf("%w: %s", ErrGraphDuplicateNodeID, n.ID))
			continue
		}
		if !n.Type.Valid() {
			problems = append(problems, fmt.Errorf("%w: %s", ErrInvalidNodeType, n.Type))
		}
		nodeMap[n.ID] = n
	}
	for _, e := range g.Edges {
		if _, ok := nodeMap[e.Source]; !ok {
			problems = append(problems, fmt.Errorf("%w: source %s", ErrGraphInvalidEdgeRef, e.Source))
		}
		if _, ok := nodeMap[e.Target]; !ok {
			problems = append(problems, fmt.Errorf("%w: target %s", ErrGraphInvalidEdgeRef, e.Target))
		}
	}
	return nodeMap, problems
}

func triggerTypeProblems(triggers []*Node, wfType WorkflowType) []error {
	var problems []error
	seen := make(map[TriggerType]bool, len(triggers))
	for _, triggerNode := range triggers {
		tt := TriggerType(triggerNode.Type)
		switch {
		case !tt.Valid():
			problems = append(problems, fmt.Errorf("%w: %s", ErrInvalidTriggerType, tt))
		case tt.WorkflowType() != wfType:
			problems = append(problems, fmt.Errorf("%w: trigger %s not allowed in %s workflow", ErrGraphTriggerIncompatibleWithType, tt, wfType))
		case seen[tt]:
			problems = append(problems, fmt.Errorf("%w: %s", ErrGraphDuplicateTriggerType, tt))
		}
		seen[tt] = true
	}
	return problems
}

func connectionProblems(g *Graph, nodeMap map[string]*Node, triggers []*Node, outgoing, incoming map[string][]string, exempt map[string]bool) []error {
	var problems []error
	for _, triggerNode := range triggers {
		if len(incoming[triggerNode.ID]) > 0 {
			problems = append(problems, fmt.Errorf("%w: %q", ErrGraphTriggerHasIncoming, triggerNode.ID))
		}
	}
	notConnected := !anyTriggerConnected(triggers, nodeMap, outgoing)
	if notConnected {
		problems = append(problems, fmt.Errorf("%w: %q", ErrGraphTriggerNotConnected, triggers[0].ID))
	}

	withoutIncoming := make(map[string]bool)
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type.Category() == NodeCategoryDecoration {
			continue
		}
		if !n.Type.IsTrigger() && len(incoming[n.ID]) == 0 {
			withoutIncoming[n.ID] = true
			problems = append(problems, fmt.Errorf("%w: %q", ErrGraphNodeNoIncoming, n.ID))
		}
		if !n.Type.IsEnd() && !exempt[n.ID] && len(outgoing[n.ID]) == 0 && !(notConnected && n.Type.IsTrigger()) {
			problems = append(problems, fmt.Errorf("%w: %q", ErrGraphNodeNoOutgoing, n.ID))
		}
	}

	visited := reachableFromTriggers(triggers, nodeMap, outgoing)
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type.Category() == NodeCategoryDecoration || visited[n.ID] || withoutIncoming[n.ID] {
			continue
		}
		problems = append(problems, fmt.Errorf("%w: %q", ErrGraphOrphanNode, n.ID))
	}
	return problems
}

func anyTriggerConnected(triggers []*Node, nodeMap map[string]*Node, outgoing map[string][]string) bool {
	for _, triggerNode := range triggers {
		for _, target := range outgoing[triggerNode.ID] {
			if tn, ok := nodeMap[target]; ok && tn.Type.Category() != NodeCategoryDecoration {
				return true
			}
		}
	}
	return false
}

func reachableFromTriggers(triggers []*Node, nodeMap map[string]*Node, outgoing map[string][]string) map[string]bool {
	visited := make(map[string]bool)
	var queue []string
	for _, t := range triggers {
		if !visited[t.ID] {
			visited[t.ID] = true
			queue = append(queue, t.ID)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range outgoing[cur] {
			if node, ok := nodeMap[next]; ok && node.Type.Category() == NodeCategoryDecoration {
				continue
			}
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return visited
}

type ConfigValidator interface {
	Validate(n *Node) error
}

func ValidateNodeConfigs(g *Graph, catalog []NodeDefinition, validators ...ConfigValidator) error {
	return firstProblem(append(NodeConfigProblems(g, catalog), ReferenceProblems(g, catalog, validators...)...))
}

func NodeConfigProblems(g *Graph, catalog []NodeDefinition) []error {
	var problems []error
	eachConfiguredNode(g, catalog, func(n *Node, def NodeDefinition) {
		for _, field := range def.ConfigSchema {
			if err := fieldProblem(n, field); err != nil {
				problems = append(problems, err)
			}
		}
	})
	return problems
}

func ReferenceProblems(g *Graph, catalog []NodeDefinition, validators ...ConfigValidator) []error {
	var problems []error
	eachConfiguredNode(g, catalog, func(n *Node, _ NodeDefinition) {
		for _, v := range validators {
			if err := v.Validate(n); err != nil {
				problems = append(problems, err)
			}
		}
	})
	return problems
}

func eachConfiguredNode(g *Graph, catalog []NodeDefinition, visit func(n *Node, def NodeDefinition)) {
	defMap := NodeCatalogMap(catalog)
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if def, ok := defMap[n.Type]; ok && len(def.ConfigSchema) > 0 {
			visit(n, def)
		}
	}
}

func fieldProblem(n *Node, field ConfigField) error {
	if field.RequiredFor(n.Config) {
		if val, exists := n.Config[field.Key]; !exists || isEmpty(val) {
			return fmt.Errorf("%w: node %q (%s) field %q", ErrNodeMissingRequiredField, n.ID, n.Type, field.Key)
		}
	}
	if field.Min == nil && field.Max == nil {
		return nil
	}
	raw, exists := n.Config[field.Key]
	if !exists {
		return nil
	}
	num, ok := numericConfigValue(raw)
	if !ok {
		return nil
	}
	if field.Min != nil && num < *field.Min {
		return fmt.Errorf("%w: node %q (%s) field %q = %v (mínimo %v)", ErrNodeFieldOutOfRange, n.ID, n.Type, field.Key, num, *field.Min)
	}
	if field.Max != nil && num > *field.Max {
		return fmt.Errorf("%w: node %q (%s) field %q = %v (máximo %v)", ErrNodeFieldOutOfRange, n.ID, n.Type, field.Key, num, *field.Max)
	}
	return nil
}

func numericConfigValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case string:
		s := strings.TrimSpace(n)
		if s == "" || strings.Contains(s, "{{") {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
	return 0, false
}

func isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

func validateCycles(nodeMap map[string]*Node, outgoing map[string][]string) error {
	components := stronglyConnectedComponents(nodeMap, outgoing)
	for _, component := range components {
		if !isCycleComponent(component, outgoing) {
			continue
		}

		hasWaitOrLoop := false
		for _, nodeID := range component {
			nt := nodeMap[nodeID].Type
			if nt.IsWait() || nt.WaitsForCaller() || nt == NodeTypeActionLoop {
				hasWaitOrLoop = true
				break
			}
		}
		if !hasWaitOrLoop {
			return ErrGraphCycleDetected
		}

		canExitToEnd := false
		for _, nodeID := range component {
			if canReachEnd(nodeID, nodeMap, outgoing) {
				canExitToEnd = true
				break
			}
		}
		if !canExitToEnd {
			return ErrGraphCycleDetected
		}
	}

	return nil
}

func canReachEnd(start string, nodeMap map[string]*Node, outgoing map[string][]string) bool {
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if nodeMap[current].Type.IsEnd() {
			return true
		}
		for _, next := range outgoing[current] {
			if visited[next] {
				continue
			}
			visited[next] = true
			queue = append(queue, next)
		}
	}
	return false
}

func isCycleComponent(component []string, outgoing map[string][]string) bool {
	if len(component) > 1 {
		return true
	}
	nodeID := component[0]
	for _, next := range outgoing[nodeID] {
		if next == nodeID {
			return true
		}
	}
	return false
}

func stronglyConnectedComponents(nodeMap map[string]*Node, outgoing map[string][]string) [][]string {
	index := 0
	stack := make([]string, 0, len(nodeMap))
	onStack := make(map[string]bool, len(nodeMap))
	indexes := make(map[string]int, len(nodeMap))
	lowlink := make(map[string]int, len(nodeMap))
	components := make([][]string, 0)

	for nodeID := range nodeMap {
		indexes[nodeID] = -1
	}

	var strongConnect func(string)
	strongConnect = func(nodeID string) {
		indexes[nodeID] = index
		lowlink[nodeID] = index
		index++
		stack = append(stack, nodeID)
		onStack[nodeID] = true

		for _, next := range outgoing[nodeID] {
			if indexes[next] == -1 {
				strongConnect(next)
				if lowlink[next] < lowlink[nodeID] {
					lowlink[nodeID] = lowlink[next]
				}
			} else if onStack[next] && indexes[next] < lowlink[nodeID] {
				lowlink[nodeID] = indexes[next]
			}
		}

		if lowlink[nodeID] != indexes[nodeID] {
			return
		}

		component := make([]string, 0)
		for {
			lastIndex := len(stack) - 1
			last := stack[lastIndex]
			stack = stack[:lastIndex]
			onStack[last] = false
			component = append(component, last)
			if last == nodeID {
				break
			}
		}
		components = append(components, component)
	}

	for nodeID := range nodeMap {
		if indexes[nodeID] == -1 {
			strongConnect(nodeID)
		}
	}

	return components
}
