package geocoding_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
)

func keyOf(c geocoding.Claim) geocoding.AnswerKey {
	return geocoding.AnswerKey{WorkspaceID: c.WorkspaceID, Fingerprint: c.Fingerprint}
}

func TestChainAppliesAStoredAnswerWithoutASlotOrACall(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	c := claimOf("a-1", "ws-1", paulista("Av. Paulista"))
	stored, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, keyOf(c), geo.Located(providerFix(geo.PrecisionAddress)), now.Add(-24*time.Hour))
	s.answers.stored[keyOf(c)] = stored
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{c}, now.Add(time.Minute))
	r := settlementOf(t, got.Settlements, "a-1").Resolution
	if r.Address.GeoStatus != lead.GeoLocated || r.Address.Fix.Source != geo.SourceProvider || r.Address.Fix.Precision != geo.PrecisionAddress || r.NextAt != nil {
		t.Fatalf("resolution = %+v, want the stored provider position", r)
	}
	if len(s.provider.calls) != 0 || s.usage.takes != 0 {
		t.Fatalf("provider calls = %d, slots = %d, want neither for a stored answer", len(s.provider.calls), s.usage.takes)
	}
	if s.metrics.reused != 1 || s.answers.reads != 1 || len(s.answers.remembered) != 0 {
		t.Fatalf("reused = %d, reads = %d, remembered = %d, want one reuse from one read and nothing written", s.metrics.reused, s.answers.reads, len(s.answers.remembered))
	}
}

func TestChainServesAStoredAnswerPastTheDeadlineAndUnderAPause(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	pause, _ := geocoding.PauseAfter(geocoding.ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), now)
	s.pauses.pause = &pause
	c := claimOf("a-1", "ws-1", paulista("Av. Paulista"))
	stored, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, keyOf(c), geo.NotFound(), now)
	s.answers.stored[keyOf(c)] = stored
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{c}, now.Add(-time.Second))
	if r := settlementOf(t, got.Settlements, "a-1").Resolution; r.Address.GeoStatus != lead.GeoApproximate || r.NextAt != nil {
		t.Fatalf("resolution = %+v, want the stored no-result answer with the reference point", r)
	}
	if len(s.provider.calls) != 0 || s.usage.takes != 0 {
		t.Fatal("a stored answer never calls the provider")
	}
}

func TestChainNeverLooksUpAnotherWorkspacesAnswer(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.settings.byWorkspace["ws-2"] = enabled()
	mine := claimOf("a-1", "ws-1", paulista("Av. Paulista"))
	theirs := claimOf("a-2", "ws-2", paulista("Av. Paulista"))
	stored, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, keyOf(theirs), geo.Located(providerFix(geo.PrecisionAddress)), now)
	s.answers.stored[keyOf(theirs)] = stored
	s.chain(t).Resolve(context.Background(), []geocoding.Claim{mine}, now.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("provider calls = %d, slots = %d, want the workspace to pay for its own answer", len(s.provider.calls), s.usage.takes)
	}
}

func TestChainLooksUpAnswersOnlyForEnabledWorkspacesThatWantTheProvider(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-on"] = enabled()
	tight := paulista("Av. Paulista")
	tight.ZipCode = "01310100"
	s.chain(t).Resolve(context.Background(), []geocoding.Claim{
		claimOf("off", "ws-off", paulista("Rua A")),
		claimOf("tight", "ws-on", tight),
	}, now.Add(time.Minute))
	if s.answers.reads != 0 {
		t.Fatalf("answer reads = %d, want none when no claim may reach the provider", s.answers.reads)
	}
}

func TestChainStoresTheAnswerRightAfterTheCallSoTheSameTextPaysOnce(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	first := claimOf("a-1", "ws-1", paulista("Av. Paulista"))
	sameBuilding := claimOf("a-2", "ws-1", paulista("Av. Paulista"))
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{first, sameBuilding}, now.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("provider calls = %d, slots = %d, want one paid call for two addresses with the same text", len(s.provider.calls), s.usage.takes)
	}
	for _, id := range []string{"a-1", "a-2"} {
		if r := settlementOf(t, got.Settlements, id).Resolution; r.Address.GeoStatus != lead.GeoLocated || r.Address.Fix.Source != geo.SourceProvider {
			t.Fatalf("%s = %+v, want the provider's position", id, r.Address)
		}
	}
	if len(s.answers.remembered) != 1 || s.answers.remembered[0].Key != keyOf(first) || s.answers.remembered[0].Kind != geocoding.AnswerLocated {
		t.Fatalf("remembered = %+v, want the located answer stored once", s.answers.remembered)
	}

	retry := newSetup()
	retry.settings.byWorkspace["ws-1"] = enabled()
	retry.answers = s.answers
	retry.chain(t).Resolve(context.Background(), []geocoding.Claim{first}, now.Add(time.Minute))
	if len(retry.provider.calls) != 0 || retry.usage.takes != 0 {
		t.Fatalf("a batch claimed again after a failed write-back paid %d calls and %d slots, want none", len(retry.provider.calls), retry.usage.takes)
	}
}

