package customfield

import "testing"

func TestFormatValueJoinedUsesTheGivenSeparator(t *testing.T) {
	cases := []struct {
		name  string
		value any
		sep   string
		want  string
	}{
		{name: "strings", value: []string{"a", "b"}, sep: ", ", want: "a, b"},
		{name: "decoded json", value: []any{"a", 2.0}, sep: ", ", want: "a, 2"},
		{name: "a scalar", value: 3.5, sep: ", ", want: "3.5"},
		{name: "the stored delimiter", value: []string{"a", "b"}, sep: MultiSelectDelimiter, want: "a|b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatValueJoined(tc.value, tc.sep); got != tc.want {
				t.Fatalf("FormatValueJoined = %q, want %q", got, tc.want)
			}
		})
	}
	if FormatValue([]string{"a", "b"}) != FormatValueJoined([]string{"a", "b"}, MultiSelectDelimiter) {
		t.Fatal("FormatValue must stay the stored-delimiter form")
	}
}
