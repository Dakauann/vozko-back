package template

import (
	"encoding/json"
	"fmt"
)

func (t *Template) HistoryRecord(params []string) (string, json.RawMessage, error) {
	info := t.RenderInfo(params)
	metadata, err := json.Marshal(info)
	if err != nil {
		return "", nil, fmt.Errorf("template %s metadata: %w", t.Name, err)
	}
	body, _ := info["body_text"].(string)
	return body, json.RawMessage(metadata), nil
}
