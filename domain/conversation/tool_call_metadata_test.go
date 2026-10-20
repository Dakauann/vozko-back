package conversation

import "testing"

func TestAToolRowNamesItsToolCallOnlyWhenItHasOne(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"call_1", `{"tool_call_id":"call_1"}`},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := string(ToolCallMetadata(tc.id)); got != tc.want {
			t.Fatalf("ToolCallMetadata(%q) = %s, want %s", tc.id, got, tc.want)
		}
	}
}
