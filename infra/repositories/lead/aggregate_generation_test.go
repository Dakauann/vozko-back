package lead

import "testing"

func TestAggregateGenerations_ReadTheGenerationEveryLeadWriteBumps(t *testing.T) {
	state := newFakeState()
	generations := NewAggregateGenerations(state)

	before, err := generations.Version("ws-1")
	if err != nil || before != "0" {
		t.Fatalf("Version() = %q, %v; want 0 before any write", before, err)
	}
	newAggregateCache(state).bump("ws-1")
	after, err := generations.Version("ws-1")
	if err != nil || after == before {
		t.Fatalf("Version() = %q, %v; a lead write must move the generation", after, err)
	}
	if err := generations.Bump("ws-1"); err != nil {
		t.Fatalf("Bump() error = %v", err)
	}
	if again, _ := generations.Version("ws-1"); again == after {
		t.Fatal("Bump() must move the same generation the repository writes")
	}

	state.fail = true
	if _, err := generations.Version("ws-1"); err == nil {
		t.Fatal("an unreadable generation must be reported, never read as zero")
	}
}
