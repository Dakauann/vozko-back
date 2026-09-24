package conversation_repository

import (
	"strings"
	"testing"
)

// How a conversation was last closed (who, why and with which outcome) lives
// on every channel's conversation row. Projected for official WhatsApp only,
// every other channel showed a finished conversation without its closer or its
// outcome.
func TestEveryChannelProjectsHowItWasClosed(t *testing.T) {
	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			if ch.CloseTable == "" {
				t.Fatal("CloseTable is empty: finished conversations on this channel lose their closer and outcome")
			}
			fields := ch.closeFieldsSQL()
			for _, column := range []string{"close_source", "close_reason", "close_outcome", "closed_at"} {
				if !strings.Contains(fields, ch.CloseTable+"."+column) {
					t.Errorf("does not read %s.%s:\n%s", ch.CloseTable, column, fields)
				}
				if strings.Count(fields, "AS "+column) != 1 {
					t.Errorf("expected exactly one %s alias:\n%s", column, fields)
				}
			}
			if !strings.Contains(ch.entryInfoSQL(), fields) {
				t.Error("the live update does not project the close fields the list does")
			}
		})
	}
}

func TestAChannelWithoutCloseColumnsStillProjectsThem(t *testing.T) {
	fields := channelQuery{}.closeFieldsSQL()
	for _, column := range []string{"close_source", "close_reason", "close_outcome", "closed_at"} {
		if !strings.Contains(fields, "AS "+column) {
			t.Errorf("missing %s:\n%s", column, fields)
		}
	}
}
