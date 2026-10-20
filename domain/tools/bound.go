package tools

import (
	"regexp"
	"strings"
)

const MaxToolNameLength = 64

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func ValidToolName(name string) bool {
	return toolNamePattern.MatchString(name)
}

func (d Definition) BindingIdentity(config map[string]interface{}) string {
	name := strings.ToLower(strings.TrimSpace(d.Name))
	if d.BindingKey == "" {
		return name
	}
	return d.ToolNameFor(config)
}

func (d Definition) ToolNameFor(config map[string]interface{}) string {
	if d.BindingKey == "" {
		return d.Name
	}
	key, _ := config[d.BindingKey].(string)
	return BoundToolName(d.Name, key)
}

func BoundToolName(base, bindingKey string) string {
	key := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		}
		return '_'
	}, strings.ToLower(strings.TrimSpace(bindingKey)))
	if key == "" {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(base)) + "_" + key
	if len(name) > MaxToolNameLength {
		name = name[:MaxToolNameLength]
	}
	return name
}

func IsBoundToolName(name, base string) bool {
	_, ok := BoundKeyOf(name, base)
	return ok
}

func BoundKeyOf(name, base string) (string, bool) {
	prefix := strings.ToLower(strings.TrimSpace(base)) + "_"
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) <= len(prefix) || !strings.HasPrefix(name, prefix) {
		return "", false
	}
	return name[len(prefix):], true
}
