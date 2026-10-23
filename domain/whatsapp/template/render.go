package template

import (
	"fmt"
	"strings"
)

type RenderedButton struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	URL         string `json:"url,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
}

type RenderedComponent struct {
	Type    string           `json:"type"`
	Format  string           `json:"format,omitempty"`
	Text    string           `json:"text,omitempty"`
	Buttons []RenderedButton `json:"buttons,omitempty"`
}

func (t *Template) RenderSlots(values []string) map[string]interface{} {
	return t.RenderInfo(t.SplitParameters(values))
}

func (t *Template) RenderInfo(body, header []string) map[string]interface{} {
	var bodyNames, headerNames []string
	for _, slot := range t.ParameterSlots() {
		if slot.Header {
			headerNames = append(headerNames, slot.Name)
			continue
		}
		bodyNames = append(bodyNames, slot.Name)
	}
	bodyText := fillParameters(strings.TrimSpace(t.GetBodyText()), bodyNames, body)
	if bodyText == "" {
		bodyText = fmt.Sprintf("[Template: %s]", t.Name)
	}

	var components []RenderedComponent
	for _, comp := range t.Components {
		rc := RenderedComponent{Type: comp.Type, Format: comp.Format, Text: comp.Text}
		switch {
		case comp.Type == "BODY":
			rc.Text = fillParameters(rc.Text, bodyNames, body)
		case comp.Type == "HEADER" && comp.Format == "TEXT":
			rc.Text = fillParameters(rc.Text, headerNames, header)
		}

		for _, btn := range comp.Buttons {
			rc.Buttons = append(rc.Buttons, RenderedButton{
				Type:        btn.Type,
				Text:        btn.Text,
				URL:         btn.URL,
				PhoneNumber: btn.PhoneNumber,
			})
		}

		components = append(components, rc)
	}

	info := map[string]interface{}{
		"template_name": t.Name,
		"language":      t.Language,
		"category":      string(t.Category),
		"body_text":     bodyText,
		"components":    components,
	}
	if t.HeaderMediaURL != nil {
		info["header_media_url"] = *t.HeaderMediaURL
	}
	return info
}

func (t *Template) RenderedBodyText(params []string) string {
	info := t.RenderInfo(params, nil)
	if s, ok := info["body_text"].(string); ok {
		return s
	}
	return ""
}

func fillParameters(text string, names, values []string) string {
	for i, value := range values {
		if i < len(names) {
			text = strings.ReplaceAll(text, "{{"+names[i]+"}}", value)
		}
	}
	return text
}
