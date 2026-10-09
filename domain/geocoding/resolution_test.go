package geocoding

import (
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/lead"
)

var at = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func refFix(precision geo.Precision) geo.Fix {
	return geo.Fix{Point: geo.Point{Lat: -23.55, Lng: -46.63}, Precision: precision, Source: geo.SourceReference, FixedAt: at}
}

func providerFix(precision geo.Precision) geo.Fix {
	return geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.64}, Precision: precision, Source: geo.SourceProvider, Provider: string(ProviderOpenCage), FixedAt: at}
}

func pendingAddress(street string) lead.Address {
	return lead.Address{
		ID: "a-1", Label: lead.AddressHome, Primary: true, GeoStatus: lead.GeoPending,
		Postal: address.Postal{ZipCode: "01310100", Street: street, Number: "100", City: "São Paulo", State: "SP"},
	}
}

func TestWantsProvider(t *testing.T) {
	manual := geo.Fix{Point: geo.Point{Lat: -23.5, Lng: -46.6}, Precision: geo.PrecisionCity, Source: geo.SourceManual}
	tests := []struct {
		name       string
		current    *geo.Fix
		candidates []geo.Fix
		street     string
		want       SkipReason
	}{
		{"a district point with a street asks the provider", nil, []geo.Fix{refFix(geo.PrecisionDistrict)}, "Av. Paulista", ""},
		{"nothing found with a street asks the provider", nil, nil, "Av. Paulista", ""},
		{"a street point is precise enough", nil, []geo.Fix{refFix(geo.PrecisionStreet), refFix(geo.PrecisionCity)}, "Av. Paulista", SkipPreciseEnough},
		{"without a street the provider cannot do better", nil, []geo.Fix{refFix(geo.PrecisionDistrict)}, " ", SkipNoStreet},
		{"a manual pin is never second-guessed", &manual, []geo.Fix{refFix(geo.PrecisionDistrict)}, "Av. Paulista", SkipConfirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := pendingAddress(tt.street)
			a.Fix = tt.current
			if got := WantsProvider(a, tt.candidates); got != tt.want {
				t.Fatalf("WantsProvider() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	minute := time.Minute
	tests := []struct {
		name       string
		attempt    Attempt
		wantStatus lead.GeoStatus
		wantFix    *geo.Precision
		wantSource geo.FixSource
		wantNext   *time.Duration
		changed    bool
	}{
		{
			name:       "a street point from the reference locates the address",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionStreet), refFix(geo.PrecisionCity)}, Provider: Skipped(SkipPreciseEnough)},
			wantStatus: lead.GeoLocated, wantFix: ptr(geo.PrecisionStreet), wantSource: geo.SourceReference, changed: true,
		},
		{
			name:       "a bairro point without a provider is approximate and leaves the queue",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}, Provider: Skipped(SkipProviderOff)},
			wantStatus: lead.GeoApproximate, wantFix: ptr(geo.PrecisionDistrict), wantSource: geo.SourceReference, changed: true,
		},
		{
			name:       "the provider's better answer wins",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}, Provider: Answered(geo.Located(providerFix(geo.PrecisionAddress)))},
			wantStatus: lead.GeoLocated, wantFix: ptr(geo.PrecisionAddress), wantSource: geo.SourceProvider, changed: true,
		},
		{
			name:       "a provider that finds nothing keeps the reference point",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionCity)}, Provider: Answered(geo.NotFound())},
			wantStatus: lead.GeoApproximate, wantFix: ptr(geo.PrecisionCity), wantSource: geo.SourceReference, changed: true,
		},
		{
			name:       "nothing anywhere is not found",
			attempt:    Attempt{Provider: Answered(geo.NotFound())},
			wantStatus: lead.GeoNotFound, changed: true,
		},
		{
			name:       "nothing in the reference and no provider is not found",
			attempt:    Attempt{Provider: Skipped(SkipProviderOff)},
			wantStatus: lead.GeoNotFound, changed: true,
		},
		{
			name:       "an ambiguous answer with nothing else is ambiguous",
			attempt:    Attempt{Provider: Answered(geo.Ambiguous())},
			wantStatus: lead.GeoAmbiguous, changed: true,
		},
		{
			name:       "a provider that is down leaves the address waiting, never not found",
			attempt:    Attempt{Attempts: 1, Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}, Provider: Answered(geo.Unavailable(geo.ReasonProviderDown, 0))},
			wantStatus: lead.GeoUnavailable, wantFix: ptr(geo.PrecisionDistrict), wantSource: geo.SourceReference, wantNext: &minute, changed: true,
		},
		{
			name:       "the provider's own wait is honoured when longer than the backoff",
			attempt:    Attempt{Attempts: 1, Provider: Answered(geo.Unavailable(geo.ReasonRateLimited, 10*time.Minute))},
			wantStatus: lead.GeoUnavailable, wantNext: ptr(10 * time.Minute), changed: true,
		},
		{
			name:       "a refused query text keeps the reference point and leaves the queue as refused",
			attempt:    Attempt{Attempts: 4, Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}, Provider: Answered(geo.Unavailable(geo.ReasonQueryRefused, 0))},
			wantStatus: lead.GeoRefused, wantFix: ptr(geo.PrecisionDistrict), wantSource: geo.SourceReference, changed: true,
		},
		{
			name:       "a refused query text with nothing else is refused, never not found",
			attempt:    Attempt{Attempts: 1, Provider: Answered(geo.Unavailable(geo.ReasonQueryRefused, 0))},
			wantStatus: lead.GeoRefused, changed: true,
		},
		{
			name:       "a refused account leaves the address waiting until the pause ends",
			attempt:    Attempt{Attempts: 1, Candidates: []geo.Fix{refFix(geo.PrecisionCity)}, Provider: Answered(geo.Unavailable(geo.ReasonKeyRejected, AccountPause))},
			wantStatus: lead.GeoUnavailable, wantFix: ptr(geo.PrecisionCity), wantSource: geo.SourceReference, wantNext: ptr(AccountPause), changed: true,
		},
		{
			name:       "a paused provider leaves the address waiting, never located",
			attempt:    Attempt{Attempts: 1, Provider: Answered(geo.Unavailable(geo.ReasonProviderPaused, 20*time.Minute))},
			wantStatus: lead.GeoUnavailable, wantNext: ptr(20 * time.Minute), changed: true,
		},
		{
			name:       "the reference being down leaves the address waiting",
			attempt:    Attempt{Attempts: 3, ReferenceDown: geo.ReasonReferenceNotLoaded, Provider: Skipped(SkipReferenceDown)},
			wantStatus: lead.GeoUnavailable, wantNext: ptr(16 * time.Minute), changed: true,
		},
		{
			name:       "a spent quota waits for the next cycle with the reference point applied",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}, Provider: QuotaReached(at.Add(72 * time.Hour))},
			wantStatus: lead.GeoQuotaExceeded, wantFix: ptr(geo.PrecisionDistrict), wantSource: geo.SourceReference, wantNext: ptr(72 * time.Hour), changed: true,
		},
		{
			name:       "an address left for the next sweep stays pending",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionCity)}, Provider: Deferred()},
			wantStatus: lead.GeoPending, wantFix: ptr(geo.PrecisionCity), wantSource: geo.SourceReference, wantNext: ptr(time.Duration(0)), changed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.attempt.Address = pendingAddress("Av. Paulista")
			tt.attempt.Fingerprint = tt.attempt.Address.Fingerprint()
			tt.attempt.Now = at
			got, err := Resolve(tt.attempt)
			if err != nil {
				t.Fatalf("Resolve() err = %v", err)
			}
			if got.Address.GeoStatus != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Address.GeoStatus, tt.wantStatus)
			}
			if tt.wantFix == nil && got.Address.Fix != nil {
				t.Fatalf("fix = %+v, want none", got.Address.Fix)
			}
			if tt.wantFix != nil && (got.Address.Fix == nil || got.Address.Fix.Precision != *tt.wantFix || got.Address.Fix.Source != tt.wantSource) {
				t.Fatalf("fix = %+v, want %q from %q", got.Address.Fix, *tt.wantFix, tt.wantSource)
			}
			if tt.wantNext == nil && got.NextAt != nil {
				t.Fatalf("next = %v, want out of the queue", got.NextAt)
			}
			if tt.wantNext != nil && (got.NextAt == nil || !got.NextAt.Equal(at.Add(*tt.wantNext))) {
				t.Fatalf("next = %v, want %v", got.NextAt, at.Add(*tt.wantNext))
			}
			if got.Changed != tt.changed {
				t.Fatalf("changed = %v, want %v", got.Changed, tt.changed)
			}
		})
	}
}

