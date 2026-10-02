package advertising

import "testing"

func TestCustomerFileNeedsAnIdentifyingColumn(t *testing.T) {
	d := CustomerListDraft{AdAccountID: "a", Name: "Clientes", Source: SourceFile, FileMediaID: "m", Columns: []MatchKey{MatchFirstName, MatchCity}}
	requireIssues(t, d.Validate(), FieldIssue{"columns", "needs_email_or_phone"})
	d.Columns = append(d.Columns, MatchPhone)
	if err := d.Validate(); err != nil {
		t.Fatalf("file refused: %v", err)
	}
	crm := CustomerListDraft{AdAccountID: "a", Name: "Clientes do CRM", Source: SourceCRM}
	if err := crm.Validate(); err != nil {
		t.Fatalf("crm source refused: %v", err)
	}
}

func TestLookalikeRules(t *testing.T) {
	d := LookalikeDraft{AdAccountID: "a", Name: "Parecidos", OriginAudienceID: "123", Percent: 1}
	if err := d.Validate(); err != nil || d.Ratio() != 0.01 {
		t.Fatalf("lookalike refused: %v ratio %v", err, d.Ratio())
	}
	d.Percent = 11
	requireIssues(t, d.Validate(), FieldIssue{"percent", "invalid"})
}

func TestSavedAudienceValidatesItsTargeting(t *testing.T) {
	s := SavedAudience{Name: "Mulheres SP"}
	s.Normalize()
	requireIssues(t, s.Validate(), FieldIssue{"targeting.locations", "required"})
}

func TestBatchesSplitAtMetasLimit(t *testing.T) {
	rows := make([]int, 25_001)
	if got := Batches(rows, CustomerBatchSize()); len(got) != 3 || len(got[2]) != 5_001 {
		t.Fatalf("batches %d last %d", len(got), len(got[len(got)-1]))
	}
}
