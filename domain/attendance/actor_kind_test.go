package attendance

import "testing"

func TestTheActorKindFollowsTheAssigneeID(t *testing.T) {
	cases := map[string]string{
		"workflow:2377d865-d097-41a3-aae1-9136de90a7a8": ActorKindWorkflow,
		"ai:cbfd6142-1d0b-43fd-9548-72ac56398d7e":       ActorKindAI,
		"f0a54bc3-9933-45d0-88d0-64ed22c911a5":          ActorKindHuman,
	}
	for id, want := range cases {
		if got := ActorKindOf(id); got != want {
			t.Fatalf("%s: got %s, want %s", id, got, want)
		}
	}
}
