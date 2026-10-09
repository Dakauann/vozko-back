package geocoding_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
)

type chainSetup struct {
	reference *fakeReference
	provider  *fakeProvider
	settings  *fakeSettings
	usage     *fakeUsage
	metrics   *fakeMetrics
	notifier  *fakeNotifier
	answers   *fakeAnswers
	pauses    *fakePauses
}

func newSetup() *chainSetup {
	return &chainSetup{
		reference: loadedReference(),
		provider:  &fakeProvider{outcome: geo.Located(providerFix(geo.PrecisionAddress))},
		settings:  &fakeSettings{byWorkspace: map[string]geocoding.Settings{}},
		usage:     &fakeUsage{used: map[string]geocoding.Usage{}},
		metrics:   newMetrics(),
		notifier:  &fakeNotifier{},
		answers:   newAnswers(),
		pauses:    &fakePauses{},
	}
}

func (s *chainSetup) chain(t *testing.T) *Chain {
	t.Helper()
	c, err := NewChain(ChainDeps{
		Reference: s.reference,
		Providers: map[geocoding.Provider]geo.Geocoder{geocoding.ProviderOpenCage: s.provider},
		Settings:  s.settings,
		Usage:     s.usage,
		Metrics:   s.metrics,
		Answers:   s.answers,
		Pauses:    s.pauses,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewChain() err = %v", err)
	}
	return c
}

func settlementOf(t *testing.T, got []geocoding.Settlement, id string) geocoding.Settlement {
	t.Helper()
	for _, s := range got {
		if s.Claim.AddressID == id {
			return s
		}
	}
	t.Fatalf("no settlement for %s in %+v", id, got)
	return geocoding.Settlement{}
}

func TestNewChainRefusesMissingDependencies(t *testing.T) {
	s := newSetup()
	full := ChainDeps{Reference: s.reference, Settings: s.settings, Usage: s.usage, Metrics: s.metrics, Answers: s.answers, Pauses: s.pauses}
	for name, deps := range map[string]ChainDeps{
		"reference": {Settings: s.settings, Usage: s.usage, Metrics: s.metrics, Answers: s.answers, Pauses: s.pauses},
		"settings":  {Reference: s.reference, Usage: s.usage, Metrics: s.metrics, Answers: s.answers, Pauses: s.pauses},
		"usage":     {Reference: s.reference, Settings: s.settings, Metrics: s.metrics, Answers: s.answers, Pauses: s.pauses},
		"metrics":   {Reference: s.reference, Settings: s.settings, Usage: s.usage, Answers: s.answers, Pauses: s.pauses},
		"answers":   {Reference: s.reference, Settings: s.settings, Usage: s.usage, Metrics: s.metrics, Pauses: s.pauses},
		"pauses":    {Reference: s.reference, Settings: s.settings, Usage: s.usage, Metrics: s.metrics, Answers: s.answers},
	} {
		if _, err := NewChain(deps); err == nil {
			t.Fatalf("NewChain() without %s must refuse", name)
		}
	}
	if _, err := NewChain(full); err != nil {
		t.Fatalf("NewChain() without providers is a server with no external provider: err = %v", err)
	}
}

func TestChainUsesTheReferenceAloneWhenItPinsTheStreet(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	tight := paulista("Av. Paulista")
	tight.ZipCode = "01310100"
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", tight)}, now.Add(time.Minute))
	r := settlementOf(t, got.Settlements, "a-1").Resolution
	if r.Address.GeoStatus != lead.GeoLocated || r.Address.Fix.Precision != geo.PrecisionStreet || r.Address.Fix.Source != geo.SourceReference {
		t.Fatalf("resolution = %+v, want the street point from the reference", r.Address)
	}
	if len(s.provider.calls) != 0 || s.usage.takes != 0 {
		t.Fatalf("the provider was asked %d times and %d slots taken for an address the reference already pins", len(s.provider.calls), s.usage.takes)
	}
}

func TestChainAsksTheProviderOnlyForWorkspacesThatEnabledIt(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-on"] = enabled()
	claims := []geocoding.Claim{
		claimOf("on", "ws-on", paulista("Av. Paulista")),
		claimOf("off", "ws-off", paulista("Av. Paulista")),
	}
	got := s.chain(t).Resolve(context.Background(), claims, now.Add(time.Minute))
	on := settlementOf(t, got.Settlements, "on").Resolution
	if on.Address.Fix.Source != geo.SourceProvider || on.Address.Fix.Precision != geo.PrecisionAddress || on.Address.GeoStatus != lead.GeoLocated {
		t.Fatalf("enabled workspace = %+v, want the provider's address fix", on.Address)
	}
	off := settlementOf(t, got.Settlements, "off").Resolution
	if off.Address.Fix.Source != geo.SourceReference || off.Address.Fix.Precision != geo.PrecisionDistrict || off.Address.GeoStatus != lead.GeoApproximate {
		t.Fatalf("workspace without a provider = %+v, want the bairro point", off.Address)
	}
	if len(s.provider.calls) != 1 || s.provider.calls[0].WorkspaceID != "ws-on" {
		t.Fatalf("provider calls = %+v, want one for the enabled workspace", s.provider.calls)
	}
	if s.metrics.provider["opencage:ok"] != 1 {
		t.Fatalf("provider metrics = %+v, want one ok", s.metrics.provider)
	}
}

func TestChainNeverCallsTheProviderWithoutTakingASlot(t *testing.T) {
	s := newSetup()
	ceiling := int64(2)
	s.settings.byWorkspace["ws-1"] = geocoding.Settings{Provider: geocoding.ProviderOpenCage, MonthlyCeiling: &ceiling}
	claims := []geocoding.Claim{
		claimOf("a-1", "ws-1", paulista("Rua A")),
		claimOf("a-2", "ws-1", paulista("Rua B")),
		claimOf("a-3", "ws-1", paulista("Rua C")),
	}
	got := s.chain(t).Resolve(context.Background(), claims, now.Add(time.Minute))
	if len(s.provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1 (a ceiling of 2 gives a daily share of 1)", len(s.provider.calls))
	}
	for _, id := range []string{"a-2", "a-3"} {
		r := settlementOf(t, got.Settlements, id).Resolution
		if r.Address.GeoStatus != lead.GeoQuotaExceeded || r.NextAt == nil || r.Address.Fix == nil || r.Address.Fix.Precision != geo.PrecisionDistrict {
			t.Fatalf("%s = %+v, want quota_exceeded with the bairro point applied and a time to come back", id, r)
		}
	}
	if s.usage.takes != 2 {
		t.Fatalf("slots asked = %d, want 2: once refused, the workspace is not asked again in this batch", s.usage.takes)
	}
	if s.metrics.quotaHits["daily"] != 1 {
		t.Fatalf("quota hits = %+v, want one daily hit", s.metrics.quotaHits)
	}
}

func TestChainTreatsAZeroCeilingAsNoExternalCalls(t *testing.T) {
	s := newSetup()
	zero := int64(0)
	s.settings.byWorkspace["ws-1"] = geocoding.Settings{Provider: geocoding.ProviderOpenCage, MonthlyCeiling: &zero}
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
	if r := settlementOf(t, got.Settlements, "a-1").Resolution; r.Address.GeoStatus != lead.GeoApproximate || r.NextAt != nil {
		t.Fatalf("resolution = %+v, want the bairro point and out of the queue", r)
	}
	if len(s.provider.calls) != 0 || s.usage.takes != 0 {
		t.Fatal("a zero ceiling must not reach the provider or the usage counter")
	}
}

func TestChainFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		breakIt func(s *chainSetup)
		status  lead.GeoStatus
		calls   int
	}{
		{"an unreadable answer cache never pays for the address", func(s *chainSetup) { s.answers.readErr = errDown }, lead.GeoUnavailable, 0},
		{"an unreadable provider pause never calls the provider", func(s *chainSetup) { s.pauses.readErr = errDown }, lead.GeoUnavailable, 0},
		{"a paused provider is never called", func(s *chainSetup) {
			pause, _ := geocoding.PauseAfter(geocoding.ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), now.Add(-time.Minute))
			s.pauses.pause = &pause
		}, lead.GeoUnavailable, 0},
		{"the provider being down leaves the address waiting", func(s *chainSetup) { s.provider.err = errDown }, lead.GeoUnavailable, 1},
		{"the provider answering unavailable leaves it waiting", func(s *chainSetup) {
			s.provider.outcome = geo.Unavailable(geo.ReasonProviderDown, time.Hour)
		}, lead.GeoUnavailable, 1},
		{"unreadable settings never mean no provider", func(s *chainSetup) { s.settings.err = errDown }, lead.GeoUnavailable, 0},
		{"an unreadable usage counter never spends quota", func(s *chainSetup) { s.usage.err = errDown }, lead.GeoUnavailable, 0},
		{"a reference that is not loaded leaves every address waiting", func(s *chainSetup) { s.reference.coverage = geo.Coverage{} }, lead.GeoUnavailable, 0},
		{"a reference that cannot be read leaves every address waiting", func(s *chainSetup) { s.reference.lookupErr = errDown }, lead.GeoUnavailable, 0},
		{"unreadable coverage leaves every address waiting", func(s *chainSetup) { s.reference.coverageErr = errDown }, lead.GeoUnavailable, 0},
		{"a provider the server lost leaves the address waiting", func(s *chainSetup) {
			s.settings.byWorkspace["ws-1"] = geocoding.Settings{Provider: "opencage"}
			s.provider = nil
		}, lead.GeoUnavailable, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSetup()
			s.settings.byWorkspace["ws-1"] = enabled()
			tt.breakIt(s)
			var c *Chain
			if s.provider == nil {
				var err error
				c, err = NewChain(ChainDeps{Reference: s.reference, Settings: s.settings, Usage: s.usage, Metrics: s.metrics, Answers: s.answers, Pauses: s.pauses, Now: func() time.Time { return now }})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				c = s.chain(t)
			}
			got := c.Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(time.Minute))
			r := settlementOf(t, got.Settlements, "a-1").Resolution
			if r.Address.GeoStatus != tt.status || r.NextAt == nil {
				t.Fatalf("resolution = %+v, want %q with a retry time", r, tt.status)
			}
			if s.provider != nil && len(s.provider.calls) != tt.calls {
				t.Fatalf("provider calls = %d, want %d", len(s.provider.calls), tt.calls)
			}
			if tt.calls == 0 && s.usage.err == nil && s.usage.takes != 0 {
				t.Fatalf("usage slots taken = %d, want none when no call can happen", s.usage.takes)
			}
			if len(s.answers.remembered) != 0 {
				t.Fatalf("remembered %+v, want no failure stored as an answer", s.answers.remembered)
			}
		})
	}
}

