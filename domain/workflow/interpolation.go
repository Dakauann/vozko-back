package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Matches {{ path }} where path may contain array indexing (data[0]) and
// optional surrounding whitespace.
var interpolationPattern = regexp.MustCompile(`\{\{\s*([\w.\[\]]+)\s*\}\}`)

func formatValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// normalizeIndexing rewrites array indexing (data[0]) into dot form (data.0) so
// every path segment is parsed uniformly by the resolver. It only changes how
// the path is *tokenised* — an integer segment is applied as an index only when
// the current value is an array (see deepResolve), so bracket indexing stays
// array-only and never accidentally indexes a map.
func normalizeIndexing(path string) string {
	if !strings.ContainsRune(path, '[') {
		return path
	}
	return strings.NewReplacer("[", ".", "]", "").Replace(path)
}

// deepResolve walks a dot-separated path (already normalized) into nested data:
// string segments index maps by key, integer segments index arrays by position.
func deepResolve(root interface{}, path string) (interface{}, bool) {
	cur := root
	for _, p := range strings.Split(path, ".") {
		if p == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]interface{}:
			v, ok := node[p]
			if !ok {
				return nil, false
			}
			cur = v
		case []interface{}:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func Interpolate(text string, state *RunState, sysCtx map[string]interface{}) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return interpolationPattern.ReplaceAllStringFunc(text, func(match string) string {

		inner := normalizeIndexing(strings.TrimSpace(match[2 : len(match)-2]))
		parts := strings.SplitN(inner, ".", 2)

		var scope, key string
		if len(parts) == 2 {
			scope, key = parts[0], parts[1]
		} else {

			scope, key = "var", inner
		}

		switch scope {
		case "var":
			val, ok := state.Get(key)
			if !ok {

				if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
					if root, ok2 := state.Get(key[:dotIdx]); ok2 {
						if resolved, ok3 := deepResolve(root, key[dotIdx+1:]); ok3 {
							return formatValue(resolved)
						}
					}
				}
				return match
			}
			return formatValue(val)
		case "sys":
			if sysCtx != nil {
				if val, ok := sysCtx[key]; ok {
					return formatValue(val)
				}
			}
			return resolveBuiltinSys(key)
		case "last":
			val, ok := state.Get("_last_" + key)
			if !ok {

				if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
					if root, ok2 := state.Get("_last_" + key[:dotIdx]); ok2 {
						if resolved, ok3 := deepResolve(root, key[dotIdx+1:]); ok3 {
							return formatValue(resolved)
						}
					}
				}
				return match
			}
			return formatValue(val)
		case "ai":
			val, ok := state.Get("_ai_" + key)
			if !ok {

				if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
					if root, ok2 := state.Get("_ai_" + key[:dotIdx]); ok2 {
						if resolved, ok3 := deepResolve(root, key[dotIdx+1:]); ok3 {
							return formatValue(resolved)
						}
					}
				}
				return match
			}
			return formatValue(val)
		default:

			if scope == "node" {
				nodeParts := strings.SplitN(key, ".", 2)
				if len(nodeParts) == 2 {
					stateKey := "_node_" + nodeParts[0] + "_" + nodeParts[1]
					if val, ok := state.Get(stateKey); ok {
						return formatValue(val)
					}
					// Deep access: {{node.<id>.<outputKey>.<path...>}} — the first
					// segment is the output key, the remainder a dot/bracket path into
					// its value (mirrors var/last/ai and the n8n expression standard).
					if dotIdx := strings.IndexByte(nodeParts[1], '.'); dotIdx > 0 {
						if root, ok := state.Get("_node_" + nodeParts[0] + "_" + nodeParts[1][:dotIdx]); ok {
							if resolved, ok2 := deepResolve(root, nodeParts[1][dotIdx+1:]); ok2 {
								return formatValue(resolved)
							}
						}
					}
				}
				return match
			}

			if strings.HasPrefix(scope, "node_") {
				if val, ok := state.Get("_" + scope + "_" + key); ok {
					return formatValue(val)
				}
			}

			if val, ok := state.Get(scope); ok {
				// Fast path: a literal top-level map key (may itself contain dots).
				if m, isMap := val.(map[string]interface{}); isMap {
					if v, found := m[key]; found {
						return formatValue(v)
					}
				}
				// deepResolve walks both maps (by key) and arrays (by index), so a
				// top-level array variable captured from a JSON array response —
				// e.g. {{token_consulta_cadastro[0].token}} — resolves here too.
				if resolved, ok3 := deepResolve(val, key); ok3 {
					return formatValue(resolved)
				}
			}
			// Fallback: HTTP response envelope for a captured variable, so
			// {{captureVar.status_code}} / {{captureVar.success}} resolve even when
			// the captured value is the bare body. Body fields win (checked above).
			if meta, ok := state.Get("_httpmeta_" + scope); ok {
				if resolved, ok3 := deepResolve(meta, key); ok3 {
					return formatValue(resolved)
				}
			}
			return match
		}
	})
}

