package unofficial_whatsapp_campaign

import "testing"

func TestChangesWhatIsSent(t *testing.T) {
	current := &Campaign{InstanceID: "inst-1", Message: MessageSpec{Kind: KindText, Bodies: []string{"Olá {{1}}"}}}
	cases := []struct {
		name  string
		input *Campaign
		want  bool
	}{
		{name: "the same message written loosely", input: &Campaign{Message: MessageSpec{Kind: " TEXT ", Bodies: []string{" Olá {{1}} ", ""}}}},
		{name: "the same message on the same number", input: &Campaign{InstanceID: "inst-1", Message: MessageSpec{Kind: KindText, Bodies: []string{"Olá {{1}}"}}}},
		{name: "another text", input: &Campaign{Message: MessageSpec{Kind: KindText, Bodies: []string{"Oi"}}}, want: true},
		{name: "another number", input: &Campaign{InstanceID: "inst-2", Message: current.Message}, want: true},
	}
	for _, tc := range cases {
		if got := current.ChangesWhatIsSent(tc.input); got != tc.want {
			t.Fatalf("%s: ChangesWhatIsSent = %v, want %v", tc.name, got, tc.want)
		}
	}
}
