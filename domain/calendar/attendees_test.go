package calendar

import (
	"reflect"
	"testing"
)

func TestParseAttendeesReadsEveryShapeTheAgentToolAndTheWorkflowNodeSend(t *testing.T) {
	ana := Attendee{Email: "ana@example.com"}
	bob := Attendee{Email: "bob@example.com", Name: "Bob"}
	cases := []struct {
		name         string
		raw          interface{}
		wantAccepted []Attendee
		wantRejected []string
	}{
		{"nothing configured", nil, nil, nil},
		{"a blank text", "   ", nil, nil},
		{"one bare address", "ana@example.com", []Attendee{ana}, nil},
		{"a display name with the address", "Bob <bob@example.com>", []Attendee{bob}, nil},
		{"commas, semicolons and new lines all separate", "ana@example.com, Bob <bob@example.com>;\ncarla@example.com", []Attendee{ana, bob, {Email: "carla@example.com"}}, nil},
		{"the same address twice in any case is one guest", "ana@example.com\nANA@Example.com", []Attendee{ana}, nil},
		{"surrounding spaces are not part of the address", "  ana@example.com  ,  ", []Attendee{ana}, nil},
		{"a token that is not an address is rejected and the rest kept", "5519983401415, ana@example.com", []Attendee{ana}, []string{"5519983401415"}},
		{"a list of texts is read item by item", []interface{}{"ana@example.com", "Bob <bob@example.com>"}, []Attendee{ana, bob}, nil},
		{"a missing list item never becomes text", []interface{}{nil, "ana@example.com"}, []Attendee{ana}, nil},
		{"a list item is not split on separators", []interface{}{"ana@example.com, bob@example.com"}, nil, []string{"ana@example.com, bob@example.com"}},
		{"a typed list of strings", []string{"ana@example.com", ""}, []Attendee{ana}, nil},
		{"a value that is not text is read as text and rejected", 42, nil, []string{"42"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			accepted, rejected := ParseAttendees(c.raw)
			if len(accepted) != 0 || len(c.wantAccepted) != 0 {
				if !reflect.DeepEqual(accepted, c.wantAccepted) {
					t.Fatalf("accepted = %+v, want %+v", accepted, c.wantAccepted)
				}
			}
			if !reflect.DeepEqual(rejected, c.wantRejected) {
				t.Fatalf("rejected = %q, want %q", rejected, c.wantRejected)
			}
		})
	}
}
