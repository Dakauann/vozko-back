package emailcampaign

import (
	"strings"

	"vozko/domain/campaignaudience"
	"vozko/domain/emaildesign"
)

func (c Campaign) AudienceSpec() (campaignaudience.Spec, bool) {
	switch {
	case c.Selection != nil:
		return campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: c.Selection, Variables: c.MergePolicies}, true
	case strings.TrimSpace(c.ListID) != "":
		return campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: c.ListID, Variables: c.MergePolicies}, true
	}
	return campaignaudience.Spec{}, false
}

func (c Campaign) AudienceOwner() string {
	return campaignaudience.EmailCampaignOwner(c.ID)
}

func (c Campaign) MergeSubjects() string {
	subjects := []string{c.Subject}
	if c.ABTest != nil {
		subjects = append(subjects, c.ABTest.VariantBSubject)
	}
	return strings.Join(subjects, "\n")
}

func (c Campaign) Personalize(p emaildesign.Personalization) emaildesign.Personalization {
	for _, policy := range c.MergePolicies {
		if policy.Missing != campaignaudience.MissingText {
			continue
		}
		if p.Fallbacks == nil {
			p.Fallbacks = map[string]string{}
		}
		p.Fallbacks[policy.Key] = policy.Text
	}
	return p
}

func (c Campaign) MissingExcluded(p emaildesign.Personalization) []string {
	var missing []string
	for _, policy := range c.MergePolicies {
		if policy.Missing == campaignaudience.MissingExclude && p.Value(policy.Key) == "" {
			missing = append(missing, policy.Key)
		}
	}
	return missing
}