func resolveBuiltinSys(key string) string {
	switch key {
	case "date":
		return time.Now().UTC().Format("2006-01-02")
	case "time":
		return time.Now().UTC().Format("15:04:05")
	case "timestamp":
		return fmt.Sprintf("%d", time.Now().Unix())
	default:
		return "{{sys." + key + "}}"
	}
}

func ResolveVariable(ref string, state *RunState) (interface{}, bool) {
	ref = normalizeIndexing(ref)
	parts := strings.SplitN(ref, ".", 2)
	var scope, key string
	if len(parts) == 2 {
		scope, key = parts[0], parts[1]
	} else {
		scope, key = "var", ref
	}

	switch scope {
	case "var":
		if val, ok := state.Get(key); ok {
			return val, true
		}
		if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
			if root, ok := state.Get(key[:dotIdx]); ok {
				if resolved, ok := deepResolve(root, key[dotIdx+1:]); ok {
					return resolved, true
				}
			}
		}
		return nil, false
	case "last":
		if val, ok := state.Get("_last_" + key); ok {
			return val, true
		}
		if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
			if root, ok := state.Get("_last_" + key[:dotIdx]); ok {
				if resolved, ok := deepResolve(root, key[dotIdx+1:]); ok {
					return resolved, true
				}
			}
		}
		return nil, false
	case "ai":
		if val, ok := state.Get("_ai_" + key); ok {
			return val, true
		}
		if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
			if root, ok := state.Get("_ai_" + key[:dotIdx]); ok {
				if resolved, ok := deepResolve(root, key[dotIdx+1:]); ok {
					return resolved, true
				}
			}
		}
		return nil, false
	case "node":
		nodeParts := strings.SplitN(key, ".", 2)
		if len(nodeParts) == 2 {
			stateKey := "_node_" + nodeParts[0] + "_" + nodeParts[1]
			if val, ok := state.Get(stateKey); ok {
				return val, true
			}
			// Deep access into a node output value, e.g. node.<id>.tool_args.cep
			if dotIdx := strings.IndexByte(nodeParts[1], '.'); dotIdx > 0 {
				if root, ok := state.Get("_node_" + nodeParts[0] + "_" + nodeParts[1][:dotIdx]); ok {
					if resolved, ok2 := deepResolve(root, nodeParts[1][dotIdx+1:]); ok2 {
						return resolved, true
					}
				}
			}
		}
		return nil, false
	default:
		if strings.HasPrefix(scope, "node_") {
			if val, ok := state.Get("_" + scope + "_" + key); ok {
				return val, true
			}
		}
		if val, ok := state.Get(scope); ok {
			if m, isMap := val.(map[string]interface{}); isMap {
				if v, found := m[key]; found {
					return v, true
				}
			}
			// Index into a top-level array (or nested path) captured variable.
			if resolved, ok := deepResolve(val, key); ok {
				return resolved, true
			}
		}
		// Fallback: HTTP response envelope companion (status_code/success/body).
		if meta, ok := state.Get("_httpmeta_" + scope); ok {
			if resolved, ok := deepResolve(meta, key); ok {
				return resolved, true
			}
		}

		if val, ok := state.Get(ref); ok {
			return val, true
		}
		return nil, false
	}
}

func InterpolateMap(m map[string]interface{}, state *RunState, sysCtx map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			result[k] = Interpolate(val, state, sysCtx)
		case map[string]interface{}:
			result[k] = InterpolateMap(val, state, sysCtx)
		default:
			result[k] = v
		}
	}
	return result
}

type VariableDependency struct {
	Scope   string `json:"scope"`
	NodeID  string `json:"nodeId"`
	Key     string `json:"key"`
	FullRef string `json:"fullRef"`
}

type DependencySource string

const (
	DependencySourcePreviousNode DependencySource = "previous_node"
	DependencySourceSpecificNode DependencySource = "specific_node"
	DependencySourceTrigger      DependencySource = "trigger"
	DependencySourceAI           DependencySource = "ai"
	DependencySourceSystem       DependencySource = "system"
	DependencySourceCustom       DependencySource = "custom"
	DependencySourceAgentVar     DependencySource = "agent_variable"
)

