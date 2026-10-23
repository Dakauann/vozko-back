package emaildesign

import "testing"

func TestApplyUTM(t *testing.T) {
	full := UTM{Source: "email", Medium: "newsletter", Campaign: "black friday", Content: "hero"}
	cases := []struct {
		name string
		href string
		utm  UTM
		want string
	}{
		{name: "no query", href: "https://example.com/p", utm: full, want: "https://example.com/p?utm_source=email&utm_medium=newsletter&utm_campaign=black+friday&utm_content=hero"},
		{name: "existing query", href: "https://example.com/p?a=1", utm: UTM{Source: "email"}, want: "https://example.com/p?a=1&utm_source=email"},
		{name: "keeps existing utm", href: "https://example.com/p?utm_source=insta&b=2", utm: UTM{Source: "email", Medium: "news"}, want: "https://example.com/p?utm_source=insta&b=2&utm_medium=news"},
		{name: "keeps existing utm any case", href: "https://example.com/p?UTM_SOURCE=insta", utm: UTM{Source: "email"}, want: "https://example.com/p?UTM_SOURCE=insta"},
		{name: "fragment", href: "https://example.com/p#top", utm: UTM{Source: "email"}, want: "https://example.com/p?utm_source=email#top"},
		{name: "query and fragment", href: "https://example.com/p?a=1#top", utm: UTM{Source: "email"}, want: "https://example.com/p?a=1&utm_source=email#top"},
		{name: "trailing question mark", href: "https://example.com/p?", utm: UTM{Source: "email"}, want: "https://example.com/p?utm_source=email"},
		{name: "http", href: "http://example.com", utm: UTM{Medium: "m"}, want: "http://example.com?utm_medium=m"},
		{name: "uppercase scheme", href: "HTTPS://example.com/", utm: UTM{Source: "s"}, want: "HTTPS://example.com/?utm_source=s"},
		{name: "mailto untouched", href: "mailto:a@example.com", utm: full, want: "mailto:a@example.com"},
		{name: "tel untouched", href: "tel:+5511999999999", utm: full, want: "tel:+5511999999999"},
		{name: "relative untouched", href: "/p", utm: full, want: "/p"},
		{name: "empty utm", href: "https://example.com/p", utm: UTM{}, want: "https://example.com/p"},
		{name: "values escaped", href: "https://example.com", utm: UTM{Campaign: "a&b=c"}, want: "https://example.com?utm_campaign=a%26b%3Dc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyUTM(tc.href, tc.utm); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
