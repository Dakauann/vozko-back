package conversation

import "testing"

func TestNormalizeCollapsesBlankUUIDPointers(t *testing.T) {
	blank := ""
	spaces := "   "
	real := "9ca4ffe2-f7b6-4e88-8691-8bc27c697c20"
	padded := "  " + real + "  "

	cases := []struct {
		name    string
		in      *string
		wantNil bool
		want    string
	}{
		{"empty", &blank, true, ""},
		{"whitespace only", &spaces, true, ""},
		{"nil stays nil", nil, true, ""},
		{"real id survives", &real, false, real},
		{"padded id is trimmed, not dropped", &padded, false, real},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Message{ID: "id", EntryID: "entry", MediaID: tc.in, ReadBy: tc.in}
			m.Normalize()

			for field, got := range map[string]*string{"MediaID": m.MediaID, "ReadBy": m.ReadBy} {
				if tc.wantNil {
					if got != nil {
						t.Fatalf("%s = %q, want nil (a uuid column cannot take it)", field, *got)
					}
					continue
				}
				if got == nil {
					t.Fatalf("%s = nil, want %q — a real id was dropped", field, tc.want)
				}
				if *got != tc.want {
					t.Fatalf("%s = %q, want %q", field, *got, tc.want)
				}
			}
		})
	}
}

func TestNormalizeDoesNotMutateCallerString(t *testing.T) {
	original := "  9ca4ffe2-f7b6-4e88-8691-8bc27c697c20  "
	held := original

	m := &Message{ID: "id", EntryID: "entry", MediaID: &held}
	m.Normalize()

	if held != original {
		t.Fatalf("caller's string was mutated: %q, want %q", held, original)
	}
}
