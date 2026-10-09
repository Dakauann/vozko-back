package crmfilter

import (
	"testing"

	"vozko/domain/leadmap"
)

func TestLeadAddressSummaryCountsLeadsAndThoseWithAPrimaryAddress(t *testing.T) {
	if got, want := LeadAddressSummarySelect("la"), "COUNT(*) AS total, COUNT(la.lead_id) AS with_address"; got != want {
		t.Fatalf("LeadAddressSummarySelect() = %q, want %q", got, want)
	}
	want := "LEFT JOIN lead_addresses la ON la.lead_id = leads.id AND la.is_primary AND la.workspace_id = leads.workspace_id"
	if got := LeadPrimaryAddressJoin("la"); got != want {
		t.Fatalf("LeadPrimaryAddressJoin() = %q, want %q", got, want)
	}
}

func TestLeadAddressSummaryRowDerivesTheLeadsWithoutAnAddress(t *testing.T) {
	row := LeadAddressSummaryRow{Total: 10, WithAddress: 6, OnMap: 3, Approximate: 2, NotFound: 1, QuotaExceeded: 4, Refused: 5, Pending: 7}
	want := leadmap.Summary{Total: 10, OnMap: 3, Approximate: 2, WithoutAddress: 4, NotFound: 1, QuotaExceeded: 4, Refused: 5, Pending: 7}
	if got := row.Summary(); got != want {
		t.Fatalf("Summary() = %+v, want %+v", got, want)
	}
}
