package buttonaction

import "vozko/domain/whatsapp/template"

type Source string

const (
	SourceNone     Source = ""
	SourceTemplate Source = "template"
	SourceCampaign Source = "campaign"
)

type Reason string

const (
	ReasonResolved          Reason = "resolved"
	ReasonNoMatch           Reason = "no_match"
	ReasonNoAction          Reason = "no_action"
	ReasonStale             Reason = "stale"
	ReasonTemplateNotFound  Reason = "template_not_found"
	ReasonTemplateAmbiguous Reason = "template_ambiguous"
)

type Resolution struct {
	Template    TemplateRef   `json:"template"`
	ButtonIndex int           `json:"buttonIndex"`
	Action      *ButtonAction `json:"action,omitempty"`
	Source      Source        `json:"source,omitempty"`
	Reason      Reason        `json:"reason"`
}

func Resolve(t *template.Template, settings TemplateSettings, routes CampaignRoutes, index int) Resolution {
	out := Resolution{Template: RefOf(t), ButtonIndex: index}
	if t == nil {
		out.Reason = ReasonTemplateNotFound
		return out
	}
	if _, ok := quickReplyAt(t, index); !ok {
		out.Reason = ReasonNoMatch
		return out
	}
	templateAction, hasTemplateAction := settings.ActionAt(index)
	templateFresh := hasTemplateAction && !templateAction.StaleAgainst(t)
	if templateFresh && IsExit(templateAction.Kind) {
		return resolved(out, templateAction, SourceTemplate)
	}
	if route, ok := campaignRoute(t, routes, index); ok {
		return resolved(out, route, SourceCampaign)
	}
	if templateFresh {
		return resolved(out, templateAction, SourceTemplate)
	}
	if hasTemplateAction {
		out.Reason = ReasonStale
		return out
	}
	out.Reason = ReasonNoAction
	return out
}

func campaignRoute(t *template.Template, routes CampaignRoutes, index int) (ButtonAction, bool) {
	if !routes.appliesTo(t) {
		return ButtonAction{}, false
	}
	route, ok := routes.RouteAt(index)
	if !ok || IsExit(route.Kind) || route.StaleAgainst(t) {
		return ButtonAction{}, false
	}
	return route, true
}

func resolved(out Resolution, action ButtonAction, source Source) Resolution {
	out.Action = &action
	out.Source = source
	out.Reason = ReasonResolved
	return out
}

func ValidateOverride(t *template.Template, settings TemplateSettings, override ButtonAction) error {
	if err := override.Validate(t); err != nil {
		return err
	}
	if IsExit(override.Kind) {
		return ErrExitLocked
	}
	if action, ok := settings.ActionAt(override.ButtonIndex); ok && IsExit(action.Kind) && !action.StaleAgainst(t) {
		return ErrExitLocked
	}
	return nil
}
