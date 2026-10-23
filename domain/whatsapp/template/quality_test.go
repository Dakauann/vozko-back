package template

import "testing"

func TestRateQualityKeepsMetasValueAndSaysWhenItChanged(t *testing.T) {
	tmpl := &Template{}
	if !tmpl.RateQuality(" green ") || tmpl.Quality != "GREEN" {
		t.Fatalf("first rating, got %q", tmpl.Quality)
	}
	if tmpl.RateQuality("GREEN") {
		t.Fatal("the same rating is not a change")
	}
	if tmpl.RateQuality("") || tmpl.Quality != "GREEN" {
		t.Fatalf("an empty rating keeps the last one, got %q", tmpl.Quality)
	}
	if !tmpl.RateQuality("RED") || tmpl.Quality != "RED" {
		t.Fatalf("a new rating replaces it, got %q", tmpl.Quality)
	}
}
