package ai

import "testing"

// Every caller that asks a model for strict-schema JSON has to survive the
// markdown fence some providers still wrap it in. Tolerated rather than
// refused, because the alternative is throwing away an answer that has already
// been paid for.

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
		// A fenced block whose body itself contains backticks must not lose
		// them: only the outermost fence is the provider's.
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
