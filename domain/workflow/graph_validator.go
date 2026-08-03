package workflow

import (
	"fmt"
	"strconv"
	"strings"
)

func ValidateGraph(g *Graph, wfType WorkflowType, skipOutgoingCheck ...map[string]bool) error {
	exempt := map[string]bool{}
	if len(skipOutgoingCheck) > 0 && skipOutgoingCheck[0] != nil {
		exempt = skipOutgoingCheck[0]
	}
	if len(g.Nodes) == 0 {
		return ErrGraphEmpty
	}
	if len(g.Nodes) > MaxNodesPerWorkflow {
		return ErrGraphTooManyNodes
	}
	if len(g.Edges) > MaxEdgesPerWorkflow {
		return ErrGraphTooManyEdges
	}
	if !wfType.Valid() {
		return ErrInvalidWorkflowType
	}

	nodeMap := make(map[string]*Node, len(g.Nodes))
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if _, exists := nodeMap[n.ID]; exists {
			return fmt.Errorf("%w: %s", ErrGraphDuplicateNodeID, n.ID)
		}
		if !n.Type.Valid() {
			return fmt.Errorf("%w: %s", ErrInvalidNodeType, n.Type)
		}
		nodeMap[n.ID] = n
	}

	for _, e := range g.Edges {
		if _, ok := nodeMap[e.Source]; !ok {
			return fmt.Errorf("%w: source %s", ErrGraphInvalidEdgeRef, e.Source)
		}
		if _, ok := nodeMap[e.Target]; !ok {
			return fmt.Errorf("%w: target %s", ErrGraphInvalidEdgeRef, e.Target)
		}
	}

	var triggers []*Node
	hasEnd := false
	for _, n := range nodeMap {
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
		return ErrGraphNoTrigger
	}
	if !hasEnd {
		return ErrGraphNoEndNode
	}

	seenTriggerTypes := make(map[TriggerType]bool, len(triggers))
	for _, triggerNode := range triggers {
		tt := TriggerType(triggerNode.Type)
		if !tt.Valid() {
			return fmt.Errorf("%w: %s", ErrInvalidTriggerType, tt)
		}
		if tt.WorkflowType() != wfType {
			return fmt.Errorf("%w: trigger %s not allowed in %s workflow", ErrGraphTriggerIncompatibleWithType, tt, wfType)
		}
		if seenTriggerTypes[tt] {
			return fmt.Errorf("%w: %s", ErrGraphDuplicateTriggerType, tt)
		}
		seenTriggerTypes[tt] = true
	}

	outgoing := make(map[string][]string)
	incoming := make(map[string][]string)
	for _, e := range g.Edges {
		outgoing[e.Source] = append(outgoing[e.Source], e.Target)
		incoming[e.Target] = append(incoming[e.Target], e.Source)
	}

	for _, triggerNode := range triggers {
		if len(incoming[triggerNode.ID]) > 0 {
			return fmt.Errorf("workflow: trigger node %s must not have incoming edges", triggerNode.ID)
		}
	}

	// At least one trigger must actually feed the flow. A trigger whose only (or
	// zero) outgoing edges go to decoration nodes is a dangling entry point, the
	// workflow can never start from it. The AI commonly drops a trigger node and
	// forgets to wire it; the generic per-node "no outgoing" rule below also
	// rejects a wholly-unwired single trigger, but this fires first with a precise,
	// trigger-specific error the builder can act on directly.
	triggerConnected := false
	for _, triggerNode := range triggers {
		for _, target := range outgoing[triggerNode.ID] {
			if tn, ok := nodeMap[target]; ok && tn.Type.Category() != NodeCategoryDecoration {
				triggerConnected = true
				break
			}
		}
		if triggerConnected {
			break
		}
	}
	if !triggerConnected {
		return ErrGraphTriggerNotConnected
	}

	for id, n := range nodeMap {
		if n.Type.Category() == NodeCategoryDecoration {
			continue
		}
		if !n.Type.IsTrigger() && len(incoming[id]) == 0 {
			// Quote the id (like the config/edge errors) so the lint can lift it
			// into LintIssue.NodeID and the editor can offer "Ver no fluxo".
			return fmt.Errorf("%w: %q", ErrGraphNodeNoIncoming, id)
		}
		if !n.Type.IsEnd() && !exempt[id] && len(outgoing[id]) == 0 {
			return fmt.Errorf("%w: %q", ErrGraphNodeNoOutgoing, id)
		}
	}

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
	for id, node := range nodeMap {
		if node.Type.Category() == NodeCategoryDecoration {
			continue
		}
		if !visited[id] {
			return fmt.Errorf("%w: %q", ErrGraphOrphanNode, id)
		}
	}

	if err := validateCycles(nodeMap, outgoing); err != nil {
		return err
	}

	return nil
}

// TODO: make it validate passed agent id on ai agent nodes, validate template id, etcetc

type ConfigValidator interface {
	Validate(n *Node) error
}

func ValidateNodeConfigs(g *Graph, catalog []NodeDefinition, validators ...ConfigValidator) error {
	defMap := make(map[NodeType]NodeDefinition, len(catalog))
	for _, d := range catalog {
		defMap[d.Type] = d
	}

	for i := range g.Nodes {
		n := &g.Nodes[i]
		def, ok := defMap[n.Type]
		if !ok || len(def.ConfigSchema) == 0 {
			continue
		}
		for _, field := range def.ConfigSchema {
			if field.Required {
				val, exists := n.Config[field.Key]
				if !exists || isEmpty(val) {
					return fmt.Errorf("%w: node %q (%s) field %q", ErrNodeMissingRequiredField, n.ID, n.Type, field.Key)
				}
			}
			// Numeric range bounds (range/number fields declare Min/Max): a value
			// that's present and statically numeric must fall within [Min, Max].
			// PURE + schema-driven, so the builder lint AND activation reject an
			// out-of-range value, the AI sees it instead of failing at run time.
			if field.Min != nil || field.Max != nil {
				if raw, exists := n.Config[field.Key]; exists {
					if num, ok := numericConfigValue(raw); ok {
						if field.Min != nil && num < *field.Min {
							return fmt.Errorf("%w: node %q (%s) field %q = %v (mínimo %v)", ErrNodeFieldOutOfRange, n.ID, n.Type, field.Key, num, *field.Min)
						}
						if field.Max != nil && num > *field.Max {
							return fmt.Errorf("%w: node %q (%s) field %q = %v (máximo %v)", ErrNodeFieldOutOfRange, n.ID, n.Type, field.Key, num, *field.Max)
						}
					}
				}
			}
		}

		for _, v := range validators {
			if err := v.Validate(n); err != nil {
				return err
			}
		}
	}
	return nil
}

// numericConfigValue coerces a config value to a float64 when it is statically
// numeric (a JSON number, a Go numeric, or a numeric string). It returns ok=false
// for empty/non-numeric/interpolated ("{{…}}") values, which can't be range-checked
// statically and are left to run time.
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
			if nt.IsWait() || nt == NodeTypeActionLoop {
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