func TestChainDefersProviderCallsPastTheDeadline(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Rua A"))}, now.Add(-time.Second))
	r := settlementOf(t, got.Settlements, "a-1").Resolution
	if r.Address.GeoStatus != lead.GeoPending || r.NextAt == nil || !r.NextAt.Equal(now) || r.Address.Fix == nil {
		t.Fatalf("resolution = %+v, want pending again with the bairro point applied", r)
	}
	if len(s.provider.calls) != 0 || s.usage.takes != 0 {
		t.Fatal("no provider call and no slot past the deadline")
	}
}

func TestChainReadsTheReferenceOnceAndSettingsOncePerBatch(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	var claims []geocoding.Claim
	for _, id := range []string{"a", "b", "c", "d"} {
		claims = append(claims, claimOf(id, "ws-1", paulista("Rua "+id)))
	}
	s.chain(t).Resolve(context.Background(), claims, now.Add(time.Minute))
	if s.reference.lookups != 1 || s.settings.reads != 1 {
		t.Fatalf("reference lookups = %d, settings reads = %d, want one each per batch", s.reference.lookups, s.settings.reads)
	}
}

func TestChainNeverPaysForAStaleClaimAndBacksItOff(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	c := claimOf("a-1", "ws-1", paulista("Av. Paulista"))
	c.Fingerprint, c.Attempts = "older", 2
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{c}, now.Add(time.Minute))
	if len(s.provider.calls) != 0 || s.usage.takes != 0 {
		t.Fatalf("provider calls = %d, usage slots = %d, want neither for a stale claim", len(s.provider.calls), s.usage.takes)
	}
	if len(got.Release) != 0 || len(got.Settlements) != 1 {
		t.Fatalf("Resolve() = %+v, want one settlement and no release", got)
	}
	r := got.Settlements[0].Resolution
	if r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil || !r.NextAt.Equal(now.Add(geocoding.Backoff(2))) {
		t.Fatalf("resolution = %+v, want unavailable with a backoff", r)
	}
}

