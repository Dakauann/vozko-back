package shared

import (
	"reflect"
	"testing"
)

func TestSentiment_Valid(t *testing.T) {
	for _, s := range []Sentiment{SentimentPositive, SentimentNeutral, SentimentNegative} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []Sentiment{"", "Positive", "POSITIVE", "positive ", "mixed"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestSentimentValues_Order(t *testing.T) {
	want := []string{"positive", "neutral", "negative"}
	if got := SentimentValues(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SentimentValues() = %v, want %v", got, want)
	}
}
