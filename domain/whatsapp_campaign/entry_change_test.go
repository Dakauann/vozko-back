package whatsapp_campaign

import "testing"

func TestAnEntryEditChangesTheRecipientUnlessItOnlyTogglesTheAI(t *testing.T) {
	number, name, on := "5511999990000", "Ana", true
	cases := []struct {
		name  string
		input UpdateEntryInput
		want  bool
	}{
		{name: "only the AI toggle", input: UpdateEntryInput{AutomationEnabled: &on}},
		{name: "the number", input: UpdateEntryInput{Number: &number}, want: true},
		{name: "the name", input: UpdateEntryInput{Name: &name}, want: true},
		{name: "the variables", input: UpdateEntryInput{Variables: []string{"x"}}, want: true},
		{name: "the metadata", input: UpdateEntryInput{Metadata: map[string]interface{}{"k": "v"}}, want: true},
		{name: "the AI toggle with the number", input: UpdateEntryInput{AutomationEnabled: &on, Number: &number}, want: true},
	}
	for _, tc := range cases {
		if got := tc.input.ChangesTheRecipient(); got != tc.want {
			t.Fatalf("%s: ChangesTheRecipient = %v, want %v", tc.name, got, tc.want)
		}
	}
}
