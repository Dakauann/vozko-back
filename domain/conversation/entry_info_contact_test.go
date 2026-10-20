package conversation

import "testing"

func TestContactLabelPrefersTheNameThenTheNumber(t *testing.T) {
	cases := []struct {
		name string
		info EntryInfo
		want string
	}{
		{"name wins", EntryInfo{LeadName: " Ana Souza ", LeadNumber: "5511999990000"}, "Ana Souza"},
		{"number when the name is blank", EntryInfo{LeadName: "  ", LeadNumber: " 5511999990000 "}, "5511999990000"},
		{"a neutral phrase when nothing is known", EntryInfo{}, UnknownContactLabel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.info.ContactLabel(); got != c.want {
				t.Fatalf("ContactLabel() = %q, want %q", got, c.want)
			}
		})
	}
}