func TestResolveKeepsAManualPin(t *testing.T) {
	a := pendingAddress("Av. Paulista")
	manual := geo.Fix{Point: geo.Point{Lat: -23.5, Lng: -46.6}, Precision: geo.PrecisionDistrict, Source: geo.SourceManual}
	a.Fix, a.GeoStatus = &manual, lead.GeoApproximate
	got, err := Resolve(Attempt{Address: a, Fingerprint: a.Fingerprint(), Now: at,
		Candidates: []geo.Fix{refFix(geo.PrecisionStreet)}, Provider: Skipped(SkipConfirmed)})
	if err != nil {
		t.Fatalf("Resolve() err = %v", err)
	}
	if got.Address.Fix.Source != geo.SourceManual || got.Address.GeoStatus != lead.GeoApproximate || got.Changed {
		t.Fatalf("Resolve() = %+v, want the manual pin kept and nothing changed", got)
	}
}

func TestResolveBacksOffAStaleFingerprintWithoutTouchingThePosition(t *testing.T) {
	a := pendingAddress("Av. Paulista")
	reference := refFix(geo.PrecisionDistrict)
	a.Fix, a.GeoStatus = &reference, lead.GeoPending
	got, err := Resolve(Attempt{Address: a, Fingerprint: "older", Attempts: 3, Now: at, Candidates: []geo.Fix{refFix(geo.PrecisionStreet)}, Provider: Skipped(SkipStale)})
	if err != nil {
		t.Fatalf("Resolve() err = %v, want a settlement that backs off", err)
	}
	if got.Address.GeoStatus != lead.GeoUnavailable || got.NextAt == nil || !got.NextAt.Equal(at.Add(Backoff(3))) {
		t.Fatalf("Resolve() = %+v, want unavailable until %v", got, at.Add(Backoff(3)))
	}
	if !geo.SameFix(got.Address.Fix, &reference) || !got.Changed {
		t.Fatalf("Resolve() = %+v, want the stored position kept and the status change written", got)
	}
}

