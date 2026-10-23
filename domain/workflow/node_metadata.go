package workflow

import (
	"slices"
	"strings"
)

const nodeDisplayNameKey = "display_name"

var commonNodeConfigKeys = []string{nodeDisplayNameKey}

func CommonNodeConfigKeys() []string {
	return slices.Clone(commonNodeConfigKeys)
}

func IsCommonNodeConfigKey(key string) bool {
	return slices.Contains(commonNodeConfigKeys, key)
}

func (n Node) DisplayName() string {
	name, _ := n.Config[nodeDisplayNameKey].(string)
	return strings.TrimSpace(name)
}
