package crmtools

import (
	"fmt"
	"strings"

	"vozko/domain/buttonaction"
	"vozko/domain/copilot"
	"vozko/domain/media"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/usecases/copilot/copilottools"
)

type templateContentArgs struct {
	HeaderText       string   `json:"header_text" desc:"título curto opcional (sem variáveis)"`
	HeaderMediaID    string   `json:"header_media_id" desc:"media_id de um anexo para o cabeçalho (imagem, vídeo ou documento)" id:"true"`
	Body             string   `json:"body" req:"true" desc:"texto da mensagem; variáveis como {{1}}, {{2}}"`
	BodyExamples     []string `json:"body_examples" desc:"um exemplo para cada variável do texto, na ordem"`
	Footer           string   `json:"footer" desc:"rodapé curto opcional, sem variáveis"`
	QuickReplies     []string `json:"quick_replies" desc:"textos de botões de resposta rápida (até 3)"`
	ExitButton       string   `json:"exit_button" desc:"texto exato de um dos quick_replies que tira o lead da lista quando tocado"`
	ExitConfirmation string   `json:"exit_confirmation" desc:"resposta enviada uma vez a quem sair (opcional)"`
}

type keptParts struct {
	header  *tmpl.TemplateComponent
	footer  *tmpl.TemplateComponent
	buttons *tmpl.TemplateComponent
}

func (a templateContentArgs) components(files media.GetMediaUseCase, cc copilot.Context, kept keptParts) ([]tmpl.TemplateComponent, *string, error) {
	var out []tmpl.TemplateComponent
	var headerURL *string
	switch {
	case strings.TrimSpace(a.HeaderMediaID) != "":
		m, err := files.GetMedia(cc.WorkspaceID, strings.TrimSpace(a.HeaderMediaID))
		if err != nil {
			return nil, nil, err
		}
		url := m.URL
		headerURL = &url
		out = append(out, tmpl.TemplateComponent{Type: "HEADER", Format: headerFormat(m.Type), Example: &tmpl.TemplateExample{HeaderHandle: []string{url}}})
	case strings.TrimSpace(a.HeaderText) != "":
		out = append(out, tmpl.TemplateComponent{Type: "HEADER", Format: "TEXT", Text: strings.TrimSpace(a.HeaderText)})
	case kept.header != nil && tmpl.IsMediaHeader(*kept.header):
		out = append(out, tmpl.TemplateComponent{Type: "HEADER", Format: kept.header.Format})
	case kept.header != nil:
		out = append(out, *kept.header)
	}
	body := tmpl.TemplateComponent{Type: "BODY", Text: strings.TrimSpace(a.Body)}
	if len(a.BodyExamples) > 0 {
		body.Example = &tmpl.TemplateExample{BodyText: [][]string{a.BodyExamples}}
	}
	out = append(out, body)
	if footer := strings.TrimSpace(a.Footer); footer != "" {
		out = append(out, tmpl.TemplateComponent{Type: "FOOTER", Text: footer})
	} else if kept.footer != nil {
		out = append(out, *kept.footer)
	}
	if len(a.QuickReplies) > 0 {
		buttons := make([]tmpl.TemplateButton, 0, len(a.QuickReplies))
		for _, text := range a.QuickReplies {
			buttons = append(buttons, tmpl.TemplateButton{Type: "QUICK_REPLY", Text: strings.TrimSpace(text)})
		}
		out = append(out, tmpl.TemplateComponent{Type: "BUTTONS", Buttons: buttons})
	} else if kept.buttons != nil {
		out = append(out, *kept.buttons)
	}
	return out, headerURL, nil
}

func (a templateContentArgs) exitActions(components []tmpl.TemplateComponent) ([]buttonaction.Input, error) {
	exit := strings.TrimSpace(a.ExitButton)
	if exit == "" {
		return nil, nil
	}
	for _, comp := range components {
		if !strings.EqualFold(comp.Type, "BUTTONS") {
			continue
		}
		for i, button := range comp.Buttons {
			if strings.EqualFold(button.Type, "QUICK_REPLY") && strings.TrimSpace(button.Text) == exit {
				return []buttonaction.Input{{ButtonIndex: i, ButtonText: exit, Kind: buttonaction.KindOptOut}}, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: exit_button precisa ser o texto exato de um botão de resposta rápida do modelo", copilottools.ErrInvalidArgs)
}

func requireShownExit(draft *tmpl.Template, a templateContentArgs) error {
	actions, err := a.exitActions(draft.Components)
	if err != nil {
		return err
	}
	settings, err := buttonaction.SettingsFor(draft, actions)
	if err != nil {
		return fmt.Errorf("%w: exit_button: %v", copilottools.ErrInvalidArgs, err)
	}
	if err := buttonaction.RequireTemplateExit(draft.Category, buttonaction.ExitVerdict(draft, settings, buttonaction.CampaignRoutes{})); err != nil {
		return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, marketingExitHint)
	}
	return nil
}
