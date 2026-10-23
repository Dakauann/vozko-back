package carriercampaign

import "vozko/domain/selection"

func (c Campaign) ConfirmAudience(matched int) error {
	audience := c.Audience.Inferred()
	if err := audience.ValidateConfirmed(); err != nil {
		return err
	}
	return audience.ConfirmCount(matched)
}

func (c Campaign) AudienceFingerprint() string {
	audience := c.Audience.Inferred()
	if audience.Mode == "" || audience.Mode == selection.ModeIDs {
		return ""
	}
	return selection.Fingerprint(audience.EffectiveFilter())
}
