package template

import "vozko/domain/conversation"

// The template package and the WhatsApp client port each own a component type,
// and every field has to be copied between them in both directions: create and
// replicate go one way, sync comes back the other.
//
// That copying used to be written out at each of those three call sites, which
// is how this feature broke in the first place. A field added to the component
// reached whichever site the author was editing and silently defaulted at the
// other two, so a template could be created with a code button and read back
// without one. The same shape as the bug where a per-conversation fact was read
// from one channel's repository and every other channel got the zero value.
//
// One pair of functions, so a new field is added once and cannot be forgotten.

// ToClientComponents converts stored components into the shape the WhatsApp
// client sends.
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

// FromClientComponents converts what the WhatsApp client read back into stored
// components.
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
