package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
)

const (
	interactiveButtonsKey  = "buttons"
	interactiveSectionsKey = "sections"
)

type interactiveListRowJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type interactiveListSectionJSON struct {
	Title string                   `json:"title"`
	Rows  []interactiveListRowJSON `json:"rows"`
}

func InteractivePromptStyle(config map[string]interface{}) string {
	style, _ := config["interactive_type"].(string)
	if strings.EqualFold(strings.TrimSpace(style), channel.InteractiveStyleList) {
		return channel.InteractiveStyleList
	}
	return channel.InteractiveStyleButtons
}

func InteractiveButtons(config map[string]interface{}) ([]conversation.InteractiveButton, error) {
	var buttons []conversation.InteractiveButton
	if err := decodeInteractiveOptions(config, interactiveButtonsKey, &buttons); err != nil {
		return nil, fmt.Errorf("botões inválidos: %w", err)
	}
	return buttons, nil
}

func InteractiveListSections(config map[string]interface{}) ([]conversation.ListSection, error) {
	parsed, err := decodeInteractiveListSections(config)
	if err != nil {
		return nil, err
	}
	sections := make([]conversation.ListSection, 0, len(parsed))
	for _, s := range parsed {
		rows := make([]conversation.ListRow, 0, len(s.Rows))
		for _, r := range s.Rows {
			rows = append(rows, conversation.ListRow{ID: r.ID, Title: r.Title, Description: r.Description})
		}
		sections = append(sections, conversation.ListSection{Title: s.Title, Rows: rows})
	}
	return sections, nil
}

func decodeInteractiveListSections(config map[string]interface{}) ([]interactiveListSectionJSON, error) {
	var sections []interactiveListSectionJSON
	if err := decodeInteractiveOptions(config, interactiveSectionsKey, &sections); err != nil {
		return nil, fmt.Errorf("opções da lista inválidas: %w", err)
	}
	return sections, nil
}

func decodeInteractiveOptions(config map[string]interface{}, key string, target interface{}) error {
	raw, err := interactiveOptionsJSON(config[key])
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func interactiveOptionsJSON(value interface{}) ([]byte, error) {
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

func (w *Workflow) normalizeInteractivePromptConfigs() {
	for i := range w.Graph.Nodes {
		node := &w.Graph.Nodes[i]
		if node.Type.IsInteractivePrompt() {
			encodeImportedInteractiveOptions(node.Config)
		}
	}
}

func encodeImportedInteractiveOptions(config map[string]interface{}) {
	if isJSONList(config[interactiveButtonsKey]) {
		if buttons, err := InteractiveButtons(config); err == nil {
			setEncodedOptions(config, interactiveButtonsKey, buttons)
		}
	}
	if isJSONList(config[interactiveSectionsKey]) {
		if sections, err := decodeInteractiveListSections(config); err == nil {
			setEncodedOptions(config, interactiveSectionsKey, sections)
		}
	}
}

func isJSONList(value interface{}) bool {
	_, ok := value.([]interface{})
	return ok
}

func setEncodedOptions(config map[string]interface{}, key string, options interface{}) {
	encoded, err := json.Marshal(options)
	if err != nil {
		return
	}
	config[key] = string(encoded)
}
