package template_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
)

type syncTemplatesClientMock struct {
	outputs []*conversation.ListTemplatesOutput
	err     error
	inputs  []conversation.ListTemplatesInput
	index   int
}

func (m *syncTemplatesClientMock) SendTextMessage(context.Context, conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendAudioMessage(context.Context, conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendAudioBytes(context.Context, string, []byte, string, string) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) UploadAudio(context.Context, []byte, string) (string, error) {
	return "", nil
}

func (m *syncTemplatesClientMock) UploadImage(context.Context, []byte, string, string) (string, error) {
	return "", nil
}

func (m *syncTemplatesClientMock) UploadMedia(context.Context, []byte, string, string) (string, error) {
	return "", nil
}

func (m *syncTemplatesClientMock) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}

func (m *syncTemplatesClientMock) SendImageMessage(context.Context, conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendVideoMessage(context.Context, conversation.SendVideoMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendDocumentMessage(context.Context, conversation.SendDocumentMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendStickerMessage(context.Context, conversation.SendStickerMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendButtonMessage(context.Context, conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendListMessage(context.Context, conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendFlowMessage(context.Context, conversation.SendFlowMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) SendTypingIndicator(context.Context, string) error {
	return nil
}

func (m *syncTemplatesClientMock) MarkMessageAsRead(context.Context, string) error {
	return nil
}

func (m *syncTemplatesClientMock) SendTemplateMessage(context.Context, conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) ListTemplates(_ context.Context, input conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	m.inputs = append(m.inputs, input)
	if m.err != nil {
		return nil, m.err
	}
	if m.index >= len(m.outputs) {
		return nil, fmt.Errorf("unexpected ListTemplates call %d", m.index+1)
	}
	output := m.outputs[m.index]
	m.index++
	return output, nil
}

func (m *syncTemplatesClientMock) GetTemplate(context.Context, string) (*conversation.Template, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) CreateTemplate(context.Context, conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	return nil, nil
}

func (m *syncTemplatesClientMock) EditTemplate(context.Context, string, conversation.EditTemplateInput) error {
	return nil
}

func (m *syncTemplatesClientMock) DeleteTemplate(context.Context, conversation.DeleteTemplateInput) error {
	return nil
}

func (m *syncTemplatesClientMock) UploadMediaForTemplate(context.Context, conversation.UploadMediaForTemplateInput) (string, error) {
	return "", nil
}

type syncTemplatesFactoryMock struct {
	client    conversation.WhatsAppClient
	wabaID    string
	clientErr error
	wabaErr   error
}

func (m *syncTemplatesFactoryMock) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	if m.clientErr != nil {
		return nil, m.clientErr
	}
	return m.client, nil
}

func (m *syncTemplatesFactoryMock) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	if m.clientErr != nil {
		return nil, m.clientErr
	}
	return m.client, nil
}

func (m *syncTemplatesFactoryMock) WABAIdForPhone(string) (string, error) {
	if m.wabaErr != nil {
		return "", m.wabaErr
	}
	return m.wabaID, nil
}

func TestSyncTemplates_PaginatesAndNormalizesCategoryAndStatus(t *testing.T) {
	repo := newMockTemplateRepo()
	client := &syncTemplatesClientMock{
		outputs: []*conversation.ListTemplatesOutput{
			{
				Templates: []conversation.Template{{
					ID:       "ext-1",
					Name:     "welcome_template",
					Language: "pt_BR",
					Category: "utility",
					Status:   "approved",
				}},
				HasMore:   true,
				NextAfter: "cursor-1",
			},
			{
				Templates: []conversation.Template{{
					ID:       "ext-2",
					Name:     "otp_template",
					Language: "en_US",
					Category: "authentication",
					Status:   "paused",
				}},
				HasMore: false,
			},
		},
	}
	factory := &syncTemplatesFactoryMock{client: client, wabaID: "waba-1"}
	uc := NewSyncTemplatesUseCase(repo, factory, templateQualityHistory{}, &fakeQualityRecorder{})

	synced, err := uc.Execute(template.SyncTemplatesInput{BusinessPhoneID: "phone-1", PageSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(synced) != 2 {
		t.Fatalf("expected 2 synced templates, got %d", len(synced))
	}
	if len(client.inputs) != 2 {
		t.Fatalf("expected 2 ListTemplates calls, got %d", len(client.inputs))
	}
	if client.inputs[0].Limit != 1 || client.inputs[0].After != "" {
		t.Fatalf("unexpected first ListTemplates input: %+v", client.inputs[0])
	}
	if client.inputs[1].Limit != 1 || client.inputs[1].After != "cursor-1" {
		t.Fatalf("unexpected second ListTemplates input: %+v", client.inputs[1])
	}

	first, err := repo.FindByExternalIDAndWABA("ext-1", "waba-1")
	if err != nil {
		t.Fatalf("expected first template in repo: %v", err)
	}
	if first.Category != template.TemplateCategoryUtility {
		t.Fatalf("expected first category UTILITY, got %s", first.Category)
	}
	if first.Status != template.TemplateStatusApproved {
		t.Fatalf("expected first status APPROVED, got %s", first.Status)
	}

	second, err := repo.FindByExternalIDAndWABA("ext-2", "waba-1")
	if err != nil {
		t.Fatalf("expected second template in repo: %v", err)
	}
	if second.Category != template.TemplateCategoryAuthentication {
		t.Fatalf("expected second category AUTHENTICATION, got %s", second.Category)
	}
	if second.Status != template.TemplateStatusPaused {
		t.Fatalf("expected second status PAUSED, got %s", second.Status)
	}
}

func (m *syncTemplatesClientMock) SendCallPermissionRequest(context.Context, conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	return &conversation.SendTextMessageOutput{}, nil
}

type templateQualityHistory map[string]string

func (h templateQualityHistory) LatestValues(quality.Subject, quality.Signal, []string) (map[string]string, error) {
	return h, nil
}

func TestSyncTemplates_RecordsTheQualityOnlyWhenItIsNewOrMoved(t *testing.T) {
	rated := func(id, score string) conversation.Template {
		return conversation.Template{ID: id, Name: id, Language: "pt_BR", Category: "utility", Status: "approved", QualityScore: score, QualityScoreAt: 1760000000}
	}
	client := &syncTemplatesClientMock{outputs: []*conversation.ListTemplatesOutput{{Templates: []conversation.Template{
		rated("seen-green", "GREEN"), rated("turned-yellow", "YELLOW"), rated("first-reading", "GREEN"), rated("unrated", ""),
	}}}}
	recorder := &fakeQualityRecorder{}
	history := templateQualityHistory{"seen-green": "GREEN", "turned-yellow": "GREEN"}
	repo := newMockTemplateRepo()
	uc := NewSyncTemplatesUseCase(repo, &syncTemplatesFactoryMock{client: client, wabaID: "waba-1"}, history, recorder)

	if _, err := uc.Execute(template.SyncTemplatesInput{BusinessPhoneID: "phone-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(recorder.changes) != 2 {
		t.Fatalf("only the moved and the first reading are recorded, got %+v", recorder.changes)
	}
	moved := recorder.changes[0]
	if moved.ExternalID != "turned-yellow" || moved.Previous != "GREEN" || moved.Current != "YELLOW" || moved.Source != quality.SourcePoll ||
		moved.WABAID != "waba-1" || !moved.OccurredAt.Equal(time.Unix(1760000000, 0)) {
		t.Fatalf("moved change %+v", moved)
	}
	if first := recorder.changes[1]; first.ExternalID != "first-reading" || first.Previous != "" || first.Current != "GREEN" {
		t.Fatalf("first reading %+v", first)
	}
	if stored, _ := repo.FindByExternalIDAndWABA("turned-yellow", "waba-1"); stored == nil || stored.Quality != "YELLOW" {
		t.Fatalf("the template keeps its current quality, got %+v", stored)
	}
}

func TestSyncTemplates_FailsWhenTheQualityCannotBeRecorded(t *testing.T) {
	boom := errors.New("db down")
	client := &syncTemplatesClientMock{outputs: []*conversation.ListTemplatesOutput{{Templates: []conversation.Template{{ID: "t1", Name: "t1", QualityScore: "RED"}}}}}
	uc := NewSyncTemplatesUseCase(newMockTemplateRepo(), &syncTemplatesFactoryMock{client: client, wabaID: "waba-1"}, templateQualityHistory{}, &fakeQualityRecorder{err: boom})

	if _, err := uc.Execute(template.SyncTemplatesInput{BusinessPhoneID: "phone-1"}); !errors.Is(err, boom) {
		t.Fatalf("expected the record failure, got %v", err)
	}
}