func TestChainQueryCarriesThePostalAddress(t *testing.T) {
	s := newSetup()
	s.settings.byWorkspace["ws-1"] = enabled()
	s.chain(t).Resolve(context.Background(), []geocoding.Claim{claimOf("a-1", "ws-1", paulista("Av. Paulista"))}, now.Add(time.Minute))
	want := paulista("Av. Paulista").Normalize()
	if len(s.provider.calls) != 1 || s.provider.calls[0].Postal != want {
		t.Fatalf("query = %+v, want the normalized postal address", s.provider.calls)
	}
}

func TestChainWaitsForStatesTheReferenceDoesNotCoverYet(t *testing.T) {
	s := newSetup()
	s.reference.coverage = geo.Coverage{States: map[string]bool{"SP": true}}
	elsewhere := paulista("Rua A")
	elsewhere.City, elsewhere.State, elsewhere.ZipCode = "Contagem", "MG", "32000000"
	got := s.chain(t).Resolve(context.Background(), []geocoding.Claim{
		claimOf("sp", "ws-1", paulista("")),
		claimOf("mg", "ws-1", elsewhere),
	}, now.Add(time.Minute))
	if r := settlementOf(t, got.Settlements, "sp").Resolution; r.Address.GeoStatus != lead.GeoApproximate {
		t.Fatalf("covered address = %+v, want the bairro point", r.Address)
	}
	if r := settlementOf(t, got.Settlements, "mg").Resolution; r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil {
		t.Fatalf("uncovered address = %+v, want it waiting for the reference instead of not found", r)
	}
	if len(s.reference.looked) != 1 || s.reference.looked[0].State != "SP" {
		t.Fatalf("looked up %+v, want only the covered address", s.reference.looked)
	}
}
