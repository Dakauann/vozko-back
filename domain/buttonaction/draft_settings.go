package buttonaction

import "vozko/domain/whatsapp/template"

func SettingsFor(t *template.Template, inputs []Input) (TemplateSettings, error) {
	actions := make([]ButtonAction, 0, len(inputs))
	for _, in := range inputs {
		action, err := BindButtonAction(t, in, nil)
		if err != nil {
			return TemplateSettings{}, err
		}
		actions = append(actions, action)
	}
	if err := EnsureOnePerButton(actions); err != nil {
		return TemplateSettings{}, err
	}
	return TemplateSettings{Actions: actions}, nil
}
