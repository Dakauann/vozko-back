package unofficial_whatsapp_campaign

import "errors"

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrCampaignNeedsExit, "unofficial_campaign_needs_exit"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
