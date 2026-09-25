package whatsapp_campaign

import "testing"

func TestParseReportSection(t *testing.T) {
	for _, section := range ReportSections() {
		if got, ok := ParseReportSection(string(section)); !ok || got != section {
			t.Fatalf("ParseReportSection(%q) = %q, %v", section, got, ok)
		}
	}
	// The route parameter is user input; only the named sections exist, as with attendance.ParseSection.
	for _, raw := range []string{"", "everything", "Summary", "summary "} {
		if _, ok := ParseReportSection(raw); ok {
			t.Fatalf("ParseReportSection(%q) accepted", raw)
		}
	}
}
