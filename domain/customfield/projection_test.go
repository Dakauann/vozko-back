package customfield

import "testing"

func TestProjectForMarksWhatTheViewerMayRead(t *testing.T) {
	plain := &Definition{ID: "a", Key: "bairro"}
	sensitive := &Definition{ID: "b", Key: "classificacao", Sensitive: true}
	cases := []struct {
		name   string
		viewer Viewer
		want   []bool
	}{
		{"without the sensitive permission", Viewer{}, []bool{true, false}},
		{"with the sensitive permission", Viewer{ReadsSensitive: true}, []bool{true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProjectFor([]*Definition{plain, sensitive}, tc.viewer)
			if len(got) != len(tc.want) {
				t.Fatalf("ProjectFor() returned %d definitions, want %d", len(got), len(tc.want))
			}
			for i, projected := range got {
				if projected.Readable != tc.want[i] {
					t.Errorf("definition %s readable = %v, want %v", projected.Key, projected.Readable, tc.want[i])
				}
			}
			if got[1].ID != "b" || got[1].LegalBasis != sensitive.LegalBasis {
				t.Errorf("the projection must keep the definition, got %+v", got[1])
			}
		})
	}
}

func TestProjectForSkipsMissingDefinitions(t *testing.T) {
	if got := ProjectFor([]*Definition{nil, {ID: "a"}}, Viewer{}); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("ProjectFor() = %+v", got)
	}
}
