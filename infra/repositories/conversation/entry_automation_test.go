package conversation_repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func aiProfileColumns() []string {
	return []string{"lead_id", "business_phone_id", "campaign_id", "campaign_name",
		"agent_id", "workflow_id", "agent_responses_enabled", "workflow_enabled",
		"automation_enabled", "conversation_status"}
}

func TestEntryAIProfile_ReadsEveryChannelThroughItsEntryInfoQuery(t *testing.T) {
	// The roulette decides on every channel, so every channel must be readable;
	// a channel missing here would silently hand its conversations to humans.
	for _, ch := range channelQueries {
		t.Run(string(ch.EntryType), func(t *testing.T) {
			db, mock, sqlDB := newStatusDB(t)
			defer sqlDB.Close()

			mock.ExpectQuery(`AS automation_enabled`).
				WithArgs("entry-1").
				WillReturnRows(sqlmock.NewRows(aiProfileColumns()).
					AddRow("", "", "", "", "agent-1", "", true, false, nil, ""))

			got, err := NewEntryAutomationReader(db).EntryAutomation("entry-1", string(ch.EntryType))
			if err != nil {
				t.Fatal(err)
			}
			want := conversation.AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true}
			if got.AgentID != want.AgentID || got.AgentResponsesEnabled != want.AgentResponsesEnabled ||
				got.WorkflowEnabled || got.AutomationEnabled != nil {
				t.Fatalf("profile = %+v, want %+v", got, want)
			}
		})
	}
}

func TestEntryAIProfile_CarriesThePauseSwitch(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`AS automation_enabled`).
		WillReturnRows(sqlmock.NewRows(aiProfileColumns()).
			AddRow("", "", "", "", "agent-1", "wf-1", true, true, false, ""))

	got, err := NewEntryAutomationReader(db).EntryAutomation("entry-1", string(shared.EntryTypeTelegram))
	if err != nil {
		t.Fatal(err)
	}
	if got.AutomationEnabled == nil || *got.AutomationEnabled {
		t.Fatalf("AutomationEnabled = %v, want an explicit false", got.AutomationEnabled)
	}
	if got.WorkflowID != "wf-1" || !got.WorkflowEnabled {
		t.Fatalf("workflow fields lost: %+v", got)
	}
}

func TestEntryAIProfile_FailsInsteadOfReturningAZeroProfile(t *testing.T) {
	// A zero profile reads as "no AI" and routes the conversation to a human,
	// exposing it. Every way the read can fail must surface as an error.
	t.Run("query error", func(t *testing.T) {
		db, mock, sqlDB := newStatusDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`AS automation_enabled`).WillReturnError(errors.New("connection reset"))

		if _, err := NewEntryAutomationReader(db).EntryAutomation("entry-1", "whatsapp"); err == nil {
			t.Fatal("a failed query must be an error")
		}
	})

	t.Run("no such conversation", func(t *testing.T) {
		db, mock, sqlDB := newStatusDB(t)
		defer sqlDB.Close()
		mock.ExpectQuery(`AS automation_enabled`).WillReturnRows(sqlmock.NewRows(aiProfileColumns()))

		_, err := NewEntryAutomationReader(db).EntryAutomation("entry-1", "whatsapp")
		if !errors.Is(err, conversation.ErrConversationNotFound) {
			t.Fatalf("err = %v, want ErrConversationNotFound", err)
		}
	})

	t.Run("unknown channel", func(t *testing.T) {
		db, _, sqlDB := newStatusDB(t)
		defer sqlDB.Close()

		_, err := NewEntryAutomationReader(db).EntryAutomation("entry-1", "fax")
		if !errors.Is(err, conversation.ErrEntryTypeInvalid) {
			t.Fatalf("err = %v, want ErrEntryTypeInvalid", err)
		}
	})
}

func TestEntryAccountID_ReadsTheChannelAccountFromTheSameQuery(t *testing.T) {
	// The roulette pointer is keyed by the channel account (business phone on
	// WhatsApp); a conversation nobody was assigned to has none on record.
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`AS business_phone_id`).
		WithArgs("entry-1").
		WillReturnRows(sqlmock.NewRows(aiProfileColumns()).
			AddRow("", "account-7", "", "", "", "", false, false, nil, ""))

	got, err := NewEntryAutomationReader(db).EntryAccountID("entry-1", string(shared.EntryTypeTelegram))
	if err != nil {
		t.Fatal(err)
	}
	if got != "account-7" {
		t.Fatalf("account = %q, want account-7", got)
	}
}

func TestEntryAccountID_FailsForAnUnknownConversation(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`AS business_phone_id`).WillReturnRows(sqlmock.NewRows(aiProfileColumns()))

	_, err := NewEntryAutomationReader(db).EntryAccountID("entry-1", "whatsapp")
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("err = %v, want ErrConversationNotFound", err)
	}
}
