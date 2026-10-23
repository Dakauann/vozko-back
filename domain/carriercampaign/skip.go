package carriercampaign

import "vozko/domain/campaign"

type SkipReason string

const (
	SkipNoPhone         SkipReason = "no_phone"
	SkipOptedOut                   = SkipReason(campaign.SkipOptedOut)
	SkipBlocked                    = SkipReason(campaign.SkipBlocked)
	SkipSuppressed      SkipReason = "suppressed"
	SkipFrequencyCap    SkipReason = "frequency_cap"
	SkipDuplicate       SkipReason = "duplicate"
	SkipMissingVariable            = SkipReason(campaign.SkipMissingVariable)
	SkipSenderPaused    SkipReason = "sender_paused"
	SkipBalance         SkipReason = "balance"
)

func AllSkipReasons() []SkipReason {
	return []SkipReason{
		SkipNoPhone, SkipOptedOut, SkipBlocked, SkipSuppressed, SkipFrequencyCap,
		SkipDuplicate, SkipMissingVariable, SkipSenderPaused, SkipBalance,
	}
}

func (r SkipReason) Valid() bool {
	for _, known := range AllSkipReasons() {
		if r == known {
			return true
		}
	}
	return false
}

var sharedSkips = map[SkipReason]campaign.SkipReason{
	SkipNoPhone:         campaign.SkipNoIdentity,
	SkipOptedOut:        campaign.SkipOptedOut,
	SkipBlocked:         campaign.SkipBlocked,
	SkipMissingVariable: campaign.SkipMissingVariable,
}

func (r SkipReason) Shared() (campaign.SkipReason, bool) {
	reason, ok := sharedSkips[r]
	return reason, ok
}

func SkipFromShared(reason campaign.SkipReason) (SkipReason, bool) {
	for local, known := range sharedSkips {
		if known == reason {
			return local, true
		}
	}
	return "", false
}

type RecipientFacts struct {
	Lead             campaign.LeadFacts
	Suppressed       bool
	Duplicate        bool
	MissingVariable  bool
	OverFrequencyCap bool
	SenderPaused     bool
	BalanceShort     bool
}

func SkipFor(kind Kind, f RecipientFacts) SkipReason {
	if refusal := campaign.LeadRefusal(f.Lead); refusal != "" {
		local, _ := SkipFromShared(refusal)
		return local
	}
	switch {
	case f.Suppressed:
		return SkipSuppressed
	case f.Duplicate:
		return SkipDuplicate
	case f.MissingVariable:
		return SkipMissingVariable
	case f.OverFrequencyCap && kind.ObeysMarketingRules():
		return SkipFrequencyCap
	case f.SenderPaused:
		return SkipSenderPaused
	case f.BalanceShort:
		return SkipBalance
	}
	return ""
}
