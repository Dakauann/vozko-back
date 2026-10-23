package node_executors

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/conversation"
)

type interactiveSendClient struct {
	conversation.WhatsAppClient
}

func (interactiveSendClient) SendButtonMessage(context.Context, conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	return &conversation.SendTextMessageOutput{MessageID: "wamid.buttons"}, nil
}

func (interactiveSendClient) SendListMessage(context.Context, conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	return &conversation.SendTextMessageOutput{MessageID: "wamid.list"}, nil
}

func TestWorkflowButtonsAndListsAreStoredAsAutomatedMessagesWithTheirChoices(t *testing.T) {
	cases := []struct {
		name string
		send func(*whatsappSender) error
		want wantedPrompt
	}{
		{
			name: "buttons",
			send: func(s *whatsappSender) error {
				_, _, err := s.SendButtonsWithInput(context.Background(), waMediaRun(), conversation.SendButtonMessageInput{
					HeaderType: conversation.HeaderTypeText, HeaderText: "Plano", BodyText: "Qual plano?", FooterText: "Responda",
					Buttons: []conversation.InteractiveButton{
						{Type: conversation.ButtonTypeReply, ID: "basico", Title: "Básico"},
						{Type: conversation.ButtonTypeReply, ID: "pro", Title: "Pro"},
					},
				})
				return err
			},
			want: wantedPrompt{kind: conversation.InteractiveKindButtons, body: "Qual plano?", options: []string{"basico", "pro"}},
		},
		{
			name: "list",
			send: func(s *whatsappSender) error {
				_, _, err := s.SendListWithInput(context.Background(), waMediaRun(), conversation.SendListMessageInput{
					HeaderText: "Agenda", BodyText: "Escolha o horário", FooterText: "Fuso de Brasília", ButtonText: "Ver horários",
					Sections: []conversation.ListSection{
						{Title: "Manhã", Rows: []conversation.ListRow{{ID: "h9", Title: "9h"}}},
						{Title: "Tarde", Rows: []conversation.ListRow{{ID: "h14", Title: "14h", Description: "depois do almoço"}}},
					},
				})
				return err
			},
			want: wantedPrompt{kind: conversation.InteractiveKindList, body: "Escolha o horário", options: []string{"h9", "h14"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history := &recordingHistory{}
			sender := newMediaSender(interactiveSendClient{}, history, nil)

			if err := tc.send(sender); err != nil {
				t.Fatal(err)
			}

			if len(history.records) != 1 {
				t.Fatalf("records = %+v", history.records)
			}
			record := history.records[0]
			if record.MessageType != conversation.MessageTypeAIResponse || record.Text != tc.want.body {
				t.Fatalf("record = %+v", record)
			}
			got, ok := record.Message().Interactive()
			if !ok || got.Kind != tc.want.kind || !reflect.DeepEqual(got.OptionIDs(), tc.want.options) {
				t.Fatalf("interactive = %+v, %v; want %+v", got, ok, tc.want)
			}
		})
	}
}

type wantedPrompt struct {
	kind    conversation.InteractiveKind
	body    string
	options []string
}
