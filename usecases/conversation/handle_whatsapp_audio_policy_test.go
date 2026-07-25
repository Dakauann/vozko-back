package conversation_usecase

import (
	"context"
	"testing"

	agent "vozko/domain/agent"
	"vozko/domain/conversation"
)

type fallbackPolicyWhatsAppClient struct {
	sent []conversation.SendTextMessageInput
}

func (m *fallbackPolicyWhatsAppClient) SendTextMessage(_ context.Context, input conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	m.sent = append(m.sent, input)
	return &conversation.SendTextMessageOutput{MessageID: "fallback-msg"}, nil
}

func (m *fallbackPolicyWhatsAppClient) SendAudioMessage(context.Context, conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) SendAudioBytes(context.Context, string, []byte, string, string) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) UploadAudio(context.Context, []byte, string) (string, error) {
	return "", nil
}

func (m *fallbackPolicyWhatsAppClient) UploadImage(context.Context, []byte, string, string) (string, error) {
	return "", nil
}

func (m *fallbackPolicyWhatsAppClient) UploadMedia(context.Context, []byte, string, string) (string, error) {
	return "", nil
}

func (m *fallbackPolicyWhatsAppClient) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}

func (m *fallbackPolicyWhatsAppClient) SendImageMessage(context.Context, conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) SendVideoMessage(context.Context, conversation.SendVideoMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) SendDocumentMessage(context.Context, conversation.SendDocumentMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) SendStickerMessage(context.Context, conversation.SendStickerMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) SendButtonMessage(context.Context, conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (m *fallbackPolicyWhatsAppClient) SendListMessage(context.Context, conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (m *fallbackPolicyWhatsAppClient) SendCallPermissionRequest(context.Context, conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) SendTypingIndicator(context.Context, string) error {
	return nil
}

func (m *fallbackPolicyWhatsAppClient) MarkMessageAsRead(context.Context, string) error {
	return nil
}

func (m *fallbackPolicyWhatsAppClient) SendTemplateMessage(context.Context, conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) ListTemplates(context.Context, conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) GetTemplate(context.Context, string) (*conversation.Template, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) CreateTemplate(context.Context, conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	return nil, nil
}

func (m *fallbackPolicyWhatsAppClient) UpdateTemplate(context.Context, string, conversation.UpdateTemplateInput) error {
	return nil
}

func (m *fallbackPolicyWhatsAppClient) DeleteTemplate(context.Context, conversation.DeleteTemplateInput) error {
	return nil
}

func (m *fallbackPolicyWhatsAppClient) UploadMediaForTemplate(context.Context, conversation.UploadMediaForTemplateInput) (string, error) {
	return "", nil
}

func TestSendWhatsAppFallbackTextIfEligible_SuppressesWithoutEligibleContext(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	client := &fallbackPolicyWhatsAppClient{}

	testCases := []struct {
		name string
		ctx  *agentContext
	}{
		{name: "nil context"},
		{
			name: "skip response",
			ctx: &agentContext{
				skipResponse: true,
				agent:        &agent.Agent{ID: "agent-1", WorkspaceID: "workspace-1"},
			},
		},
		{
			name: "missing agent",
			ctx:  &agentContext{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client.sent = nil

			err := uc.sendWhatsAppFallbackTextIfEligible(
				context.Background(),
				client,
				testCase.ctx,
				"5511999999999",
				"fallback body",
				"unit-test",
			)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if len(client.sent) != 0 {
				t.Fatalf("expected no fallback message to be sent, got %d", len(client.sent))
			}
		})
	}
}

func TestSendWhatsAppFallbackTextIfEligible_SendsForEligibleAgentContext(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	client := &fallbackPolicyWhatsAppClient{}

	err := uc.sendWhatsAppFallbackTextIfEligible(
		context.Background(),
		client,
		&agentContext{agent: &agent.Agent{ID: "agent-1", WorkspaceID: "workspace-1"}},
		"5511999999999",
		"fallback body",
		"unit-test",
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(client.sent) != 1 {
		t.Fatalf("expected 1 fallback message, got %d", len(client.sent))
	}
	if client.sent[0].Body != "fallback body" {
		t.Fatalf("expected fallback body to be sent, got %q", client.sent[0].Body)
	}
}
