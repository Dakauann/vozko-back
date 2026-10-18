package campaign

type LeadFactsGroup struct {
	Facts LeadFacts
	Leads int
}

type AudienceExits struct {
	OptedOut int `json:"optedOut"`
	Blocked  int `json:"blocked"`
}

func (a AudienceExits) NotReceiving() int {
	return a.OptedOut + a.Blocked
}

func TallyAudienceExits(groups []LeadFactsGroup) AudienceExits {
	var exits AudienceExits
	for _, group := range groups {
		switch LeadRefusal(group.Facts) {
		case SkipOptedOut:
			exits.OptedOut += group.Leads
		case SkipBlocked:
			exits.Blocked += group.Leads
		}
	}
	return exits
}
