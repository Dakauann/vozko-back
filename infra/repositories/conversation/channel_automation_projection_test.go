package conversation_repository

import (
	"strings"
	"testing"
)

func TestEveryChannelProjectsTheAutomationOverrideOnBothPaths(t *testing.T) {
	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			if ch.AutomationColumn == "" {
				t.Error("AutomationColumn is empty: the inbox list will report this channel as always automated")
			}
			if !strings.Contains(ch.entryInfoSQL(), "AS automation_enabled") {
				t.Error("EntryInfoSQL does not project automation_enabled: " +
					"entry_update broadcasts will report this channel as always automated")
			}
		})
	}
}

func TestTheAutomationProjectionIsAliasedForScanning(t *testing.T) {
	for _, ch := range channelQueries {
		if strings.Count(ch.entryInfoSQL(), "AS automation_enabled") != 1 {
			t.Errorf("%s: expected exactly one automation_enabled alias", ch.EntryType)
		}
	}
}

func TestBothPathsReadTheSameColumn(t *testing.T) {
	for _, ch := range channelQueries {
		if !strings.Contains(ch.entryInfoSQL(), ch.AutomationColumn+" AS automation_enabled") {
			t.Errorf("%s: EntryInfoSQL does not project %q; the list and the broadcast could disagree",
				ch.EntryType, ch.AutomationColumn)
		}
	}
}
