package lead

import "testing"

func rowsOf(numbers ...string) []ImportRow {
	rows := make([]ImportRow, 0, len(numbers))
	for i, number := range numbers {
		rows = append(rows, ImportRow{Line: i + 1, Number: number})
	}
	return rows
}

func TestPrepareImportNormalizesLocalNumbers(t *testing.T) {
	prepared := PrepareImport(rowsOf("11987654321", "(11) 98765-4322", "+55 11 98765-4323"))

	if len(prepared.Rejected) != 0 {
		t.Fatalf("rejected = %+v, want none", prepared.Rejected)
	}
	want := []string{"5511987654321", "5511987654322", "5511987654323"}
	if len(prepared.Inputs) != len(want) {
		t.Fatalf("inputs = %d, want %d", len(prepared.Inputs), len(want))
	}
	for i, number := range want {
		if prepared.Inputs[i].Number != number {
			t.Errorf("input[%d] = %q, want %q", i, prepared.Inputs[i].Number, number)
		}
	}
}

func TestPrepareImportRejectsUnreachableNumbers(t *testing.T) {
	prepared := PrepareImport([]ImportRow{
		{Line: 1, Number: "5511987654321"},
		{Line: 2, Number: "not a phone"},
		{Line: 3, Number: "123"},
		{Line: 4, Number: ""},
	})

	if len(prepared.Inputs) != 1 {
		t.Fatalf("inputs = %d, want 1", len(prepared.Inputs))
	}
	if len(prepared.Rejected) != 3 {
		t.Fatalf("rejected = %d, want 3", len(prepared.Rejected))
	}
	for _, rejection := range prepared.Rejected {
		if rejection.Reason != ReasonInvalid {
			t.Errorf("line %d reason = %q, want invalid", rejection.Line, rejection.Reason)
		}
	}
	if prepared.Rejected[0].Line != 2 {
		t.Errorf("first rejection line = %d, want 2", prepared.Rejected[0].Line)
	}
}

func TestPrepareImportDedupesWithinFile(t *testing.T) {
	prepared := PrepareImport(rowsOf("5511987654321", "11987654321", "5511987654322"))

	if len(prepared.Inputs) != 2 {
		t.Fatalf("inputs = %d, want 2", len(prepared.Inputs))
	}
	if len(prepared.Rejected) != 1 {
		t.Fatalf("rejected = %d, want 1", len(prepared.Rejected))
	}
	if prepared.Rejected[0].Reason != ReasonDuplicate {
		t.Errorf("reason = %q, want duplicate", prepared.Rejected[0].Reason)
	}
	if prepared.Rejected[0].Line != 2 {
		t.Errorf("line = %d, want 2 (the repeat, not the first sighting)", prepared.Rejected[0].Line)
	}
}

func TestPrepareImportCollapsesNinthDigitVariants(t *testing.T) {
	prepared := PrepareImport(rowsOf("551187654321", "5511987654321"))

	if len(prepared.Inputs) != 1 {
		t.Fatalf("inputs = %d, want 1 (same person, two spellings)", len(prepared.Inputs))
	}
	if len(prepared.Rejected) != 1 || prepared.Rejected[0].Reason != ReasonDuplicate {
		t.Fatalf("rejected = %+v, want one duplicate", prepared.Rejected)
	}
}

func TestPrepareImportKeepsContactWhenNameIsUnusable(t *testing.T) {
	long := make([]byte, MaxLeadNameLength+10)
	for i := range long {
		long[i] = 'a'
	}
	prepared := PrepareImport([]ImportRow{
		{Line: 1, Number: "5511987654321", Name: string(long)},
	})

	if len(prepared.Inputs) != 1 {
		t.Fatalf("inputs = %d, want 1: the number is what makes the lead reachable", len(prepared.Inputs))
	}
	if prepared.Inputs[0].Name != "" {
		t.Errorf("name = %q, want empty", prepared.Inputs[0].Name)
	}
}

func TestPrepareImportNormalizesNameWhitespace(t *testing.T) {
	prepared := PrepareImport([]ImportRow{
		{Line: 1, Number: "5511987654321", Name: "  Ana   Maria  "},
	})

	if got := prepared.Inputs[0].Name; got != "Ana Maria" {
		t.Errorf("name = %q, want %q", got, "Ana Maria")
	}
}

func TestPrepareImportDropsImpossibleAges(t *testing.T) {
	negative, zero, absurd, real := -5, 0, 900, 34
	prepared := PrepareImport([]ImportRow{
		{Line: 1, Number: "5511987654321", Age: &negative},
		{Line: 2, Number: "5511987654322", Age: &zero},
		{Line: 3, Number: "5511987654323", Age: &absurd},
		{Line: 4, Number: "5511987654324", Age: &real},
	})

	if len(prepared.Inputs) != 4 {
		t.Fatalf("inputs = %d, want 4: a bad age never costs the contact", len(prepared.Inputs))
	}
	for i := 0; i < 3; i++ {
		if prepared.Inputs[i].Age != nil {
			t.Errorf("input[%d].Age = %v, want nil", i, *prepared.Inputs[i].Age)
		}
	}
	if prepared.Inputs[3].Age == nil || *prepared.Inputs[3].Age != 34 {
		t.Errorf("input[3].Age = %v, want 34", prepared.Inputs[3].Age)
	}
}

func TestPrepareImportMapsNumbersBackToLines(t *testing.T) {
	prepared := PrepareImport([]ImportRow{
		{Line: 7, Number: "11987654321"},
	})

	if got := prepared.LineByNumber["5511987654321"]; got != 7 {
		t.Errorf("line for normalized number = %d, want 7", got)
	}
}

func TestParseExistingPolicy(t *testing.T) {
	cases := []struct {
		in     string
		want   ExistingPolicy
		wantOK bool
	}{
		{"", PolicyFillEmpty, true},
		{"fill_empty", PolicyFillEmpty, true},
		{"skip", PolicySkip, true},
		{" skip ", PolicySkip, true},
		{"overwrite", "", false},
	}
	for _, c := range cases {
		got, ok := ParseExistingPolicy(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("ParseExistingPolicy(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
