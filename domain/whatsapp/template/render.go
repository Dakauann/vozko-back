package template

import (
	"fmt"
	"strings"
)

// RenderedButton / RenderedComponent / TemplateInfo are the shape the CRM
// template bubble reads. The json tags are load-bearing: they are the wire
// contract with the frontend renderer, not an internal detail.
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

// RenderInfo substitutes a send's variables into the template and returns what
// the CRM should draw.
//
// It lives on the entity because three callers needed it and each had grown its
// own copy: the campaign consumer, the reopen-window sender, and now cold
// outbound. Three copies of a substitution rule is three ways for the message a
// customer received to disagree with the message the operator is shown.
//
// Substitution accepts BOTH placeholder styles for every parameter, because a
// template's declared format and the format its body actually uses have been
// observed to disagree after an upstream edit, and rendering "{{1}}" to a
// customer is worse than trying both.
func (t *Template) RenderInfo(params []string) map[string]interface{} {
	bodyText := strings.TrimSpace(t.GetBodyText())
	paramNames := t.GetParameterNames()

	if bodyText != "" && len(params) > 0 {
		for i, p := range params {
			bodyText = strings.ReplaceAll(bodyText, fmt.Sprintf("{{%d}}", i+1), p)
			if i < len(paramNames) {
				bodyText = strings.ReplaceAll(bodyText, "{{"+paramNames[i]+"}}", p)
			}
		}
	}
	if bodyText == "" {
		// A template with no body still has to render as something in a list of
		// conversations; an empty bubble reads as a bug.
		bodyText = fmt.Sprintf("[Template: %s]", t.Name)
	}

	var components []RenderedComponent
	// paramIdx walks forward across components: the parameters are one flat list
	// shared by header and body, so a header that consumed {{1}} must not let the
	// body consume it again.
	paramIdx := 0
	for _, comp := range t.Components {
		rc := RenderedComponent{Type: comp.Type, Format: comp.Format, Text: comp.Text}

		if (comp.Type == "BODY" || (comp.Type == "HEADER" && comp.Format == "TEXT")) && rc.Text != "" && len(params) > 0 {
			for i := paramIdx; i < len(params); i++ {
				positional := fmt.Sprintf("{{%d}}", i+1)
				if strings.Contains(rc.Text, positional) {
					rc.Text = strings.ReplaceAll(rc.Text, positional, params[i])
					paramIdx = i + 1
					continue
				}
				if i < len(paramNames) {
					named := "{{" + paramNames[i] + "}}"
					if strings.Contains(rc.Text, named) {
						rc.Text = strings.ReplaceAll(rc.Text, named, params[i])
						paramIdx = i + 1
					}
				}
			}
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

// RenderedBodyText is the one-line form, for callers that want the resolved text
// without the component tree.
func (t *Template) RenderedBodyText(params []string) string {
	info := t.RenderInfo(params)
	if s, ok := info["body_text"].(string); ok {
		return s
	}
	return ""
}