func TestChainStoresEveryProviderAnswerAndNeverAFailure(t *testing.T) {
	tests := []struct {
		name    string
		outcome geo.Outcome
		kind    geocoding.AnswerKind
	}{
		{"located", geo.Located(providerFix(geo.PrecisionStreet)), geocoding.AnswerLocated},
		{"ambiguous", geo.Ambiguous(), geocoding.AnswerAmbiguous},
		{"no result", geo.NotFound(), geocoding.AnswerNotFound},
		{"a refused query text", geo.Unavailable(geo.ReasonQueryRefused, 0), geocoding.AnswerRefused},
		{"a provider that is down", geo.Unavailable(geo.ReasonProviderDown, 0), ""},
		{"a rate limit", geo.Unavailable(geo.ReasonRateLimited, time.Minute), ""},
		{"a refused account", geo.Unavailable(geo.ReasonKeyRejected, 0), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSetup()
			s.settings.byWorkspace["ws-1"] = enabled()
			s.provider.outcome = tt.outcome
			s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
			if tt.kind == "" {
				if len(s.answers.remembered) != 0 {
					t.Fatalf("remembered %+v, want a failure never stored", s.answers.remembered)
				}
				return
			}
			if len(s.answers.remembered) != 1 || s.answers.remembered[0].Kind != tt.kind || s.answers.remembered[0].Provider != geocoding.ProviderOpenCage {
				t.Fatalf("remembered %+v, want one %q answer", s.answers.remembered, tt.kind)
			}
		})
	}
}

func TestChainAppliesAnAnswerItCouldNotStore(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.answers.writeErr = errDown
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
	if r := settlementOf(t, got.Settlements, "a-1").Resolution; r.Address.GeoStatus != lead.GeoLocated {
		t.Fatalf("resolution = %+v, want the paid answer applied even when it could not be stored", r)
	}
}

func TestChainNeverRetriesARefusedQueryText(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.provider.outcome = geo.Unavailable(geo.ReasonQueryRefused, 0)
	first := claimOf("a-1", "ws-1", paulista("Rua A"))
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{first}, now.Add(time.Minute))
	r := settlementOf(t, got.Settlements, "a-1").Resolution
	if r.Address.GeoStatus != lead.GeoRefused || r.NextAt != nil || r.Address.Fix == nil || r.Address.Fix.Source != geo.SourceReference {
		t.Fatalf("resolution = %+v, want refused, out of the queue, with the reference point kept", r)
	}
	sameText := claimOf("a-9", "ws-1", paulista("Rua A"))
	again := s.chain(t).Resolve(context.Background(), []geocoding.Claim{sameText}, now.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("provider calls = %d, slots = %d, want the refused text never paid again", len(s.provider.calls), s.usage.takes)
	}
	if r := settlementOf(t, again.Settlements, "a-9").Resolution; r.Address.GeoStatus != lead.GeoRefused || r.NextAt != nil {
		t.Fatalf("same text = %+v, want refused from the stored answer", r)
	}
	if len(s.pauses.opened) != 0 {
		t.Fatalf("pauses = %+v, want a refused text never to pause the provider", s.pauses.opened)
	}
}

