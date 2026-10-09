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

func pendingAddress(street string) lead.Address {
	return lead.Address{
		ID: "a-1", Label: lead.AddressHome, Primary: true, GeoStatus: lead.GeoPending,
		Postal: address.Postal{ZipCode: "01310100", Street: street, Number: "100", City: "São Paulo", State: "SP"},
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name       string
		attempt    Attempt
		wantStatus lead.GeoStatus
		wantFix    *geo.Precision
		wantNext   *time.Duration
		changed    bool
	}{
		{
			name:       "a street point from the reference locates the address",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionStreet), refFix(geo.PrecisionCity)}},
			wantStatus: lead.GeoLocated, wantFix: ptr(geo.PrecisionStreet), changed: true,
		},
		{
			name:       "a bairro point is approximate and leaves the queue",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}},
			wantStatus: lead.GeoApproximate, wantFix: ptr(geo.PrecisionDistrict), changed: true,
		},
		{
			name:       "a city point is approximate",
			attempt:    Attempt{Candidates: []geo.Fix{refFix(geo.PrecisionCity)}},
			wantStatus: lead.GeoApproximate, wantFix: ptr(geo.PrecisionCity), changed: true,
		},
		{
			name:       "nothing in the reference is not found and leaves the queue",
			attempt:    Attempt{},
			wantStatus: lead.GeoNotFound, changed: true,
		},
		{
			name:       "a reference that is not loaded leaves the address waiting with a backoff",
			attempt:    Attempt{Attempts: 3, ReferenceDown: geo.ReasonReferenceNotLoaded},
			wantStatus: lead.GeoUnavailable, wantNext: ptr(16 * time.Minute), changed: true,
		},
		{
			name:       "an unreadable reference never places the address, whatever it was handed",
			attempt:    Attempt{Attempts: 1, ReferenceDown: geo.ReasonReferenceDown, Candidates: []geo.Fix{refFix(geo.PrecisionStreet)}},
			wantStatus: lead.GeoUnavailable, wantNext: ptr(time.Minute), changed: true,
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
			if tt.wantFix != nil && (got.Address.Fix == nil || got.Address.Fix.Precision != *tt.wantFix || got.Address.Fix.Source != geo.SourceReference) {
				t.Fatalf("fix = %+v, want %q from the reference", got.Address.Fix, *tt.wantFix)
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
	got, err := Resolve(Attempt{Address: a, Fingerprint: a.Fingerprint(), Now: at, Candidates: []geo.Fix{refFix(geo.PrecisionStreet)}})
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
	got, err := Resolve(Attempt{Address: a, Fingerprint: "older", Attempts: 3, Now: at, Candidates: []geo.Fix{refFix(geo.PrecisionStreet)}})
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
		Candidates: []geo.Fix{refFix(geo.PrecisionDistrict)}})
	if got.Outcome() != "district" {
		t.Fatalf("Outcome() = %q, want the precision", got.Outcome())
	}
	none, _ := Resolve(Attempt{Address: pendingAddress(""), Fingerprint: pendingAddress("").Fingerprint(), Now: at})
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
