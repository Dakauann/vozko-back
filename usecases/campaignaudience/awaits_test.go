package campaignaudience_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
)

func TestTheCountRunsBeforeTheTemplateAndOnlyTheCreateWaitsForIt(t *testing.T) {
	w := newWorld(t)
	w.selections.ids = []string{"a", "b"}
	w.channel.awaits = campaignaudience.BlockerTemplateRequired
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{"a": {HasContact: true}, "b": {HasContact: true, Refusal: string(campaign.SkipOptedOut)}}}
	spec := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: confirmed(everyoneSelection(), 2)}
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if count.Matched != 2 || count.Sendable == nil || *count.Sendable != 1 || count.Out[string(campaign.SkipOptedOut)] != 1 || len(count.Variables) != 0 {
		t.Fatalf("count = %+v", count)
	}
	if !reflect.DeepEqual(count.Blockers, []campaignaudience.Blocker{campaignaudience.BlockerTemplateRequired}) {
		t.Fatalf("blockers = %v", count.Blockers)
	}
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec); !errors.Is(err, campaignaudience.ErrTemplateRequired) {
		t.Fatalf("recipients = %v", err)
	}
}
