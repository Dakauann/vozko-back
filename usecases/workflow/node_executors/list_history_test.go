package node_executors

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/conversation"
)

func TestAWorkflowListIsStoredWithItsButtonTextAndRowDescriptions(t *testing.T) {
	history := &recordingHistory{}
	sender := newMediaSender(interactiveSendClient{}, history, nil)
	input := conversation.SendListMessageInput{
		BodyText: "Escolha o horário", ButtonText: "Ver horários",
		Sections: []conversation.ListSection{{Title: "Tarde", Rows: []conversation.ListRow{{ID: "h14", Title: "14h", Description: "depois do almoço"}}}},
	}

	if _, _, err := sender.SendListWithInput(context.Background(), waMediaRun(), input); err != nil {
		t.Fatal(err)
	}

	if len(history.records) != 1 {
		t.Fatalf("records = %+v", history.records)
	}
	stored := &conversation.Message{Metadata: history.records[0].Metadata}
	list, ok := stored.List()
	if !ok || !reflect.DeepEqual(list, conversation.ListPayloadOf(input)) {
		t.Fatalf("list = %+v, %v", list, ok)
	}
	if !stored.HasButtons() {
		t.Fatal("the flattened choices the resend rule reads must stay")
	}
}
