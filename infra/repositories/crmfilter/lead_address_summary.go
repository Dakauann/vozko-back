package crmfilter

import "vozko/domain/leadmap"

func LeadAddressSummarySelect(alias string) string {
	return "COUNT(*) AS total, COUNT(" + alias + ".lead_id) AS with_address"
}

func LeadPrimaryAddressJoin(alias string) string {
	return "LEFT JOIN lead_addresses " + alias + " ON " + alias + ".lead_id = leads.id AND " + alias + ".is_primary AND " +
		alias + ".workspace_id = leads.workspace_id"
}

type LeadAddressSummaryRow struct {
	Total         int64
	WithAddress   int64
	OnMap         int64
	Approximate   int64
	NotFound      int64
	QuotaExceeded int64
	Refused       int64
	Pending       int64
}

func (r LeadAddressSummaryRow) Summary() leadmap.Summary {
	return leadmap.Summary{
		Total:          r.Total,
		OnMap:          r.OnMap,
		Approximate:    r.Approximate,
		WithoutAddress: r.Total - r.WithAddress,
		NotFound:       r.NotFound,
		Pending:        r.Pending,
		QuotaExceeded:  r.QuotaExceeded,
		Refused:        r.Refused,
	}
}
