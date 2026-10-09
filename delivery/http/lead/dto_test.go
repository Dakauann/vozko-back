package lead

import (
	"testing"
	"time"

	leaddomain "vozko/domain/lead"
)

func TestRowsAndRecordsCarryTheRealNameApartFromTheStoredName(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		lead     leaddomain.Lead
		wantReal string
	}{
		{"a typed name", leaddomain.Lead{ID: routeLeadID, Number: "5511987654321", Name: "Maria Souza"}, "Maria Souza"},
		{"the own number stored as the name", leaddomain.Lead{ID: routeLeadID, Number: "5511987654321", Name: "5511987654321"}, ""},
		{"no name", leaddomain.Lead{ID: routeLeadID, Number: "5511987654321"}, ""},
		{"a relative without WhatsApp", leaddomain.Lead{ID: routeLeadID, Name: "Pedro"}, "Pedro"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			record := toLeadRecord(&c.lead, now)
			if record.RealName != c.wantReal || record.Name != c.lead.Name {
				t.Fatalf("record name = %q real = %q, want stored %q real %q", record.Name, record.RealName, c.lead.Name, c.wantReal)
			}
			row := toLeadListItem(&leaddomain.LeadWithSummary{Lead: &c.lead, Summary: &leaddomain.LeadSummary{}}, now)
			if row.RealName != c.wantReal || row.Name != c.lead.Name {
				t.Fatalf("row name = %q real = %q, want stored %q real %q", row.Name, row.RealName, c.lead.Name, c.wantReal)
			}
		})
	}
}