func TestChainReportsHowLongTheProviderStaysPaused(t *testing.T) {
	t.Run("the replica that opens the pause", func(t *testing.T) {
		s := newSetup()
		s.settings.byWorkspace["ws-1"] = enabled()
		s.provider.outcome = geo.Unavailable(geo.ReasonKeyRejected, 0)
		s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
		if got := s.metrics.pausedUntil["opencage:key_rejected"]; !got.Equal(now.Add(geocoding.AccountPause)) {
			t.Fatalf("paused until = %v, want the end of the pause just opened", s.metrics.pausedUntil)
		}
	})
	t.Run("a replica that finds the pause another opened", func(t *testing.T) {
		s := newSetup()
		s.settings.byWorkspace["ws-1"] = enabled()
		pause, _ := geocoding.PauseAfter(geocoding.ProviderOpenCage, geo.Unavailable(geo.ReasonKeyDisabled, 0), now.Add(-time.Minute))
		s.pauses.pause = &pause
		s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
		if got := s.metrics.pausedUntil["opencage:key_disabled"]; !got.Equal(pause.Until) || len(s.metrics.pausedUntil) != 1 {
			t.Fatalf("paused until = %v, want the shared pause's end", s.metrics.pausedUntil)
		}
	})
	t.Run("no pause reports nothing", func(t *testing.T) {
		s := newSetup()
		s.settings.byWorkspace["ws-1"] = enabled()
		s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
		if len(s.metrics.pausedUntil) != 0 {
			t.Fatalf("paused until = %v, want nothing without a pause", s.metrics.pausedUntil)
		}
	})
}

func TestChainPausesTheProviderForEveryWorkspaceOnAnAccountRefusal(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.settings.byWorkspace["ws-2"] = enabled()
	s.provider.outcome = geo.Unavailable(geo.ReasonKeyRejected, 0)
	claims := []geocoding.Claim{
		claimOf("a-1", "ws-1", paulista("Rua A")),
		claimOf("a-2", "ws-1", paulista("Rua B")),
		claimOf("b-1", "ws-2", paulista("Rua C")),
	}
	got := s.chain(t).Resolve(context.Background(), claims, now.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("provider calls = %d, slots = %d, want one refused call and nothing after it", len(s.provider.calls), s.usage.takes)
	}
	if len(s.pauses.opened) != 1 || s.pauses.opened[0].Reason != geo.ReasonKeyRejected || !s.pauses.opened[0].Until.Equal(now.Add(geocoding.AccountPause)) {
		t.Fatalf("opened = %+v, want one shared pause for the rejected key", s.pauses.opened)
	}
	for _, id := range []string{"a-1", "a-2", "b-1"} {
		r := settlementOf(t, got.Settlements, id).Resolution
		if r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil || r.NextAt.Before(now.Add(geocoding.AccountPause)) {
			t.Fatalf("%s = %+v, want unavailable until the pause ends", id, r)
		}
	}
	if s.metrics.pauses["opencage:key_rejected"] != 1 || len(s.answers.remembered) != 0 {
		t.Fatalf("pause metrics = %+v, remembered = %+v, want one pause counted and nothing stored", s.metrics.pauses, s.answers.remembered)
	}

	later := newSetup()
	later.settings.byWorkspace["ws-3"] = enabled()
	later.pauses = s.pauses
	next := later.chain(t).Resolve(context.Background(), []geocoding.Claim{
		claimOf("c-1", "ws-3", paulista("Rua D")),
		claimOf("c-2", "ws-3", paulista("Rua E")),
	}, now.Add(time.Minute))
	if len(later.provider.calls) != 0 || later.usage.takes != 0 {
		t.Fatalf("the next batch paid %d calls and %d slots under an open pause, want none", len(later.provider.calls), later.usage.takes)
	}
	if r := settlementOf(t, next.Settlements, "c-1").Resolution; r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil {
		t.Fatalf("resolution = %+v, want unavailable while paused", r)
	}
	if later.pauses.reads != 2 {
		t.Fatalf("pause reads = %d, want one read per batch", later.pauses.reads)
	}
}

func TestChainKeepsThePauseForTheBatchWhenItCannotBeShared(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.provider.outcome = geo.Unavailable(geo.ReasonKeyDisabled, 0)
	s.pauses.openErr = errDown
	s.chain(t).Resolve(context.Background(), []geocoding.Claim{
		claimOf("a-1", "ws-1", paulista("Rua A")),
		claimOf("a-2", "ws-1", paulista("Rua B")),
	}, now.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("provider calls = %d, slots = %d, want the batch to stop even when the pause could not be stored", len(s.provider.calls), s.usage.takes)
	}
}

