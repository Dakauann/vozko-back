package metamessaging

import (
	"encoding/json"
	"fmt"
	"strings"
)

type GraphID string

func (g *GraphID) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*g = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("id: decode string: %w", err)
		}
		*g = GraphID(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("id: decode number: %w", err)
	}
	*g = GraphID(n.String())
	return nil
}

func (g GraphID) String() string { return string(g) }
