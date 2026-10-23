package unofficial_whatsapp_campaign

import (
	"errors"

	"vozko/domain/campaign/launchcheck"
	uw "vozko/domain/unofficial_whatsapp"
)

func StartRefusalCode(err error) launchcheck.Code {
	var unusable *InstanceUnusableError
	switch {
	case errors.As(err, &unusable):
		return launchcheck.CodeNumberUnusable
	case errors.Is(err, uw.ErrRestrictedByWA):
		return launchcheck.CodeNumberRestricted
	default:
		return launchcheck.CodeNumberCannotSend
	}
}

func StartReport(pending int, refusal error, unsendableGroups int) launchcheck.Report {
	var report launchcheck.Report
	if pending <= 0 {
		report = report.With(launchcheck.Blocked(launchcheck.CodeAudienceEmpty, 0))
	}
	if refusal != nil {
		report = report.With(launchcheck.Blocked(StartRefusalCode(refusal), 0))
	}
	if unsendableGroups > 0 {
		report = report.With(launchcheck.Blocked(launchcheck.CodeGroupsUnsendable, float64(unsendableGroups)))
	}
	return report
}
