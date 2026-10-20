package workflow

import (
	"reflect"
	"testing"
)

func TestEveryEntryKindTheReadModelProducesCanBeFiltered(t *testing.T) {
	want := []EntryPointKind{EntryMessageReceived, EntryFirstMessage, EntryWebhook, EntryFormSubmitted, EntryAgentCall,
		EntryCampaignSent, EntryStageAdded, EntryNoReply, EntryCallReceived, EntryManual, EntryLeadExit}
	for _, kind := range want {
		parsed, ok := ParseEntryPointKind(" " + string(kind) + " ")
		if !ok || parsed != kind {
			t.Fatalf("%s is not accepted as a filter", kind)
		}
	}
	if len(EntryPointKinds()) != len(want) {
		t.Fatalf("kinds = %v", EntryPointKinds())
	}
	for _, bad := range []string{"", "webhooks", "trigger_webhook", "WEBHOOK"} {
		if _, ok := ParseEntryPointKind(bad); ok {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

func TestAnEntryKindIsReachedByTheTriggersThatProduceIt(t *testing.T) {
	cases := map[EntryPointKind][]TriggerType{
		EntryWebhook:   {TriggerWebhook},
		EntryAgentCall: {TriggerAgentCall},
		EntryManual:    {TriggerManual},
		EntryLeadExit:  nil,
	}
	for kind, want := range cases {
		if got := TriggerTypesOf(kind); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: triggers = %v, want %v", kind, got, want)
		}
	}
	for _, kind := range EntryPointKinds() {
		for _, trigger := range TriggerTypesOf(kind) {
			if back, ok := EntryKindOfTrigger(trigger); !ok || back != kind {
				t.Fatalf("%s maps back to %s", trigger, back)
			}
		}
	}
}
