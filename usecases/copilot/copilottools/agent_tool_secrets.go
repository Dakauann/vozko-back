package copilottools

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

const (
	maxToolSecrets     = 20
	hiddenSecretMarker = "[protegido]"
)

var headerNamePattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]{1,64}$")

type toolSecrets struct {
	catalog ToolCatalog
}

func (s toolSecrets) definitions() map[string]tools.Definition {
	out := map[string]tools.Definition{}
	if s.catalog == nil {
		return out
	}
	for _, d := range s.catalog.Definitions() {
		out[d.Name] = d
	}
	return out
}

func toolSecretKey(tool, param, entry string) string {
	return strings.Join([]string{"tool", tool, param, entry}, ":")
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type sensitiveEntry struct {
	param string
	entry string
}

func sensitiveEntries(def tools.Definition, config map[string]interface{}) []sensitiveEntry {
	var out []sensitiveEntry
	for _, param := range def.SensitiveConfig() {
		switch value := config[param].(type) {
		case nil:
		case map[string]interface{}:
			for _, entry := range sortedKeys(value) {
				out = append(out, sensitiveEntry{param: param, entry: entry})
			}
		default:
			out = append(out, sensitiveEntry{param: param})
		}
	}
	return out
}

func (s toolSecrets) fields(bindings []toolBindingArg) []copilot.SecretField {
	defs := s.definitions()
	var out []copilot.SecretField
	for _, b := range bindings {
		def, ok := defs[b.Name]
		if !ok {
			continue
		}
		for _, e := range sensitiveEntries(def, b.Config) {
			label := def.DisplayName
			if e.entry != "" {
				label = e.entry + " · " + def.DisplayName
			}
			out = append(out, copilot.SecretField{Key: toolSecretKey(b.Name, e.param, e.entry), Label: label})
		}
	}
	return out
}

func (s toolSecrets) validate(bindings []toolBindingArg) error {
	defs := s.definitions()
	count := 0
	for _, b := range bindings {
		def, ok := defs[b.Name]
		if !ok {
			continue
		}
		for _, e := range sensitiveEntries(def, b.Config) {
			count++
			if e.entry != "" && !headerNamePattern.MatchString(e.entry) {
				return fmt.Errorf("%w: nome de cabeçalho inválido %q; use só letras, números e -", errInvalidArgs, e.entry)
			}
		}
	}
	if count > maxToolSecrets {
		return fmt.Errorf("%w: no máximo %d valores protegidos por proposta", errInvalidArgs, maxToolSecrets)
	}
	return nil
}

func (s toolSecrets) conceal(args map[string]interface{}, listKey string) map[string]interface{} {
	items, ok := args[listKey].([]interface{})
	if !ok {
		return args
	}
	defs := s.definitions()
	out := make(map[string]interface{}, len(args))
	for k, v := range args {
		out[k] = v
	}
	concealed := make([]interface{}, 0, len(items))
	for _, item := range items {
		concealed = append(concealed, concealItem(item, defs))
	}
	out[listKey] = concealed
	return out
}

func concealItem(item interface{}, defs map[string]tools.Definition) interface{} {
	m, ok := item.(map[string]interface{})
	if !ok {
		return item
	}
	name, _ := m["name"].(string)
	config, _ := m["config"].(map[string]interface{})
	def, known := defs[strings.TrimSpace(name)]
	if !known || config == nil {
		return item
	}
	blanked := blankSensitive(def, config, "")
	copied := make(map[string]interface{}, len(m))
	for k, v := range m {
		copied[k] = v
	}
	copied["config"] = blanked
	return copied
}

func blankSensitive(def tools.Definition, config map[string]interface{}, marker string) map[string]interface{} {
	out := make(map[string]interface{}, len(config))
	for k, v := range config {
		out[k] = v
	}
	for _, param := range def.SensitiveConfig() {
		switch value := config[param].(type) {
		case nil:
		case map[string]interface{}:
			hidden := make(map[string]interface{}, len(value))
			for entry := range value {
				hidden[entry] = marker
			}
			out[param] = hidden
		default:
			out[param] = marker
		}
	}
	return out
}

func (s toolSecrets) reveal(bindings []toolBindingArg, args map[string]interface{}) []toolBindingArg {
	defs := s.definitions()
	out := make([]toolBindingArg, 0, len(bindings))
	for _, b := range bindings {
		def, ok := defs[b.Name]
		if !ok || b.Config == nil {
			out = append(out, b)
			continue
		}
		config := make(map[string]interface{}, len(b.Config))
		for k, v := range b.Config {
			config[k] = v
		}
		for _, e := range sensitiveEntries(def, b.Config) {
			value, _ := args[toolSecretKey(b.Name, e.param, e.entry)].(string)
			if e.entry == "" {
				config[e.param] = value
				continue
			}
			filled, _ := config[e.param].(map[string]interface{})
			copied := make(map[string]interface{}, len(filled))
			for k, v := range filled {
				copied[k] = v
			}
			copied[e.entry] = value
			config[e.param] = copied
		}
		out = append(out, toolBindingArg{Name: b.Name, Config: config})
	}
	return out
}

func (s toolSecrets) hide(bindings []agent.ToolBinding) []agent.ToolBinding {
	defs := s.definitions()
	out := make([]agent.ToolBinding, 0, len(bindings))
	for _, b := range bindings {
		if def, ok := defs[b.Name]; ok && b.Config != nil {
			b.Config = blankSensitive(def, b.Config, hiddenSecretMarker)
		}
		out = append(out, b)
	}
	return out
}
