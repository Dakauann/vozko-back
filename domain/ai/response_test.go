package ai

import "testing"

func TestUnfenceJSONStripsTheFenceProvidersAdd(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"a json-tagged fence": {
			in:   "```json\n{\"results\":[]}\n```",
			want: `{"results":[]}`,
		},
		"an untagged fence": {
			in:   "```\n{\"results\":[]}\n```",
			want: `{"results":[]}`,
		},
		"a fence with surrounding whitespace": {
			in:   "  \n```json\n  {\"a\":1}  \n```  \n",
			want: `{"a":1}`,
		},
		"no fence at all": {
			in:   ` {"results":[]} `,
			want: `{"results":[]}`,
		},
		"empty": {in: "   ", want: ""},
		"backticks inside the body": {
			in:   "```json\n{\"text\":\"use ` here\"}\n```",
			want: "{\"text\":\"use ` here\"}",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := UnfenceJSON(tc.in); got != tc.want {
				t.Fatalf("UnfenceJSON(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
