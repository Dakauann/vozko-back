package actor

import "testing"

const storedUUID = "7a1c3f0e-0000-4000-8000-00000000a1a1"

func TestSplitStoresTheBareIDBesideItsKind(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		wantID string
		kind   Kind
	}{
		{name: "person", id: storedUUID, wantID: storedUUID, kind: KindHuman},
		{name: "agent", id: "ai:" + storedUUID, wantID: storedUUID, kind: KindAI},
		{name: "workflow", id: "workflow:" + storedUUID, wantID: storedUUID, kind: KindWorkflow},
		{name: "nobody", id: "", wantID: "", kind: KindHuman},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, kind := Split(tc.id)
			if id != tc.wantID || kind != tc.kind {
				t.Fatalf("Split(%q) = %q, %q, want %q, %q", tc.id, id, kind, tc.wantID, tc.kind)
			}
		})
	}
}

func TestJoinIsTheInverseOfSplit(t *testing.T) {
	for _, id := range []string{storedUUID, "ai:" + storedUUID, "workflow:" + storedUUID} {
		stored, kind := Split(id)
		if got := Join(stored, kind); got != id {
			t.Fatalf("Join(Split(%q)) = %q", id, got)
		}
	}
}

func TestJoinReadsAnUnknownKindAsAPerson(t *testing.T) {
	for _, kind := range []Kind{"", "legacy", KindHuman} {
		if got := Join(storedUUID, kind); got != storedUUID {
			t.Fatalf("Join(%q) = %q, want the bare id", kind, got)
		}
	}
}

func TestJoinOfNothingIsNothing(t *testing.T) {
	if got := Join("", KindAI); got != "" {
		t.Fatalf("Join(\"\", ai) = %q, want empty", got)
	}
}

func TestTheSystemIsStoredAsAKindWithoutAnID(t *testing.T) {
	id, kind := Split(SystemID)
	if id != "" || kind != KindSystem {
		t.Fatalf("Split(system) = %q, %q, want no id and the system kind", id, kind)
	}
	if got := Join("", KindSystem); got != SystemID {
		t.Fatalf("Join(\"\", system) = %q, want %q", got, SystemID)
	}
}
