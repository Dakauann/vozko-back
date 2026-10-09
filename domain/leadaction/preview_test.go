package leadaction

import (
	"encoding/json"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

func TestPreviewResultAddCountsSkipsByReason(t *testing.T) {
	var r PreviewResult
	r.Add(5000, Tally{Unchanged: 30, Gone: 2})
	r.Add(120, Tally{})
	if r.Selected != 5120 || r.Eligible != 5088 || r.Skipped[SkipUnchanged] != 30 || r.Skipped[SkipGone] != 2 {
		t.Fatalf("preview = %+v", r)
	}
}

func TestRequestFingerprintTellsRequestsApart(t *testing.T) {
	filter := &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	base := selection.Selection{Mode: selection.ModeAllMatching, Filter: filter, ExpectedCount: 10}
	classify := Params{Key: "k", Value: json.RawMessage(`"A"`)}
	first := RequestFingerprint(ActionClassify, classify, base)

	if again := RequestFingerprint(ActionClassify, Params{Key: " k ", Value: json.RawMessage(`"A"`)}, base); again != first {
		t.Fatal("the same request must give the same fingerprint")
	}
	other := classify
	other.Value = json.RawMessage(`"B"`)
	if RequestFingerprint(ActionClassify, other, base) == first {
		t.Fatal("another value must give another fingerprint")
	}
	picked := selection.Selection{Mode: selection.ModeIDs, IDs: []string{"b", "a"}}
	reordered := selection.Selection{Mode: selection.ModeIDs, IDs: []string{"a", "b"}}
	if RequestFingerprint(ActionClassify, classify, picked) != RequestFingerprint(ActionClassify, classify, reordered) {
		t.Fatal("the order of picked ids does not change the request")
	}
	if RequestFingerprint(ActionBlock, Params{Blocked: blocked(true)}, base) == RequestFingerprint(ActionBlock, Params{Blocked: blocked(false)}, base) {
		t.Fatal("block and unblock are different requests")
	}
}

func TestPreviewIsVisibleToItsActorOnly(t *testing.T) {
	p := Preview{ActorID: "u-1"}
	if !p.VisibleTo("u-1") || p.VisibleTo("u-2") || p.VisibleTo("") {
		t.Fatal("a preview belongs to the person who asked for it")
	}
}

func TestAClonedPreviewSharesNothingWithTheOriginal(t *testing.T) {
	remaining := int64(40)
	p := &Preview{Status: PreviewRunning, Result: PreviewResult{Selected: 5000, Skipped: map[SkipReason]int{SkipUnchanged: 3}}, Send: &campaign.SendQuote{Count: 5, CapRemaining: &remaining}}
	c := p.Clone()
	p.Status = PreviewDone
	p.Result.Add(5000, Tally{Unchanged: 7})
	p.Send.Count = 9
	*p.Send.CapRemaining = 1
	if c.Status != PreviewRunning || c.Result.Selected != 5000 || c.Result.Skipped[SkipUnchanged] != 3 || c.Send.Count != 5 || *c.Send.CapRemaining != 40 {
		t.Fatalf("the clone followed the original: %+v %+v", c, c.Send)
	}
	var none *Preview
	if none.Clone() != nil {
		t.Fatal("a nil preview clones to nil")
	}
}