func TestStaleClaim(t *testing.T) {
	a := pendingAddress("Av. Paulista")
	if StaleClaim(a, a.Fingerprint()) || !StaleClaim(a, "older") {
		t.Fatal("a claim is stale exactly when its stored fingerprint differs from its text")
	}
}

func TestResolveOutcomeLabel(t *testing.T) {
	got, _ := Resolve(Attempt{Address: pendingAddress(""), Fingerprint: pendingAddress("").Fingerprint(), Now: at,
		Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}, Provider: Skipped(SkipNoStreet)})
	if got.Outcome() != "district" {
		t.Fatalf("Outcome() = %q, want the precision", got.Outcome())
	}
	none, _ := Resolve(Attempt{Address: pendingAddress(""), Fingerprint: pendingAddress("").Fingerprint(), Now: at, Provider: Skipped(SkipNoStreet)})
	if none.Outcome() != "not_found" {
		t.Fatalf("Outcome() = %q, want the status when there is no fix", none.Outcome())
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{0, time.Minute},
		{1, time.Minute},
		{2, 4 * time.Minute},
		{3, 16 * time.Minute},
		{5, 256 * time.Minute},
		{6, MaxBackoff},
		{60, MaxBackoff},
	}
	for _, tt := range tests {
		if got := Backoff(tt.attempts); got != tt.want {
			t.Fatalf("Backoff(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
