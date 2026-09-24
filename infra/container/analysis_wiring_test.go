package container

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
)

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

func TestAdapterSinkRegistersOnTheAnalysisAdapter(t *testing.T) {
	adapter := conversation_usecase.NewAnalysisAdapter(nil, nil)
	registerAnalysisChannels(
		[]analysisChannel{{shared.EntryTypeTelegram, resolverFor(shared.EntryTypeTelegram)}},
		adapterSink{adapter},
	)

	transcripts, err := adapter.ReadTranscripts(context.Background(), telegramConversationRef(), []string{"entry-1"})
	if err != nil {
		t.Fatalf("ReadTranscripts on a registered channel: %v", err)
	}
	if transcripts == nil {
		t.Error("the registered channel was not reachable through the adapter")
	}
}

func TestCampaignAwareResolverPassesThroughWithoutCampaigns(t *testing.T) {
	base := resolverFor(shared.EntryTypeUnofficialWhatsApp)

	for name, bundle := range map[string]*unofficialWhatsAppCampaignBundle{
		"no bundle":       nil,
		"bundle no repos": {},
	} {
		subject, err := campaignAwareResolver(base, nil, bundle)(context.Background(), "conv-1")
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
