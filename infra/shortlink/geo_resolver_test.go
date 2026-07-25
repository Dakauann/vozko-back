package shortlink

import (
	"testing"

	"vozko/domain/shortlink"
)

func TestEdgeGeoResolve(t *testing.T) {
	r := NewEdgeGeoResolver()
	got := r.Resolve(shortlink.GeoHints{Country: " br ", Region: " SP ", City: " Sao Paulo "})
	if got.Country != "BR" || got.Region != "SP" || got.City != "Sao Paulo" {
		t.Fatalf("resolve = %+v", got)
	}
}

func TestNormalizeCountry(t *testing.T) {
	cases := map[string]string{
		" br ": "BR",
		"xx":   "",
		"T1":   "",
		"":     "",
		"us":   "US",
	}
	for in, want := range cases {
		if got := normalizeCountry(in); got != want {
			t.Fatalf("normalizeCountry(%q) = %q want %q", in, got, want)
		}
	}
}
