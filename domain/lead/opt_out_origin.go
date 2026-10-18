package lead

import "slices"

type OptOutOrigin string

const (
	OptOutTemplateWord  OptOutOrigin = "template_word"
	OptOutCampaignWord  OptOutOrigin = "campaign_word"
	OptOutWorkspaceWord OptOutOrigin = "workspace_word"
)

var optOutOrigins = []OptOutOrigin{
	OptOutOrigin(ConsentButtonReply), OptOutTemplateWord, OptOutCampaignWord, OptOutWorkspaceWord, OptOutOrigin(ConsentKeyword),
	OptOutOrigin(ConsentAIAgent), OptOutOrigin(ConsentWorkflow), OptOutOrigin(ConsentManual),
	OptOutOrigin(ConsentConversationRequest), OptOutOrigin(ConsentElo), OptOutOrigin(ConsentAPI),
}

func OptOutOrigins() []OptOutOrigin { return slices.Clone(optOutOrigins) }

func (o OptOutOrigin) Valid() bool {
	if !slices.Contains(optOutOrigins, o) {
		return false
	}
	rule, known := consentRules[o.Source()]
	return known && rule.withdraw != nil
}

func (o OptOutOrigin) Source() ConsentSource {
	if _, word := o.WordTrigger(); word {
		return ConsentKeyword
	}
	return ConsentSource(o)
}

func (o OptOutOrigin) WordTrigger() (string, bool) {
	switch o {
	case OptOutTemplateWord, OptOutCampaignWord, OptOutWorkspaceWord:
		return string(o), true
	}
	return "", false
}