func TestChainStopsCallingAsSoonAsTheSettingsChange(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.usage.moved = map[string]bool{"ws-1": true}
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{
		claimOf("a-1", "ws-1", paulista("Rua A")),
		claimOf("a-2", "ws-1", paulista("Rua B")),
	}, now.Add(time.Minute))
	if len(s.provider.calls) != 0 {
		t.Fatalf("provider calls = %d, want none once the slot statement saw the new settings", len(s.provider.calls))
	}
	if s.usage.takes != 1 {
		t.Fatalf("slots asked = %d, want the workspace left alone for the rest of the batch", s.usage.takes)
	}
	for _, id := range []string{"a-1", "a-2"} {
		r := settlementOf(t, got.Settlements, id).Resolution
		if r.Address.GeoStatus != lead.GeoPending || r.NextAt == nil || !r.NextAt.Equal(now) {
			t.Fatalf("%s = %+v, want it back in the queue to be read under the new settings", id, r)
		}
	}
	if len(s.metrics.quotaHits) != 0 {
		t.Fatalf("quota hits = %+v, want a settings change never counted as a quota hit", s.metrics.quotaHits)
	}
}

func (s *chainSetup) chainOnClock(t *testing.T, clock *time.Time) *Chain {
	t.Helper()
	c, err := NewChain(ChainDeps{
		Reference: s.reference,
		Providers: map[geocoding.Provider]geo.Geocoder{geocoding.ProviderOpenCage: s.provider},
		Settings:  s.settings,
		Usage:     s.usage,
		Metrics:   s.metrics,
		Answers:   s.answers,
		Pauses:    s.pauses,
		Now:       func() time.Time { return *clock },
	})
	if err != nil {
		t.Fatalf("NewChain() err = %v", err)
	}
	return c
}

func TestChainHoldsAPauseItCouldNotShareOnThisReplicaUntilThePauseEnds(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.settings.byWorkspace["ws-2"] = enabled()
	s.provider.outcome = geo.Unavailable(geo.ReasonKeyDisabled, 0)
	s.pauses.openErr = errDown
	clock := now
	chain := s.chainOnClock(t, &clock)
	chain.Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, clock.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 || s.pauses.pause != nil {
		t.Fatalf("provider calls = %d, slots = %d, shared = %+v, want one refused call and a pause that could not be shared", len(s.provider.calls), s.usage.takes, s.pauses.pause)
	}

	clock = now.Add(10 * time.Minute)
	next := chain.Resolve(context.Background(), []geocoding.Claim{claimOf("b-1", "ws-2", paulista("Rua B"))}, clock.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("the next batch paid %d calls and %d slots while this replica knows the key is refused, want none", len(s.provider.calls), s.usage.takes)
	}
	r := settlementOf(t, next.Settlements, "b-1").Resolution
	if r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil || r.NextAt.Before(now.Add(geocoding.AccountPause)) {
		t.Fatalf("resolution = %+v, want unavailable until the local pause ends", r)
	}
	if len(s.pauses.opened) != 2 {
		t.Fatalf("share attempts = %d, want the unshared pause offered again on the next batch", len(s.pauses.opened))
	}

	s.pauses.openErr = nil
	clock = now.Add(20 * time.Minute)
	chain.Resolve(context.Background(), []geocoding.Claim{claimOf("b-2", "ws-2", paulista("Rua C"))}, clock.Add(time.Minute))
	if s.pauses.pause == nil || s.pauses.pause.Reason != geo.ReasonKeyDisabled || !s.pauses.pause.Until.Equal(now.Add(geocoding.AccountPause)) {
		t.Fatalf("shared = %+v, want the local pause shared once the store accepts it", s.pauses.pause)
	}
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatal("no call while the pause holds")
	}

	s.provider.outcome = geo.Located(providerFix(geo.PrecisionAddress))
	clock = now.Add(geocoding.AccountPause)
	chain.Resolve(context.Background(), []geocoding.Claim{claimOf("b-3", "ws-2", paulista("Rua D"))}, clock.Add(time.Minute))
	if len(s.provider.calls) != 2 || s.usage.takes != 2 {
		t.Fatalf("provider calls = %d, slots = %d, want calls again once the pause ended", len(s.provider.calls), s.usage.takes)
	}
}

