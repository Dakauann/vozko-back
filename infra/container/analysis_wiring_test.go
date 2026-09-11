package container

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
)

// The wiring that decides whether an operator's analysis switch does anything.
//
// This area has already shipped the same bug twice: a channel that reached one
// half of the pipeline and not the other, and a campaign toggle nothing behind
// it ever read. Neither failed loudly. The conversation was simply never
// classified, or was classified with the wrong campaign's configuration, and
// the only symptom was an empty dashboard.

type recordingSink struct {
	registered map[shared.EntryType]conversation_usecase.AnalysisSubjectResolver
}

func newRecordingSink() *recordingSink {
	return &recordingSink{registered: map[shared.EntryType]conversation_usecase.AnalysisSubjectResolver{}}
}

func (s *recordingSink) SetAnalysisSubjectResolver(entry shared.EntryType, resolver conversation_usecase.AnalysisSubjectResolver) {
	s.registered[entry] = resolver
}

func resolverFor(entry shared.EntryType) conversation_usecase.AnalysisSubjectResolver {
	return func(context.Context, string) (*conversation_usecase.AnalysisSubject, error) {
		return &conversation_usecase.AnalysisSubject{EntryID: string(entry), EntryType: entry}, nil
	}
}

// Every channel must reach EVERY sink. A channel the sweep knows about but the
// adapter does not is enriched and never classified; the reverse is classified
// with the channel's defaults instead of the campaign's.
func TestRegisterAnalysisChannelsGivesEverySinkEveryChannel(t *testing.T) {
	channels := []analysisChannel{
		{shared.EntryTypeWhatsApp, resolverFor(shared.EntryTypeWhatsApp)},
		{shared.EntryTypeInstagram, resolverFor(shared.EntryTypeInstagram)},
		{shared.EntryTypeTelegram, resolverFor(shared.EntryTypeTelegram)},
		{shared.EntryTypeUnofficialWhatsApp, resolverFor(shared.EntryTypeUnofficialWhatsApp)},
	}
	sweep, adapter := newRecordingSink(), newRecordingSink()

	registerAnalysisChannels(channels, sweep, adapter)

	for _, sink := range []*recordingSink{sweep, adapter} {
		if len(sink.registered) != len(channels) {
			t.Fatalf("a sink received %d of %d channels", len(sink.registered), len(channels))
		}
		for _, ch := range channels {
			if _, ok := sink.registered[ch.entry]; !ok {
				t.Errorf("%s never reached one of the sinks, so its analysis switch is read by half the pipeline", ch.entry)
			}
		}
	}
}

// A deployment without the analysis engine registers on the sweep alone. That
// is a real configuration, not a bug, and it must not panic on the sink that
// is not there.
func TestRegisterAnalysisChannelsToleratesAMissingSink(t *testing.T) {
	sweep := newRecordingSink()
	registerAnalysisChannels(
		[]analysisChannel{{shared.EntryTypeWhatsApp, resolverFor(shared.EntryTypeWhatsApp)}},
		sweep, nil,
	)
	if len(sweep.registered) != 1 {
		t.Fatalf("the present sink got %d channels", len(sweep.registered))
	}
}

// The adapter spells the method differently, so it reaches the fan-out through
// a shim. If that shim stopped registering, every channel would silently be
// enriched and never classified.
func TestAdapterSinkRegistersOnTheAnalysisAdapter(t *testing.T) {
	adapter := conversation_usecase.NewAnalysisAdapter(nil, nil)
	registerAnalysisChannels(
		[]analysisChannel{{shared.EntryTypeTelegram, resolverFor(shared.EntryTypeTelegram)}},
		adapterSink{adapter},
	)

	// Asked back through the adapter's own surface rather than its internals:
	// an unregistered channel is a quiet no-op, so a registered one has to be
	// distinguishable by behaviour.
	transcripts, err := adapter.ReadTranscripts(context.Background(), telegramConversationRef(), []string{"entry-1"})
	if err != nil {
		t.Fatalf("ReadTranscripts on a registered channel: %v", err)
	}
	// No message repository, so there is no transcript to return; the point is
	// that the call resolved the subject rather than skipping the channel.
	if transcripts == nil {
		t.Error("the registered channel was not reachable through the adapter")
	}
}

// Campaign conversations are configured by their CAMPAIGN, not by the instance
// they run on. Without campaigns available the resolver is passed through
// unchanged rather than wrapped in something that can only answer nil.
func TestCampaignAwareResolverPassesThroughWithoutCampaigns(t *testing.T) {
	base := resolverFor(shared.EntryTypeUnofficialWhatsApp)

	for name, bundle := range map[string]*unofficialWhatsAppCampaignBundle{
		"no bundle":       nil,
		"bundle no repos": {},
	} {
		subject, err := campaignAwareResolver(base, bundle)(context.Background(), "conv-1")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if subject == nil || subject.EntryType != shared.EntryTypeUnofficialWhatsApp {
			t.Errorf("%s: the channel's own resolver stopped answering: %+v", name, subject)
		}
	}
}

func telegramConversationRef() ca.ContainerRef {
	return ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceTelegram,
		AccountID: "ws-1", ContainerID: "camp-1",
	}
}
