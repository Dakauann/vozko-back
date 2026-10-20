package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type structuredConfigKey struct {
	key  string
	read func(raw []byte) (interface{}, error)
}

func readAs[T any](raw []byte) (interface{}, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

var structuredConfigKeys = map[NodeType][]structuredConfigKey{
	NodeTypeActionSendInteractive: {
		{key: interactiveButtonsKey, read: readAs[[]interactiveButtonJSON]},
		{key: interactiveSectionsKey, read: readAs[[]interactiveListSectionJSON]},
	},
}

func CanonicalizeStructuredConfigs(g *Graph) bool {
	changed := false
	for i := range g.Nodes {
		if CanonicalizeNodeConfig(&g.Nodes[i]) {
			changed = true
		}
	}
	return changed
}

func CanonicalizeNodeConfig(n *Node) bool {
	changed := false
	for _, shape := range structuredConfigKeys[n.Type.Canonical()] {
		canonical, present, err := shape.canonical(n.Config)
		if err != nil || !present {
			continue
		}
		if current, isString := n.Config[shape.key].(string); isString && current == canonical {
			continue
		}
		n.Config[shape.key] = canonical
		changed = true
	}
	return changed
}

func StructuredConfigProblems(g *Graph) []error {
	var problems []error
	for i := range g.Nodes {
		n := &g.Nodes[i]
		for _, shape := range structuredConfigKeys[n.Type.Canonical()] {
			if _, _, err := shape.canonical(n.Config); err != nil {
				problems = append(problems, fmt.Errorf("%w: node %q field %q: %s", ErrNodeUnreadableConfig, n.ID, shape.key, strings.ReplaceAll(err.Error(), `"`, "'")))
			}
		}
	}
	return problems
}

func (s structuredConfigKey) canonical(config map[string]interface{}) (string, bool, error) {
	raw, err := structuredConfigJSON(config[s.key])
	if err != nil || raw == nil {
		return "", false, err
	}
	value, err := s.read(raw)
	if err != nil {
		return "", false, err
	}
	encoded, err := encodeCanonicalJSON(value)
	if err != nil {
		return "", false, err
	}
	return encoded, true, nil
}

func structuredConfigJSON(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, nil
		}
		return []byte(trimmed), nil
	case []interface{}:
		return json.Marshal(v)
	default:
		return nil, ErrInteractiveOptionsNotAList
	}
}

func decodeStructuredConfig(config map[string]interface{}, key string, target interface{}) error {
	raw, err := structuredConfigJSON(config[key])
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func encodeCanonicalJSON(value interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func CanonicalizeGraphJSON(raw []byte) ([]byte, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var graph map[string]interface{}
	if err := decoder.Decode(&graph); err != nil {
		return nil, false, err
	}
	nodes, _ := graph["nodes"].([]interface{})
	changed := false
	for _, item := range nodes {
		node, _ := item.(map[string]interface{})
		config, isMap := node["config"].(map[string]interface{})
		if !isMap {
			continue
		}
		nodeType, _ := node["type"].(string)
		if CanonicalizeNodeConfig(&Node{Type: NodeType(nodeType), Config: config}) {
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(graph)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func StructuredConfigGraphPath() string {
	types := make([]string, 0, len(structuredConfigKeys)+len(legacyNodeTypes))
	for nodeType := range structuredConfigKeys {
		types = append(types, string(nodeType))
	}
	for legacy, current := range legacyNodeTypes {
		if _, ok := structuredConfigKeys[current]; ok {
			types = append(types, string(legacy))
		}
	}
	sort.Strings(types)
	conditions := make([]string, len(types))
	for i, nodeType := range types {
		conditions[i] = fmt.Sprintf("@.type == %q", nodeType)
	}
	return "$.nodes[*] ? (" + strings.Join(conditions, " || ") + ")"
}
