package whatsapp_campaign

import "testing"

func TestChangesWhatIsSent(t *testing.T) {
	current := &Campaign{TemplateID: "tmpl-1", BusinessPhoneID: "phone-1"}
	cases := []struct {
		name  string
		input *Campaign
		want  bool
	}{
		{name: "nothing about the send", input: &Campaign{Name: "x"}},
		{name: "the same template and number", input: &Campaign{TemplateID: "tmpl-1", BusinessPhoneID: "phone-1"}},
		{name: "another template", input: &Campaign{TemplateID: "tmpl-2"}, want: true},
		{name: "another number", input: &Campaign{BusinessPhoneID: "phone-2"}, want: true},
	}
	for _, tc := range cases {
		if got := current.ChangesWhatIsSent(tc.input); got != tc.want {
			t.Fatalf("%s: ChangesWhatIsSent = %v, want %v", tc.name, got, tc.want)
		}
	}
}
