package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
)

func TestAPinDuringASweepIsNotOverwrittenAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	created := leadWithAddress(t, repo, ws, "Ana", paulista)
	now := time.Now().UTC().Add(time.Minute)
	token := uuid.NewString()
	claims, err := q.Claim(context.Background(), claimRequest(token, now, 10, 10))
	if err != nil || len(claims) != 1 {
		t.Fatalf("Claim() = %d, %v", len(claims), err)
	}

	pin, err := geo.Pinned(geo.Point{Lat: -23.5613, Lng: -46.6565}, geo.SourceManual, now)
	if err != nil {
		t.Fatal(err)
	}
	current := reload(t, repo, ws, created.ID)
	next := *current
	if changed, err := next.PinLocation(current.Addresses[0].ID, pin); err != nil || !changed {
		t.Fatalf("PinLocation() = %v, %v", changed, err)
	}
	if err := repo.Save(context.Background(), &next, current.Version, nil); err != nil {
		t.Fatalf("a pin during the lease: %v", err)
	}

	reference := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionPostalCode, Source: geo.SourceReference, FixedAt: now}
	a := claims[0].Address
	a.Fix, a.GeoStatus = &reference, lead.GeoApproximate
	result, err := q.Settle(context.Background(), token, now, []geocoding.Settlement{{Claim: claims[0], Resolution: geocoding.Resolution{Address: a, Changed: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 || result.Stale != 1 {
		t.Fatalf("Settle() = %+v, want the pinned address refused as stale", result)
	}
	got := reload(t, repo, ws, created.ID).Addresses[0]
	if got.Fix == nil || got.Fix.Source != geo.SourceManual || got.Fix.Point != pin.Point || got.GeoStatus != lead.GeoLocated {
		t.Fatalf("address after the sweep = %+v, want the pin kept", got)
	}
}

func TestALocationAcceptedForALeadWithoutAnAddressIsStoredAgainstPostgres(t *testing.T) {
	repo, q := geocodeQueueDB(t)
	ws := uuid.NewString()
	created := newLead(t, repo, ws, lead.Draft{Name: "Bia"})
	pin, err := geo.Pinned(geo.Point{Lat: -12.9714, Lng: -38.5014}, geo.SourceLeadPin, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	current := reload(t, repo, ws, created.ID)
	next := *current
	if changed, err := next.AcceptLocation(pin); err != nil || !changed {
		t.Fatalf("AcceptLocation() = %v, %v", changed, err)
	}
	if err := repo.Save(context.Background(), &next, current.Version, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored := reload(t, repo, ws, created.ID)
	if len(stored.Addresses) != 1 || stored.Version != current.Version+1 {
		t.Fatalf("stored = %+v", stored)
	}
	got := stored.Addresses[0]
	if !got.Primary || got.Fix == nil || got.Fix.Source != geo.SourceLeadPin || got.Fix.Point != pin.Point || got.GeoStatus != lead.GeoLocated || got.Postal.City != "" {
		t.Fatalf("address = %+v", got)
	}
	claims, err := q.Claim(context.Background(), claimRequest(uuid.NewString(), time.Now().UTC().Add(time.Hour), 10, 10))
	if err != nil || len(claims) != 0 {
		t.Fatalf("a pinned address is never queued for the sweeper: %d, %v", len(claims), err)
	}
}
