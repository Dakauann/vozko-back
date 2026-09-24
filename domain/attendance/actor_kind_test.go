package attendance

import "testing"

func TestCountsAsAutomation(t *testing.T) {
	// First-response time splits samples into automation and people. A
	// workflow's reply averaged into the human FRT would flatter or sink the
	// team for work no person did.
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
