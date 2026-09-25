package copilottools

import (
	"strings"

	tmpl "vozko/domain/whatsapp/template"
)

type MessagePreview struct {
	Text        string `json:"text"`
	Channel     string `json:"channel"`
	ScheduledAt string `json:"scheduledAt,omitempty"`
}

func templatePreviewOf(template *tmpl.Template, variables []string) TemplatePreview {
	components := make([]tmpl.TemplateComponent, len(template.Components))
	copy(components, template.Components)
	if len(variables) > 0 {
		for i, c := range components {
			if strings.EqualFold(c.Type, "BODY") {
				c.Example = &tmpl.TemplateExample{BodyText: [][]string{variables}}
				components[i] = c
			}
		}
	}
	preview := TemplatePreview{Name: template.Name, Language: template.Language, Category: string(template.Category), Components: components}
	if template.HeaderMediaURL != nil {
		preview.HeaderMediaURL = *template.HeaderMediaURL
	}
	return preview
}
