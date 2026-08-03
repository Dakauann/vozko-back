package conversation_repository

import (
	"strings"
	"testing"
)

// Two query paths hydrate a conversation's automation override: the list path
// (GetEntriesWithMessages, via AutomationColumn) and the single-entry path
// (GetEntryLastMessage, via EntryInfoSQL). Only the list path selected it.
//
// The single-entry path is the one entry_update broadcasts are built from, so
// the column being absent there meant the override scanned as nil, nil reads as
// "inherit", and inherit renders as enabled. Pausing a conversation therefore
// took effect in the database and in the next page load, but the live broadcast
// that fired immediately after said the agent was still answering, the operator
// saw their own change reverted.
//
// Enabling looked like it worked, because the wrong answer and the right answer
// happen to coincide in that direction.

func TestEveryChannelProjectsTheAutomationOverrideOnBothPaths(t *testing.T) {
	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			if ch.AutomationColumn == "" {
				t.Error("AutomationColumn is empty: the inbox list will report this channel as always automated")
			}
			if !strings.Contains(ch.EntryInfoSQL, "AS automation_enabled") {
				t.Error("EntryInfoSQL does not project automation_enabled: " +
					"entry_update broadcasts will report this channel as always automated")
			}
		})
	}
}

// The projection has to be an alias the scan target can bind to. A bare column
// reference would scan into nothing and fail the same silent way.
func TestTheAutomationProjectionIsAliasedForScanning(t *testing.T) {
	for _, ch := range channelQueries {
		if strings.Count(ch.EntryInfoSQL, "AS automation_enabled") != 1 {
			t.Errorf("%s: expected exactly one automation_enabled alias", ch.EntryType)
		}
	}
}

// Both paths must read the same column, or the list and the live broadcast can
// disagree about the same conversation.
func TestBothPathsReadTheSameColumn(t *testing.T) {
	for _, ch := range channelQueries {
		// AutomationColumn is alias-qualified ("tgc.automation_enabled"); the
		// EntryInfoSQL projection uses the same alias.
		if !strings.Contains(ch.EntryInfoSQL, ch.AutomationColumn+" AS automation_enabled") {
			t.Errorf("%s: EntryInfoSQL does not project %q; the list and the broadcast could disagree",
				ch.EntryType, ch.AutomationColumn)
		}
	}
}
