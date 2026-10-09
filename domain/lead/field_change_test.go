package lead

import (
	"reflect"
	"testing"

	"vozko/domain/recordevent"
)

func TestFieldChangeDiffsOneFieldAndRedactsWhatIsNotRecorded(t *testing.T) {
	cases := []struct {
		name          string
		before, after any
		recorded      bool
		want          []recordevent.Change
	}{
		{"set", nil, "alto", true, []recordevent.Change{{Field: "f", After: "alto"}}},
		{"replaced", "baixo", "alto", true, []recordevent.Change{{Field: "f", Before: "baixo", After: "alto"}}},
		{"cleared", "baixo", nil, true, []recordevent.Change{{Field: "f", Before: "baixo"}}},
		{"unchanged", "alto", "alto", true, nil},
		{"not recorded", "baixo", "alto", false, []recordevent.Change{{Field: "f", Redacted: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FieldChange(EventUpdated, "u", "f", tc.before, tc.after, tc.recorded)
			want := recordevent.Event{Actor: "u", Kind: EventUpdated, Changes: tc.want}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("event = %#v, want %#v", got, want)
			}
		})
	}
}
