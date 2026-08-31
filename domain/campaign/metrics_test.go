package campaign

import (
	"encoding/json"
	"testing"
)

// The two channels' declared vocabularies, restated here so this package can
// assert the derivation holds for both without importing either of them (which
// would be a domain package depending on a domain package for a test).
var (
	officialSet = StatusSet{
		All: []SendStatus{
			SendStatusPending, SendStatusSent, SendStatusDelivered,
			SendStatusRead, SendStatusFailed, SendStatusNotEligiblePossibleSpam,
		},
		NonDispatch: []SendStatus{
			SendStatusPending, SendStatusFailed, SendStatusNotEligiblePossibleSpam,
		},
	}
	unofficialSet = StatusSet{
		All: []SendStatus{
			SendStatusPending, SendStatusSent, SendStatusDelivered,
			SendStatusRead, SendStatusFailed, SendStatusNotEligiblePossibleSpam,
			SendStatusSkippedNotOnWhatsApp,
		},
		NonDispatch: []SendStatus{
			SendStatusPending, SendStatusFailed,
			SendStatusNotEligiblePossibleSpam, SendStatusSkippedNotOnWhatsApp,
		},
	}
)

// Dispatched must be exactly All minus NonDispatch. This is the property the
// subtractive definition exists to guarantee, and the reason a status added to
// All cannot go missing from an export.
func TestDispatchedIsAllMinusNonDispatch(t *testing.T) {
	for name, set := range map[string]StatusSet{"official": officialSet, "unofficial": unofficialSet} {
		excluded := map[SendStatus]bool{}
		for _, s := range set.NonDispatch {
			excluded[s] = true
		}
		want := []SendStatus{}
		for _, s := range set.All {
			if !excluded[s] {
				want = append(want, s)
			}
		}
		got := set.Dispatched()
		if len(got) != len(want) {
			t.Fatalf("%s: Dispatched() = %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: Dispatched()[%d] = %q, want %q", name, i, got[i], want[i])
			}
		}
	}
}

// Both channels today happen to reduce to SENT+DELIVERED+READ. Pinning that
// makes the subtractive form's equivalence explicit rather than assumed.
func TestDispatchedIsTheDeliveryLifecycleToday(t *testing.T) {
	want := []SendStatus{SendStatusSent, SendStatusDelivered, SendStatusRead}
	for name, set := range map[string]StatusSet{"official": officialSet, "unofficial": unofficialSet} {
		got := set.Dispatched()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", name, got, want)
			}
		}
	}
}

// A status a channel does not declare must not validate against it: the
// official channel can never produce SKIPPED_NOT_ON_WHATSAPP, because it cannot
// check a number before sending.
func TestStatusSetRejectsForeignStatuses(t *testing.T) {
	if officialSet.Valid(SendStatusSkippedNotOnWhatsApp) {
		t.Fatal("official channel accepted SKIPPED_NOT_ON_WHATSAPP")
	}
	if !unofficialSet.Valid(SendStatusSkippedNotOnWhatsApp) {
		t.Fatal("unofficial channel rejected its own status")
	}
}

func TestProcessedAndDispatches(t *testing.T) {
	c := Counts{
		Total: 100, Pending: 10, Sent: 20, Delivered: 30, Read: 25,
		Failed: 8, NotEligiblePossibleSpam: 5, SkippedNotOnWhatsApp: 2,
	}
	if got, want := c.Processed(), int64(90); got != want {
		t.Errorf("Processed() = %d, want %d", got, want)
	}
	// 100 - 10 pending - 8 failed - 5 spam - 2 not-on-whatsapp
	if got, want := c.Dispatches(), int64(75); got != want {
		t.Errorf("Dispatches() = %d, want %d", got, want)
	}
}

// Counts arrive from a SQL aggregate and can disagree with Total for a moment
// while a campaign is being written. A negative dispatch count rendered as
// "-3 enviadas" is worse than a stale zero.
func TestDispatchesNeverGoesNegative(t *testing.T) {
	c := Counts{Total: 1, Pending: 5, Failed: 5}
	if got := c.Dispatches(); got != 0 {
		t.Fatalf("Dispatches() = %d, want 0", got)
	}
}

func TestNewMetricsHandlesNilAndEmpty(t *testing.T) {
	if m := NewMetrics(nil); m == nil {
		t.Fatal("NewMetrics(nil) returned nil; every caller renders this")
	}
	m := NewMetrics(&Counts{})
	if m.CompletionRate != 0 || m.SuccessRate != 0 {
		t.Fatalf("empty campaign produced rates %v/%v, want 0/0", m.CompletionRate, m.SuccessRate)
	}
}

func TestNewMetricsRates(t *testing.T) {
	m := NewMetrics(&Counts{Total: 10, Sent: 2, Delivered: 3, Read: 1, Failed: 2, Pending: 2})
	if got, want := m.Processed, int64(8); got != want {
		t.Fatalf("Processed = %d, want %d", got, want)
	}
	if got, want := m.CompletionRate, 80.0; got != want {
		t.Fatalf("CompletionRate = %v, want %v", got, want)
	}
	if got, want := m.SuccessRate, 75.0; got != want {
		t.Fatalf("SuccessRate = %v, want %v", got, want)
	}
}

// The official channel's JSON must not gain a field. The frontend has been
// reading this shape since before this package existed, and an unexpected
// skippedNotOnWhatsApp: 0 would read as "we checked and none were dead".
func TestOfficialJSONIsUnchanged(t *testing.T) {
	for _, v := range []any{NewMetrics(&Counts{Total: 3, Sent: 3}), Counts{Total: 3, Sent: 3}} {
		blob, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(blob, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, present := decoded["skippedNotOnWhatsApp"]; present {
			t.Fatalf("%T leaked skippedNotOnWhatsApp into the official payload: %s", v, blob)
		}
	}
}

func TestUnofficialJSONCarriesTheSkipBucket(t *testing.T) {
	blob, _ := json.Marshal(NewMetrics(&Counts{Total: 3, Sent: 2, SkippedNotOnWhatsApp: 1}))
	var decoded map[string]any
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["skippedNotOnWhatsApp"] != float64(1) {
		t.Fatalf("skippedNotOnWhatsApp missing or wrong: %s", blob)
	}
}
