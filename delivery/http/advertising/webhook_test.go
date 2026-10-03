package advertisinghttp

import (
	"reflect"
	"testing"

	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
)

func TestAdAccountEntriesWithChangesGoToTheAdAccountTopic(t *testing.T) {
	cases := []struct {
		name string
		env  *mm.EntryEnvelope
		want []string
	}{
		{"one change", &mm.EntryEnvelope{Entry: &mm.Entry{Changes: []*mm.Change{{Field: "with_issues_ad_objects"}}}}, []string{webhook.TopicMetaAdAccount}},
		{"many changes publish once", &mm.EntryEnvelope{Entry: &mm.Entry{Changes: []*mm.Change{{Field: "in_process_ad_objects"}, {Field: "ad_recommendations"}}}}, []string{webhook.TopicMetaAdAccount}},
		{"nil change is skipped", &mm.EntryEnvelope{Entry: &mm.Entry{Changes: []*mm.Change{nil}}}, nil},
		{"no changes", &mm.EntryEnvelope{Entry: &mm.Entry{}}, nil},
		{"messaging only", &mm.EntryEnvelope{Entry: &mm.Entry{Messaging: []*mm.MessagingEvent{{}}}}, nil},
		{"no entry", &mm.EntryEnvelope{}, nil},
		{"no envelope", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeAdAccountEntry(tc.env); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
