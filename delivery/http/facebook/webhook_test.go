package facebook

import (
	"reflect"
	"testing"

	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
)

func TestRouteEntry(t *testing.T) {
	cases := []struct {
		name  string
		entry *mm.Entry
		want  []string
	}{
		{"messaging", &mm.Entry{Messaging: []*mm.MessagingEvent{{}}}, []string{webhook.TopicFacebookMessage}},
		{"standby", &mm.Entry{Standby: []*mm.MessagingEvent{{}}}, []string{webhook.TopicFacebookMessage}},
		{"feed and mention share one topic", &mm.Entry{Changes: []*mm.Change{{Field: "feed"}, {Field: "mention"}}}, []string{webhook.TopicFacebookFeed}},
		{"mixed", &mm.Entry{Messaging: []*mm.MessagingEvent{{}}, Changes: []*mm.Change{{Field: "feed"}, {Field: "ratings"}}},
			[]string{webhook.TopicFacebookMessage, webhook.TopicFacebookFeed, webhook.TopicFacebookPage}},
		{"nothing", &mm.Entry{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeEntry(&mm.EntryEnvelope{Entry: tc.entry}); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
