package calllist

import (
	"reflect"
	"testing"
	"time"
)

func TestTheItemStampedWithACallTellsThatCallsOutcome(t *testing.T) {
	callback := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		disposition string
		callbackAt  *time.Time
		want        CallOutcome
		shown       bool
	}{
		{"a closed item names the outcome the operator chose", "interessado", nil, CallOutcome{Disposition: "interessado"}, true},
		{"a callback carries when to call again", DispositionCallback, &callback, CallOutcome{Disposition: DispositionCallback, CallbackAt: &callback}, true},
		{"a callback without its time still names the callback", DispositionCallback, nil, CallOutcome{Disposition: DispositionCallback}, true},
		{"a refusal is the list's verdict on the lead, not the call's outcome", DispositionRefused, nil, CallOutcome{}, false},
		{"a call the operator has not closed yet", "", nil, CallOutcome{}, false},
		{"a blank outcome", "  ", nil, CallOutcome{}, false},
		{"a recheck time without a callback is not shown", "interessado", &callback, CallOutcome{Disposition: "interessado"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, shown := StampedCallOutcome(tc.disposition, "", tc.callbackAt)
			if shown != tc.shown || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("StampedCallOutcome = %+v, %v; want %+v, %v", got, shown, tc.want, tc.shown)
			}
		})
	}
}

func TestTheCallbackTimeOfAnOutcomeIsACopy(t *testing.T) {
	callback := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	got, _ := StampedCallOutcome(DispositionCallback, "", &callback)
	callback = callback.Add(time.Hour)
	if got.CallbackAt == nil || got.CallbackAt.Equal(callback) {
		t.Fatalf("the outcome shares the caller's time: %+v", got)
	}
}

func TestTheRecheckTimeOfARefusedCallbackIsNotTheCallbackTime(t *testing.T) {
	recheck := time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)
	got, shown := StampedCallOutcome(DispositionCallback, "unknown_purpose", &recheck)
	if !shown || !reflect.DeepEqual(got, CallOutcome{Disposition: DispositionCallback}) {
		t.Fatalf("StampedCallOutcome = %+v, %v; want the callback without the list's recheck time", got, shown)
	}
}
