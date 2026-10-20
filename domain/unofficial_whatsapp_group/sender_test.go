package unofficial_whatsapp_group

import (
	"reflect"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func seatOf(instanceID string, announce, admin bool) *uw.Group {
	return &uw.Group{InstanceID: instanceID, JID: "120363000000000001@g.us", Announce: announce, WeAreAdmin: admin}
}

func TestEligibleSendersExplainWhyNoNumberCanPost(t *testing.T) {
	cases := map[string]struct {
		presences []Presence
		want      []string
		block     SenderBlock
	}{
		"no number in the group": {
			presences: nil, want: nil, block: SenderNotInGroup,
		},
		"only admins post and no number is admin": {
			presences: []Presence{{Seat: seatOf("n1", true, false), Connected: true}},
			want:      nil, block: SenderCannotPost,
		},
		"the number that may post is offline": {
			presences: []Presence{
				{Seat: seatOf("n1", true, false), Connected: true},
				{Seat: seatOf("n2", true, true), Connected: false},
			},
			want: nil, block: SenderOffline,
		},
		"an admin posts where only admins post": {
			presences: []Presence{
				{Seat: seatOf("n1", true, false), Connected: true},
				{Seat: seatOf("n2", true, true), Connected: true},
			},
			want: []string{"n2"}, block: SenderReady,
		},
		"every connected member posts in an open group": {
			presences: []Presence{
				{Seat: seatOf("n1", false, false), Connected: true},
				{Seat: seatOf("n2", false, false), Connected: true},
				{Seat: seatOf("n3", false, false), Connected: false},
			},
			want: []string{"n1", "n2"}, block: SenderReady,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, block := EligibleSenders(tc.presences)
			if block != tc.block {
				t.Fatalf("block = %q, want %q", block, tc.block)
			}
			if ids := instanceIDsOf(got); !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("senders = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestChooseSenderPrefersTheLeastLoadedNumber(t *testing.T) {
	presences := []Presence{
		{Seat: seatOf("n1", false, false), Connected: true},
		{Seat: seatOf("n2", false, false), Connected: true},
		{Seat: seatOf("n3", false, false), Connected: true},
	}
	load := map[string]int64{"n1": 40, "n2": 3, "n3": 3}

	ranked, block := ChooseSender(presences, func(id string) int64 { return load[id] })

	if block != SenderReady {
		t.Fatalf("block = %q, want ready", block)
	}
	if !reflect.DeepEqual(ranked, []string{"n2", "n3", "n1"}) {
		t.Fatalf("ranked = %v, want the least loaded first and ties by id", ranked)
	}
}

func TestChooseSenderKeepsTheBlockWhenNobodyCanPost(t *testing.T) {
	ranked, block := ChooseSender([]Presence{{Seat: seatOf("n1", true, false), Connected: true}}, func(string) int64 { return 0 })
	if len(ranked) != 0 || block != SenderCannotPost {
		t.Fatalf("ranked = %v block = %q, want nobody and cannot post", ranked, block)
	}
}

func TestSenderBlockPermanence(t *testing.T) {
	if SenderOffline.Permanent() || SenderReady.Permanent() {
		t.Fatalf("an offline number or a ready group reads as a permanent block")
	}
	if !SenderNotInGroup.Permanent() || !SenderCannotPost.Permanent() {
		t.Fatalf("a group without our number or without posting rights reads as temporary")
	}
}

func instanceIDsOf(presences []Presence) []string {
	if len(presences) == 0 {
		return nil
	}
	out := make([]string, 0, len(presences))
	for _, p := range presences {
		out = append(out, p.InstanceID())
	}
	return out
}
