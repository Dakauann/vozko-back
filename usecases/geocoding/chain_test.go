package geocoding_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
)

func newChain(t *testing.T, reference *fakeReference) *Chain {
	t.Helper()
	c, err := NewChain(ChainDeps{Reference: reference, Now: func() time.Time { return now }})
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

func TestNewChainRefusesAMissingReference(t *testing.T) {
	if _, err := NewChain(ChainDeps{}); err == nil {
		t.Fatal("NewChain() without the reference must refuse")
	}
	if _, err := NewChain(ChainDeps{Reference: loadedReference()}); err != nil {
		t.Fatalf("NewChain() with the reference err = %v", err)
	}
}

func TestChainPlacesEveryAddressFromTheReference(t *testing.T) {
	tight := paulista("Av. Paulista")
	tight.ZipCode = "01310100"
	nowhere := paulista("Rua A")
	nowhere.ZipCode, nowhere.District, nowhere.City = "", "", "Cidade Desconhecida"
	got := newChain(t, loadedReference()).Resolve(context.Background(), []geocoding.Claim{
		claimOf("street", "ws-1", tight),
		claimOf("bairro", "ws-2", paulista("Rua A")),
		claimOf("nowhere", "ws-1", nowhere),
	})
	street := settlementOf(t, got.Settlements, "street").Resolution
	if street.Address.GeoStatus != lead.GeoLocated || street.Address.Fix.Precision != geo.PrecisionStreet || street.Address.Fix.Source != geo.SourceReference || street.NextAt != nil {
		t.Fatalf("street = %+v, want the street point from the reference and out of the queue", street)
	}
	bairro := settlementOf(t, got.Settlements, "bairro").Resolution
	if bairro.Address.GeoStatus != lead.GeoApproximate || bairro.Address.Fix.Precision != geo.PrecisionDistrict || bairro.NextAt != nil {
		t.Fatalf("bairro = %+v, want the bairro point and out of the queue", bairro)
	}
	missing := settlementOf(t, got.Settlements, "nowhere").Resolution
	if missing.Address.GeoStatus != lead.GeoNotFound || missing.Address.Fix != nil || missing.NextAt != nil {
		t.Fatalf("nowhere = %+v, want not found and out of the queue", missing)
	}
}

func TestChainFailsClosedWhenTheReferenceCannotBeRead(t *testing.T) {
	tests := []struct {
		name    string
		breakIt func(r *fakeReference)
	}{
		{"a reference that is not loaded", func(r *fakeReference) { r.coverage = geo.Coverage{} }},
		{"a reference lookup that fails", func(r *fakeReference) { r.lookupErr = errDown }},
		{"unreadable coverage", func(r *fakeReference) { r.coverageErr = errDown }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reference := loadedReference()
			tt.breakIt(reference)
			c := claimOf("a-1", "ws-1", paulista("Rua A"))
			got := newChain(t, reference).Resolve(context.Background(), []geocoding.Claim{c})
			r := settlementOf(t, got.Settlements, "a-1").Resolution
			if r.Address.GeoStatus != lead.GeoUnavailable || r.Address.Fix != nil || r.NextAt == nil || !r.NextAt.Equal(now.Add(geocoding.Backoff(c.Attempts))) {
				t.Fatalf("resolution = %+v, want unplaced and waiting with a backoff, never located", r)
			}
		})
	}
}

func TestChainReadsTheReferenceOncePerBatch(t *testing.T) {
	reference := loadedReference()
	var claims []geocoding.Claim
	for _, id := range []string{"a", "b", "c", "d"} {
		claims = append(claims, claimOf(id, "ws-"+id, paulista("Rua "+id)))
	}
	newChain(t, reference).Resolve(context.Background(), claims)
	if reference.lookups != 1 {
		t.Fatalf("reference lookups = %d, want one per batch", reference.lookups)
	}
}

func TestChainBacksOffAStaleClaimWithoutPlacingIt(t *testing.T) {
	c := claimOf("a-1", "ws-1", paulista("Av. Paulista"))
	c.Fingerprint, c.Attempts = "older", 2
	got := newChain(t, loadedReference()).Resolve(context.Background(), []geocoding.Claim{c})
	if len(got.Release) != 0 || len(got.Settlements) != 1 {
		t.Fatalf("Resolve() = %+v, want one settlement and no release", got)
	}
	r := got.Settlements[0].Resolution
	if r.Address.GeoStatus != lead.GeoUnavailable || r.Address.Fix != nil || r.NextAt == nil || !r.NextAt.Equal(now.Add(geocoding.Backoff(2))) {
		t.Fatalf("resolution = %+v, want unavailable with a backoff", r)
	}
}

func TestChainWaitsForStatesTheReferenceDoesNotCoverYet(t *testing.T) {
	reference := loadedReference()
	reference.coverage = geo.Coverage{States: map[string]bool{"SP": true}}
	elsewhere := paulista("Rua A")
	elsewhere.City, elsewhere.State, elsewhere.ZipCode = "Contagem", "MG", "32000000"
	got := newChain(t, reference).Resolve(context.Background(), []geocoding.Claim{
		claimOf("sp", "ws-1", paulista("")),
		claimOf("mg", "ws-1", elsewhere),
	})
	if r := settlementOf(t, got.Settlements, "sp").Resolution; r.Address.GeoStatus != lead.GeoApproximate {
		t.Fatalf("covered address = %+v, want the bairro point", r.Address)
	}
	if r := settlementOf(t, got.Settlements, "mg").Resolution; r.Address.GeoStatus != lead.GeoUnavailable || r.NextAt == nil {
		t.Fatalf("uncovered address = %+v, want it waiting for the reference instead of not found", r)
	}
	if len(reference.looked) != 1 || reference.looked[0].State != "SP" {
		t.Fatalf("looked up %+v, want only the covered address", reference.looked)
	}
}
