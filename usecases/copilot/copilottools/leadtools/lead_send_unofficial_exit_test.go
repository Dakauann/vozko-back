package leadtools

import (
	"fmt"
	"testing"

	"vozko/domain/copilot"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/copilot/copilottools"
)

func TestAnUnofficialSendWhoseMessageHidesTheExitTellsEloHowToFixIt(t *testing.T) {
	res := leadActionFailure(fmt.Errorf("plan: %w", uwc.ErrCampaignNeedsExit))
	if res.Status != copilot.StatusError || res.Message != copilottools.UnofficialMessageNeedsExit {
		t.Fatalf("result = %+v", res)
	}
}
