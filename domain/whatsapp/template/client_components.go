package template

import "vozko/domain/conversation"

func ToClientComponents(components []TemplateComponent) []conversation.TemplateComponent {
	out := make([]conversation.TemplateComponent, 0, len(components))
	for _, c := range components {
		comp := conversation.TemplateComponent{
			Type:                      c.Type,
			Format:                    c.Format,
			Text:                      c.Text,
			AddSecurityRecommendation: c.AddSecurityRecommendation,
			CodeExpirationMinutes:     c.CodeExpirationMinutes,
		}

		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, conversation.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
				OTPType:     b.OTPType,
			})
		}

		if c.Example != nil {
			comp.Example = &conversation.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, conversation.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, conversation.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}

		out = append(out, comp)
	}
	return out
}

func FromClientComponents(components []conversation.TemplateComponent) []TemplateComponent {
	out := make([]TemplateComponent, 0, len(components))
	for _, c := range components {
		comp := TemplateComponent{
			Type:                      c.Type,
			Format:                    c.Format,
			Text:                      c.Text,
			AddSecurityRecommendation: c.AddSecurityRecommendation,
			CodeExpirationMinutes:     c.CodeExpirationMinutes,
		}

		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
				OTPType:     b.OTPType,
			})
		}

		if c.Example != nil {
			comp.Example = &TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}

		out = append(out, comp)
	}
	return out
}
