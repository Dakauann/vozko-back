package aiusage

import "testing"

func TestAModelCallHasTokensAndAGenerationDoesNot(t *testing.T) {
	if !(Record{Tokens: Tokens{Input: 10}}).IsModelCall() {
		t.Fatal("a call with input tokens is a model call")
	}
	if (Record{Tokens: Tokens{CacheRead: 10}}).IsModelCall() {
		t.Fatal("cache reads are part of the input count, never a call on their own")
	}
}

func TestUsageCoversTheChargesOnlyWhenEveryChargeHasItsRow(t *testing.T) {
	totals := Totals{Billed: 3}
	if !totals.Covers(3) {
		t.Fatal("three billed rows cover three charges")
	}
	if totals.Covers(4) || totals.Covers(2) {
		t.Fatal("any difference means the token count is not exact")
	}
}
