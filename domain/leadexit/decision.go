package leadexit

import (
	"vozko/domain/buttonaction"
	"vozko/domain/lead"
)

type Trigger string

const (
	TriggerButton       Trigger = "button"
	TriggerTemplateWord Trigger = Trigger(lead.OptOutTemplateWord)
	TriggerCampaignWord Trigger = Trigger(lead.OptOutCampaignWord)
)

type TemplateWords struct {
	Template     buttonaction.TemplateRef
	Words        []string
	Confirmation string
}

type Candidates struct {
	Text      string
	Tap       *buttonaction.Resolution
	Templates []TemplateWords
	Campaign  *SentCampaignMessage
}

type Decision struct {
	Trigger      Trigger
	Kind         buttonaction.Kind
	Action       *buttonaction.ButtonAction
	Template     buttonaction.TemplateRef
	Word         string
	CampaignID   string
	Confirmation string
}

func Decide(c Candidates) (Decision, bool) {
	if d, ok := tapDecision(c); ok {
		return d, true
	}
	for _, tw := range c.Templates {
		if word, ok := matchedWord(c.Text, tw.Words); ok {
			d := wordDecision(TriggerTemplateWord, word)
			d.Template, d.Confirmation = tw.Template, tw.Confirmation
			return d, true
		}
	}
	return campaignDecision(c)
}

func campaignDecision(c Candidates) (Decision, bool) {
	if c.Campaign == nil {
		return Decision{}, false
	}
	word, ok := matchedWord(c.Text, c.Campaign.ShownWords())
	if !ok {
		return Decision{}, false
	}
	d := wordDecision(TriggerCampaignWord, word)
	d.CampaignID = c.Campaign.CampaignID
	return d, true
}

func wordDecision(trigger Trigger, word string) Decision {
	return Decision{Trigger: trigger, Kind: buttonaction.KindOptOut, Word: word}
}

func tapDecision(c Candidates) (Decision, bool) {
	if c.Tap == nil || c.Tap.Action == nil || c.Tap.Reason != buttonaction.ReasonResolved {
		return Decision{}, false
	}
	action := *c.Tap.Action
	d := Decision{Trigger: TriggerButton, Kind: action.Kind, Action: &action, Template: c.Tap.Template, Confirmation: action.Confirmation}
	if d.Confirmation == "" && action.Kind == buttonaction.KindOptOut {
		d.Confirmation = templateConfirmation(c.Templates, c.Tap.Template)
	}
	return d, true
}

func templateConfirmation(templates []TemplateWords, ref buttonaction.TemplateRef) string {
	for _, tw := range templates {
		if tw.Template == ref {
			return tw.Confirmation
		}
	}
	return ""
}

func matchedWord(text string, words []string) (string, bool) {
	for _, word := range words {
		if buttonaction.MatchesExitWord(text, []string{word}) {
			return word, true
		}
	}
	return "", false
}

func ButtonMatched(r *buttonaction.Resolution) bool {
	if r == nil {
		return false
	}
	switch r.Reason {
	case buttonaction.ReasonResolved, buttonaction.ReasonNoAction, buttonaction.ReasonStale:
		return true
	}
	return false
}
