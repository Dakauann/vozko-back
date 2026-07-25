package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var agentVarPattern = regexp.MustCompile(`\{\{(\w+)\}\}`)

type InterpolatedAgent struct {
	MessagingPrompt string
	InitialMessage  string
}

func InterpolateAgent(a *Agent, vars map[string]string) InterpolatedAgent {
	defaults := buildDefaultsMap(a.Variables)
	return InterpolatedAgent{
		MessagingPrompt: resolveTemplate(a.MessagingPrompt, vars, defaults),
		InitialMessage:  resolveTemplate(a.InitialMessage, vars, defaults),
	}
}

func ExtractVariableNames(a *Agent) []string {
	seen := make(map[string]bool)
	for _, name := range extractNames(a.MessagingPrompt) {
		seen[name] = true
	}
	for _, name := range extractNames(a.InitialMessage) {
		seen[name] = true
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func VariableUsedIn(a *Agent, name string) VariableUsage {
	needle := "{{" + name + "}}"
	return VariableUsage{
		MessagingPrompt: strings.Contains(a.MessagingPrompt, needle),
		InitialMessage:  strings.Contains(a.InitialMessage, needle),
	}
}

type VariableUsage struct {
	MessagingPrompt bool `json:"messagingPrompt"`
	InitialMessage  bool `json:"initialMessage"`
}

type RequiredVariableInfo struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	DefaultValue string        `json:"defaultValue,omitempty"`
	Required     bool          `json:"required"`
	UsedIn       VariableUsage `json:"usedIn"`
}

func (a *Agent) RequiredVariableInfos() []RequiredVariableInfo {
	declared := make(map[string]bool, len(a.Variables))
	infos := make([]RequiredVariableInfo, 0, len(a.Variables))

	for _, v := range a.Variables {
		declared[v.Name] = true
		infos = append(infos, RequiredVariableInfo{
			Name:         v.Name,
			Description:  v.Description,
			DefaultValue: v.DefaultValue,
			Required:     strings.TrimSpace(v.DefaultValue) == "",
			UsedIn:       VariableUsedIn(a, v.Name),
		})
	}

	for _, name := range ExtractVariableNames(a) {
		if !declared[name] {
			infos = append(infos, RequiredVariableInfo{
				Name:     name,
				Required: true,
				UsedIn:   VariableUsedIn(a, name),
			})
		}
	}

	return infos
}

func (a *Agent) RequiredVariables() []AgentVariable {
	var required []AgentVariable
	for _, v := range a.Variables {
		if strings.TrimSpace(v.DefaultValue) == "" {
			required = append(required, v)
		}
	}
	return required
}

func buildDefaultsMap(vars []AgentVariable) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Name] = v.DefaultValue
	}
	return m
}

func resolveTemplate(text string, vars map[string]string, defaults map[string]string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return agentVarPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := match[2 : len(match)-2]
		if val, ok := vars[name]; ok {
			return val
		}
		if val, ok := defaults[name]; ok {
			return val
		}
		return ""
	})
}

func extractNames(text string) []string {
	var names []string
	for _, m := range agentVarPattern.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 && m[1] != "" {
			names = append(names, m[1])
		}
	}
	return names
}

func MetadataToVars(meta map[string]interface{}) map[string]string {
	if meta == nil {
		return nil
	}
	result := make(map[string]string, len(meta))
	for k, v := range meta {
		switch val := v.(type) {
		case string:
			result[k] = val
		case nil:
			result[k] = ""
		default:
			result[k] = fmt.Sprintf("%v", val)
		}
	}
	return result
}
