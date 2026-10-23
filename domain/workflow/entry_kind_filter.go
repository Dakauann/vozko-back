package workflow

import (
	"sort"
	"strings"
)

func EntryPointKinds() []EntryPointKind {
	return []EntryPointKind{EntryMessageReceived, EntryFirstMessage, EntryWebhook, EntryFormSubmitted, EntryAgentCall,
		EntryStageAdded, EntryCallReceived, EntryLeadExit}
}

func ParseEntryPointKind(raw string) (EntryPointKind, bool) {
	candidate := EntryPointKind(strings.TrimSpace(raw))
	for _, kind := range EntryPointKinds() {
		if kind == candidate {
			return kind, true
		}
	}
	return "", false
}

func TriggerTypesOf(kind EntryPointKind) []TriggerType {
	var triggers []TriggerType
	for trigger, produced := range entryKindOfTrigger {
		if produced == kind {
			triggers = append(triggers, trigger)
		}
	}
	sort.Slice(triggers, func(i, j int) bool { return triggers[i] < triggers[j] })
	return triggers
}
