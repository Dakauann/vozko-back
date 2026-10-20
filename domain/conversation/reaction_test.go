package conversation

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestApplyReactionKeepsOneReactionPerActorAndDirection(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	later := at.Add(time.Minute)
	ana := Reaction{Emoji: "👍", Direction: ReactionInbound, ActorID: "ana", At: at}
	bia := Reaction{Emoji: "❤️", Direction: ReactionInbound, ActorID: "bia", At: at}
	ours := Reaction{Emoji: "🙏", Direction: ReactionOutbound, ActorID: "ana", At: at}

	cases := []struct {
		name    string
		current []Reaction
		next    Reaction
		want    []Reaction
	}{
		{"a first reaction is added", nil, ana, []Reaction{ana}},
		{"the same actor reacting again replaces the emoji", []Reaction{ana}, Reaction{Emoji: "😂", Direction: ReactionInbound, ActorID: "ana", At: later},
			[]Reaction{{Emoji: "😂", Direction: ReactionInbound, ActorID: "ana", At: later}}},
		{"another actor keeps their own reaction", []Reaction{ana}, bia, []Reaction{ana, bia}},
		{"our reaction and the customer's live side by side", []Reaction{ana}, ours, []Reaction{ana, ours}},
		{"an empty emoji removes that actor's reaction", []Reaction{ana, bia}, Reaction{Direction: ReactionInbound, ActorID: "ana", At: later}, []Reaction{bia}},
		{"removing a reaction that is not there changes nothing", []Reaction{bia}, Reaction{Direction: ReactionInbound, ActorID: "ana", At: later}, []Reaction{bia}},
		{"removing the last reaction leaves none", []Reaction{ana}, Reaction{Direction: ReactionInbound, ActorID: "ana"}, []Reaction{}},
		{"the business account reacts once, so a teammate's reaction replaces ours", []Reaction{bia, ours}, Reaction{Emoji: "❤️", Direction: ReactionOutbound, ActorID: "bruno", At: later},
			[]Reaction{bia, {Emoji: "❤️", Direction: ReactionOutbound, ActorID: "bruno", At: later}}},
		{"the channel echo of our own reaction keeps who reacted", []Reaction{ours}, Reaction{Emoji: "🙏", Direction: ReactionOutbound, At: later}, []Reaction{ours}},
		{"a reaction made on the phone replaces ours", []Reaction{ours}, Reaction{Emoji: "👍", Direction: ReactionOutbound, At: later},
			[]Reaction{{Emoji: "👍", Direction: ReactionOutbound, At: later}}},
		{"removing our reaction on the phone clears it", []Reaction{bia, ours}, Reaction{Direction: ReactionOutbound, At: later}, []Reaction{bia}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := append([]Reaction(nil), c.current...)
			got := ApplyReaction(c.current, c.next)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ApplyReaction = %+v, want %+v", got, c.want)
			}
			if !reflect.DeepEqual(c.current, before) {
				t.Fatalf("the input list must not change, got %+v", c.current)
			}
		})
	}
}

func TestReactionsAreReadFromTheOneMetadataKeyAndFromLegacyMetaKeys(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	stored, _ := json.Marshal(map[string]any{
		"providerEvent":      "messages",
		ReactionsMetadataKey: []Reaction{{Emoji: "👍", Direction: ReactionInbound, ActorID: "5511", At: at}},
	})

	cases := []struct {
		name     string
		metadata string
		want     []Reaction
	}{
		{"no metadata has no reactions", "", nil},
		{"metadata without reactions has none", `{"providerEvent":"messages"}`, nil},
		{"the reactions key is the source", string(stored), []Reaction{{Emoji: "👍", Direction: ReactionInbound, ActorID: "5511", At: at}}},
		{"a legacy instagram emoji reads as a customer reaction", `{"instagram_reaction_emoji":"❤️","instagram_reaction":"love","instagram_reaction_at":"2026-10-18T12:00:00Z"}`,
			[]Reaction{{Emoji: "❤️", Direction: ReactionInbound, At: at}}},
		{"a legacy facebook reaction without emoji falls back to the reaction name", `{"facebook_reaction":"like","facebook_reaction_emoji":""}`,
			[]Reaction{{Emoji: "like", Direction: ReactionInbound}}},
		{"a legacy unreact leaves no reaction", `{"instagram_reaction_action":"unreact","instagram_reaction":"","instagram_reaction_emoji":""}`, nil},
		{"the reactions key wins over legacy keys", `{"instagram_reaction_emoji":"❤️","reactions":[]}`, []Reaction{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &Message{Metadata: json.RawMessage(c.metadata)}
			if got := m.Reactions(); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Reactions() = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestReactWritesTheReactionsKeyAndKeepsOtherMetadata(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	m := &Message{Metadata: json.RawMessage(`{"instagram_reaction_emoji":"❤️","providerEvent":"messages"}`)}

	if err := m.React(Reaction{Emoji: "👍", Direction: ReactionOutbound, ActorID: "user-1", At: at}); err != nil {
		t.Fatal(err)
	}

	want := []Reaction{{Emoji: "❤️", Direction: ReactionInbound}, {Emoji: "👍", Direction: ReactionOutbound, ActorID: "user-1", At: at}}
	if got := m.Reactions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("a legacy reaction must carry over into the new key: %+v, want %+v", got, want)
	}
	var meta map[string]any
	if err := json.Unmarshal(m.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["providerEvent"] != "messages" {
		t.Fatalf("other metadata must stay: %v", meta)
	}
}

func TestReactRefusesAnUnknownDirection(t *testing.T) {
	m := &Message{}
	if err := m.React(Reaction{Emoji: "👍", Direction: "sideways"}); err == nil {
		t.Fatal("a reaction without inbound or outbound direction must be refused")
	}
}
