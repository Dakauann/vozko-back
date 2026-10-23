package template

import (
	"encoding/json"
	"fmt"
)

func (t *Template) HistoryRecord(body, header []string) (string, json.RawMessage, error) {
	return t.historyRecord(t.RenderInfo(body, header))
}

func (t *Template) SlotsHistoryRecord(values []string) (string, json.RawMessage, error) {
	return t.HistoryRecord(t.SplitParameters(values))
}

func (t *Template) historyRecord(info map[string]interface{}) (string, json.RawMessage, error) {
	metadata, err := json.Marshal(info)
	if err != nil {
		return "", nil, fmt.Errorf("template %s metadata: %w", t.Name, err)
	}
	body, _ := info["body_text"].(string)
	return body, json.RawMessage(metadata), nil
}
