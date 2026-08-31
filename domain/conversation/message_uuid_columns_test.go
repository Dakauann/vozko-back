package conversation

import "testing"

// The uuid-backed pointer fields must never survive Normalize as a blank string.
//
// media_id and read_by are uuid columns. Postgres rejects "" with 22P02 and
// rejects the entire INSERT, so a campaign send that already reached WhatsApp
// disappears from the transcript: the lead holds a message we have no row for.
// The pointers exist so absence is expressible; callers land on &"" whenever
// they take the address of a blank source (an automation send with no operator,
// a media send with no attachment), which is why the collapse lives here rather
// than at each writer.
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
			// Both fields are checked together: they are the same column type
			// with the same failure, and fixing one alone was the original bug.
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

// Normalize must not mutate the caller's string through the pointer it was
// handed. Trimming in place would silently edit whatever else holds that
// address — the media id the caller is about to log, or reuse for a retry.
func TestNormalizeDoesNotMutateCallerString(t *testing.T) {
	original := "  9ca4ffe2-f7b6-4e88-8691-8bc27c697c20  "
	held := original

	m := &Message{ID: "id", EntryID: "entry", MediaID: &held}
	m.Normalize()

	if held != original {
		t.Fatalf("caller's string was mutated: %q, want %q", held, original)
	}
}
