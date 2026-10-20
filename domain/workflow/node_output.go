package workflow

import "strings"

const (
	StateKeyOutputNode = "_engine_output_node_id"
	lastOutputPrefix   = "_last_"
	nodeOutputPrefix   = "_node_"
)

func nodeOutputKeyPrefix(nodeID string) string {
	return nodeOutputPrefix + nodeID + "_"
}

func RecordNodeOutput(state *RunState, nodeID string, output map[string]interface{}) {
	if state == nil || len(output) == 0 {
		return
	}
	prefix := nodeOutputKeyPrefix(nodeID)
	for k, v := range output {
		state.Set(lastOutputPrefix+k, v)
		state.Set(prefix+k, v)
	}
	state.Set(StateKeyOutputNode, nodeID)
}

func NodeOutput(state *RunState, nodeID string) map[string]interface{} {
	if nodeID == "" {
		return map[string]interface{}{}
	}
	return stateFieldsWithPrefix(state, nodeOutputKeyPrefix(nodeID))
}

func LastNodeOutput(state *RunState) map[string]interface{} {
	if state == nil {
		return map[string]interface{}{}
	}
	if nodeID := state.GetString(StateKeyOutputNode); nodeID != "" {
		return NodeOutput(state, nodeID)
	}
	return stateFieldsWithPrefix(state, lastOutputPrefix)
}

func stateFieldsWithPrefix(state *RunState, prefix string) map[string]interface{} {
	result := make(map[string]interface{})
	if state == nil {
		return result
	}
	for k, v := range state.Vars {
		if strings.HasPrefix(k, prefix) {
			result[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return result
}

func (s *RunState) Lookup(path string) (interface{}, bool) {
	if s == nil {
		return nil, false
	}
	if v, ok := s.Get(path); ok {
		return v, true
	}
	return ResolveVariable(path, s)
}