type RequiredMock struct {
	StateKey    string           `json:"stateKey"`
	DisplayName string           `json:"displayName"`
	Source      DependencySource `json:"source"`
	SourceNode  string           `json:"sourceNode"`
}

func ExtractDependencies(config map[string]interface{}) []VariableDependency {
	var deps []VariableDependency
	seen := make(map[string]bool)

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch val := v.(type) {
		case string:
			matches := interpolationPattern.FindAllStringSubmatch(val, -1)
			for _, m := range matches {
				ref := m[1]
				if seen[ref] {
					continue
				}
				seen[ref] = true

				dep := parseVariableRef(ref)
				deps = append(deps, dep)
			}
		case map[string]interface{}:
			for _, vv := range val {
				walk(vv)
			}
		case []interface{}:
			for _, vv := range val {
				walk(vv)
			}
		}
	}

	walk(config)
	return deps
}

func parseVariableRef(ref string) VariableDependency {
	parts := strings.SplitN(ref, ".", 2)
	dep := VariableDependency{FullRef: ref}

	if len(parts) == 1 {

		dep.Scope = "var"
		dep.Key = ref
	} else {
		dep.Scope = parts[0]
		dep.Key = parts[1]

		if dep.Scope == "node" {
			nodeParts := strings.SplitN(dep.Key, ".", 2)
			if len(nodeParts) == 2 {
				dep.NodeID = nodeParts[0]
				dep.Key = nodeParts[1]
			} else {

				dep.NodeID = dep.Key
				dep.Key = ""
			}
		}
	}

	return dep
}

func RequiresUpstreamData(deps []VariableDependency) bool {
	for _, d := range deps {
		switch d.Scope {
		case "last", "node", "ai":
			return true
		default:

			if d.Scope != "var" && d.Scope != "sys" {
				return true
			}
		}
	}
	return false
}

func HasAIDependencies(deps []VariableDependency) bool {
	for _, d := range deps {
		if d.Scope == "ai" {
			return true
		}
	}
	return false
}

func GetRequiredMocks(deps []VariableDependency) []RequiredMock {
	var mocks []RequiredMock
	seen := make(map[string]bool)

	for _, d := range deps {
		var mock RequiredMock

		switch d.Scope {
		case "last":
			mock.StateKey = "_last_" + d.Key
			mock.DisplayName = "last." + d.Key
			mock.Source = DependencySourcePreviousNode
		case "node":
			if d.NodeID != "" && d.Key != "" {
				mock.StateKey = "_node_" + d.NodeID + "_" + d.Key
				mock.DisplayName = "node." + d.NodeID + "." + d.Key
			} else if d.NodeID != "" {
				mock.StateKey = "_node_" + d.NodeID
				mock.DisplayName = "node." + d.NodeID
			} else {
				continue
			}
			mock.Source = DependencySourceSpecificNode
			mock.SourceNode = d.NodeID
		case "ai":
			mock.StateKey = "_ai_" + d.Key
			mock.DisplayName = "ai." + d.Key
			mock.Source = DependencySourceAI
		case "var":
			mock.StateKey = d.Key
			mock.DisplayName = d.Key
			mock.Source = DependencySourceTrigger
		case "sys":

			continue
		default:

			mock.StateKey = d.FullRef
			mock.DisplayName = d.FullRef
			mock.Source = DependencySourceCustom
		}

		if seen[mock.StateKey] {
			continue
		}
		seen[mock.StateKey] = true
		mocks = append(mocks, mock)
	}

	return mocks
}

func CanTestIndependently(config map[string]interface{}) bool {
	deps := ExtractDependencies(config)
	return !RequiresUpstreamData(deps)
}

func ExtractCampaignVars(g Graph) []string {
	seen := make(map[string]bool)
	for _, node := range g.Nodes {
		extractCampvarsFromValue(node.Config, seen)
	}
	if len(seen) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}

	sortStrings(keys)
	return keys
}

var campvarsPattern = regexp.MustCompile(`\{\{(?:var\.)?campvars\.([\w]+)\}\}`)

func extractCampvarsFromValue(v interface{}, seen map[string]bool) {
	switch val := v.(type) {
	case string:
		for _, m := range campvarsPattern.FindAllStringSubmatch(val, -1) {
			seen[m[1]] = true
		}
	case map[string]interface{}:
		for _, vv := range val {
			extractCampvarsFromValue(vv, seen)
		}
	case []interface{}:
		for _, vv := range val {
			extractCampvarsFromValue(vv, seen)
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
