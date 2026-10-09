package unofficial_whatsapp_campaign

import "testing"

func TestAnEntryEditChangesTheRecipient(t *testing.T) {
	number, name := "5511999990000", "Ana"
	cases := []struct {
		name  string
		input UpdateEntryInput
		want  bool
	}{
		{name: "nothing", input: UpdateEntryInput{}},
		{name: "the number", input: UpdateEntryInput{Number: &number}, want: true},
		{name: "the name", input: UpdateEntryInput{Name: &name}, want: true},
		{name: "the variables", input: UpdateEntryInput{Variables: []string{"x"}}, want: true},
		{name: "the metadata", input: UpdateEntryInput{Metadata: map[string]interface{}{"k": "v"}}, want: true},
	}
	for _, tc := range cases {
		if got := tc.input.ChangesTheRecipient(); got != tc.want {
			t.Fatalf("%s: ChangesTheRecipient = %v, want %v", tc.name, got, tc.want)
		}
	}
}
