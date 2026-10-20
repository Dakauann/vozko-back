package notification

import "time"

const (
	EpisodeNoticePlaceholder = "EpisodeNotice"
	EpisodeTitlePlaceholder  = "EpisodeTitle"
	EpisodeTonePlaceholder   = "EpisodeTone"
)

type stepCopy struct {
	prefix      string
	label       string
	tone        string
	bodyClosing string
	notice      func(since string) string
}

var stepCopies = map[EpisodeStep]stepCopy{
	Reminder: {
		prefix: "Lembrete: ",
		label:  "Lembrete",
		tone:   "info",
		notice: func(since string) string {
			return "Isto continua acontecendo desde " + since + ". Resolva para os avisos pararem."
		},
	},
	LastNotice: {
		prefix:      "Último aviso: ",
		label:       "Último aviso",
		tone:        "warning",
		bodyClosing: " Este é o último aviso: não vamos enviar mais emails sobre isso até que seja resolvido.",
		notice: func(since string) string {
			return "Isto continua acontecendo desde " + since + ". Este é o último aviso: não vamos enviar mais emails sobre isso até que seja resolvido. O botão abaixo leva direto para a solução."
		},
	},
}

func (d Draft) ForEpisodeStep(step EpisodeStep, since time.Time, loc *time.Location) Draft {
	text, varies := stepCopies[step]
	if !varies {
		return d
	}
	when := sinceLabel(since, loc)
	d.Title = ClipTitle(text.prefix + d.Title)
	d.Body = clipRunes("Continua desde "+when+". "+d.Body+text.bodyClosing, BodyMaxRunes)
	if d.EmailCopy != nil {
		email := *d.EmailCopy
		email.Subject = text.prefix + email.Subject
		email.Placeholders = withStepPlaceholders(email.Placeholders, map[string]interface{}{
			EpisodeNoticePlaceholder: text.notice(when),
			EpisodeTitlePlaceholder:  text.label,
			EpisodeTonePlaceholder:   text.tone,
		})
		d.EmailCopy = &email
	}
	return d
}

func sinceLabel(since time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	local := since.In(loc)
	return local.Format("02/01/2006") + " às " + local.Format("15:04")
}

func withStepPlaceholders(original, step map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(original)+len(step))
	for k, v := range original {
		out[k] = v
	}
	for k, v := range step {
		out[k] = v
	}
	return out
}
