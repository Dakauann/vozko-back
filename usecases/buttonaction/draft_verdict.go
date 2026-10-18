package buttonaction_usecase

import (
	"context"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

type DraftTemplate struct {
	Category template.TemplateCategory
	Header   string
	Body     string
	Footer   string
	Buttons  []template.TemplateButton
	Actions  []buttonaction.Input
}

type DraftVerdictView struct {
	Template     *template.Template
	Verdict      buttonaction.Verdict
	Gates        buttonaction.ExitGates
	AllowedKinds []buttonaction.Kind
}

func (s *Service) DraftExitVerdict(_ context.Context, a Actor, draft DraftTemplate) (*DraftVerdictView, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	probe := draftProbe(draft)
	settings, err := buttonaction.SettingsFor(probe, draft.Actions)
	if err != nil {
		return nil, err
	}
	verdict := buttonaction.ExitVerdict(probe, settings, buttonaction.CampaignRoutes{})
	return &DraftVerdictView{
		Template:     probe,
		Verdict:      verdict,
		Gates:        buttonaction.ExitGatesOf(draft.Category, verdict),
		AllowedKinds: s.allowedKinds(a),
	}, nil
}

func draftProbe(draft DraftTemplate) *template.Template {
	probe := &template.Template{Category: draft.Category}
	texts := []template.TemplateComponent{
		{Type: "HEADER", Format: "TEXT", Text: draft.Header},
		{Type: "BODY", Text: draft.Body},
		{Type: "FOOTER", Text: draft.Footer},
	}
	for _, text := range texts {
		if text.Text != "" {
			probe.Components = append(probe.Components, text)
		}
	}
	if len(draft.Buttons) > 0 {
		probe.Components = append(probe.Components, template.TemplateComponent{Type: "BUTTONS", Buttons: draft.Buttons})
	}
	return probe
}
