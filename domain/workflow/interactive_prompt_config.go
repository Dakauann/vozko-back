package workflow

import (
	"fmt"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
)

const (
	interactiveButtonsKey  = "buttons"
	interactiveSectionsKey = "sections"
)

type interactiveButtonJSON struct {
	Type     conversation.ButtonType `json:"Type"`
	ID       string                  `json:"ID"`
	Title    string                  `json:"Title"`
	CopyCode string                  `json:"CopyCode"`
}

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
	var parsed []interactiveButtonJSON
	if err := decodeStructuredConfig(config, interactiveButtonsKey, &parsed); err != nil {
		return nil, fmt.Errorf("botões inválidos: %w", err)
	}
	buttons := make([]conversation.InteractiveButton, 0, len(parsed))
	for _, b := range parsed {
		buttons = append(buttons, conversation.InteractiveButton{Type: b.Type, ID: b.ID, Title: b.Title, CopyCode: b.CopyCode})
	}
	return buttons, nil
}

func InteractiveListSections(config map[string]interface{}) ([]conversation.ListSection, error) {
	var parsed []interactiveListSectionJSON
	if err := decodeStructuredConfig(config, interactiveSectionsKey, &parsed); err != nil {
		return nil, fmt.Errorf("opções da lista inválidas: %w", err)
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

func InteractiveOptionHandles(config map[string]interface{}) []HandleDefinition {
	handles := make([]HandleDefinition, 0, 4)
	seen := make(map[string]struct{})
	add := func(id, label string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		handles = append(handles, HandleDefinition{ID: id, Label: label})
	}

	if InteractivePromptStyle(config) == channel.InteractiveStyleList {
		sections, _ := InteractiveListSections(config)
		for _, section := range sections {
			for _, row := range section.Rows {
				add(row.ID, row.Title)
			}
		}
		return handles
	}
	buttons, _ := InteractiveButtons(config)
	for _, b := range buttons {
		if b.Type == conversation.ButtonTypeCopyCode {
			continue
		}
		add(b.ID, b.Title)
	}
	return handles
}
