package sqlexpr

import "testing"

func TestRealMessagePredicates(t *testing.T) {
	for name, tc := range map[string]struct {
		got  string
		want string
	}{
		"not the import seed":  {NotSeedPlaceholder("cm"), "NOT (cm.message_type = 'system' AND COALESCE(cm.metadata->>'seed', '') = 'lead_import')"},
		"not imported history": {LiveMessage("cm"), "COALESCE(cm.metadata->>'backfill', '') <> 'true'"},
		"a real message":       {RealMessage("m2"), "NOT (m2.message_type = 'system' AND COALESCE(m2.metadata->>'seed', '') = 'lead_import') AND COALESCE(m2.metadata->>'backfill', '') <> 'true'"},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestTheCallCounterpartIsTheOtherPartysNumber(t *testing.T) {
	for name, tc := range map[string]struct {
		got  string
		want string
	}{
		"with an alias":    {CallCounterpart("tcl"), "(CASE WHEN tcl.direction = 'inbound' THEN tcl.phone_from ELSE tcl.phone_to END)"},
		"without an alias": {CallCounterpart(""), "(CASE WHEN direction = 'inbound' THEN phone_from ELSE phone_to END)"},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}
