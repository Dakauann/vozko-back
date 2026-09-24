package conversation_repository

import (
	"errors"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestSearchFiltersByTheAutomationThatHoldsTheConversation(t *testing.T) {
	// "Responsável: IA" and "Responsável: Fluxo" must ask for the kind, never
	// compare an ai:/workflow: string with the uuid column (a query error).
	paths := map[string]conversation.SearchEntriesInput{
		"workspace": {WorkspaceID: "ws-1", EntryType: shared.EntryTypeWhatsApp},
		"campaign":  {CampaignID: "camp-1", EntryType: shared.EntryTypeWhatsApp},
	}
	for name, input := range paths {
		for _, kind := range []actor.Kind{actor.KindAI, actor.KindWorkflow} {
			t.Run(name+"/"+string(kind), func(t *testing.T) {
				db, mock, sqlDB := newStatusDB(t)
				defer sqlDB.Close()

				mock.ExpectQuery(`iaf\.assignee_kind = \$`).WillReturnError(errors.New("stop"))

				input.ResponsibleKind = kind
				_, _, _ = NewRepository(db).SearchEntriesWithMessages(input)

				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatalf("the query did not filter on assignee_kind: %v", err)
				}
			})
		}
	}
}