func TestChainHoldsTheLongerOfTheLocalAndTheSharedPause(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.provider.outcome = geo.Unavailable(geo.ReasonAccountQuotaSpent, 5*time.Hour)
	s.pauses.openErr = errDown
	clock := now
	chain := s.chainOnClock(t, &clock)
	chain.Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, clock.Add(time.Minute))
	shorter, _ := geocoding.PauseAfter(geocoding.ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), now)
	s.pauses.pause, s.pauses.openErr = &shorter, nil
	clock = now.Add(2 * time.Hour)
	got := chain.Resolve(context.Background(), []geocoding.Claim{claimOf("a-2", "ws-1", paulista("Rua B"))}, clock.Add(time.Minute))
	if len(s.provider.calls) != 1 || s.usage.takes != 1 {
		t.Fatalf("provider calls = %d, slots = %d, want none while the longer local pause holds", len(s.provider.calls), s.usage.takes)
	}
	if r := settlementOf(t, got.Settlements, "a-2").Resolution; r.NextAt == nil || r.NextAt.Before(now.Add(5*time.Hour)) {
		t.Fatalf("resolution = %+v, want it held until the longer pause ends", r)
	}
	if s.pauses.pause.Until.Before(now.Add(5 * time.Hour)) {
		t.Fatalf("shared = %+v, want the longer pause shared", s.pauses.pause)
	}
}

func TestChainPausesTheProviderWhenSeveralTextsAreRefusedInOneBatch(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.settings.byWorkspace["ws-2"] = enabled()
	s.provider.outcome = geo.Unavailable(geo.ReasonQueryRefused, 0)
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{
		claimOf("a-1", "ws-1", paulista("Rua A")),
		claimOf("b-1", "ws-2", paulista("Rua B")),
		claimOf("a-2", "ws-1", paulista("Rua C")),
	}, now.Add(time.Minute))
	if len(s.provider.calls) != geocoding.QueryRefusalsBeforePause || s.usage.takes != geocoding.QueryRefusalsBeforePause {
		t.Fatalf("provider calls = %d, slots = %d, want the calls to stop once refusals of distinct texts point at the request", len(s.provider.calls), s.usage.takes)
	}
	if r := settlementOf(t, got.Settlements, "a-1").Resolution; r.Address.GeoStatus != lead.GeoRefused {
		t.Fatalf("first refused text = %+v, want it settled as refused", r)
	}
	for _, id := range []string{"b-1", "a-2"} {
		r := settlementOf(t, got.Settlements, id).Resolution
		if r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil || r.NextAt.Before(now.Add(geocoding.AccountPause)) {
			t.Fatalf("%s = %+v, want it kept in the queue until the pause ends, never marked refused", id, r)
		}
	}
	if len(s.answers.remembered) != 1 || s.answers.remembered[0].Key.Fingerprint != claimOf("a-1", "ws-1", paulista("Rua A")).Fingerprint {
		t.Fatalf("remembered = %+v, want only the first refused text stored", s.answers.remembered)
	}
	if len(s.pauses.opened) != 1 || s.pauses.opened[0].Reason != geo.ReasonQueriesRefused || s.metrics.pauses["opencage:queries_refused"] != 1 {
		t.Fatalf("opened = %+v, metrics = %+v, want one pause for refused texts, counted", s.pauses.opened, s.metrics.pauses)
	}
}

func TestChainReadsStoredAnswersOnceForTheWholeBatchWithEachKeyOnce(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.settings.byWorkspace["ws-2"] = enabled()
	claims := []geocoding.Claim{
		claimOf("a-1", "ws-1", paulista("Rua A")),
		claimOf("a-2", "ws-1", paulista("Rua A")),
		claimOf("a-3", "ws-1", paulista("Rua B")),
		claimOf("b-1", "ws-2", paulista("Rua A")),
		claimOf("c-1", "ws-off", paulista("Rua A")),
	}
	s.chain(t).Resolve(context.Background(), claims, now.Add(time.Minute))
	if s.answers.reads != 1 || len(s.answers.asked) != 1 {
		t.Fatalf("answer reads = %d, want one for the whole batch", s.answers.reads)
	}
	want := map[geocoding.AnswerKey]bool{keyOf(claims[0]): true, keyOf(claims[2]): true, keyOf(claims[3]): true}
	asked := s.answers.asked[0]
	if len(asked) != len(want) {
		t.Fatalf("asked = %+v, want each enabled workspace's text once", asked)
	}
	for _, k := range asked {
		if !want[k] {
			t.Fatalf("asked = %+v, want only %+v", asked, want)
		}
	}
}
