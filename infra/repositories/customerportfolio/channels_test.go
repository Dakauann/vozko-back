package customerportfolio_repository

import (
	"testing"

	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

func TestEveryCarteiraContactChannelHasTablesAndADisplay(t *testing.T) {
	displayed := map[shared.EntryType]bool{}
	for _, d := range contactDisplays {
		displayed[d.channel] = true
	}
	for _, channel := range shared.KnownEntryTypes() {
		accepted := cp.ContactSubject(channel, "contact-1").Validate() == nil
		_, mapped := infracrmfilter.ContactsOf(channel)
		if accepted != mapped || accepted != displayed[channel] {
			t.Errorf("%s: accepted by the domain=%v, mapped by crmfilter=%v, displayed=%v; all three must agree", channel, accepted, mapped, displayed[channel])
		}
	}
}
