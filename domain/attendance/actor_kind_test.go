package attendance

import "testing"

func TestCountsAsAutomation(t *testing.T) {
	cases := map[string]bool{
		ActorKindAI:       true,
		ActorKindWorkflow: true,
		ActorKindHuman:    false,
		ActorKindSystem:   false,
		"":                false,
	}
	for kind, want := range cases {
		if got := CountsAsAutomation(kind); got != want {
			t.Errorf("CountsAsAutomation(%q) = %v, want %v", kind, got, want)
		}
	}
}
