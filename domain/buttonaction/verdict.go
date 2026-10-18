package buttonaction

import "vozko/domain/whatsapp/template"

type VerdictState string

const (
	VerdictOK   VerdictState = "ok"
	VerdictNone VerdictState = "none"
)

type ButtonRoute struct {
	Index  int           `json:"index"`
	Text   string        `json:"text"`
	Action *ButtonAction `json:"action"`
	Source Source        `json:"source,omitempty"`
	Stale  bool          `json:"stale"`
}

type Verdict struct {
	State     VerdictState  `json:"state"`
	Buttons   []ButtonRoute `json:"buttons"`
	ExitWords []string      `json:"exitWords"`
}

func ExitVerdict(t *template.Template, settings TemplateSettings, routes CampaignRoutes) Verdict {
	verdict := Verdict{
		Buttons:   RoutesOf(t, settings, routes),
		ExitWords: ShownExitWords(t),
		State:     VerdictNone,
	}
	if len(verdict.ExitWords) > 0 {
		verdict.State = VerdictOK
	}
	for _, button := range verdict.Buttons {
		if button.LeadsOut() {
			verdict.State = VerdictOK
		}
	}
	return verdict
}

func (b ButtonRoute) LeadsOut() bool {
	return b.Action != nil && IsExit(b.Action.Kind)
}

func RoutesOf(t *template.Template, settings TemplateSettings, routes CampaignRoutes) []ButtonRoute {
	replies := t.QuickReplies()
	out := make([]ButtonRoute, 0, len(replies))
	for _, reply := range replies {
		resolution := Resolve(t, settings, routes, reply.Index)
		out = append(out, ButtonRoute{
			Index:  reply.Index,
			Text:   reply.Text,
			Action: resolution.Action,
			Source: resolution.Source,
			Stale:  resolution.Reason == ReasonStale,
		})
	}
	return out
}
