package conversation_usecase

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	lmw "vozko/domain/lead_message_window"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// Regression cover for the 2026-08-04 outage: an operator sent a voice note over the
// WebSocket, SendAudioBytes failed, and the audio branch of SendMediaMessage had
// short-declared its own err with `:=`. The failure landed in that shadow, the guard
// after the switch read the outer err as nil, and `output.MessageID` dereferenced a
// nil pointer — killing the process and every WebSocket on it.

type stubAudioEntryRepo struct{ wce.Repository }

func (stubAudioEntryRepo) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: id, LeadID: "lead-1"}, nil
}

func (stubAudioEntryRepo) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) {
	return &wce.EntryCampaignInfo{CampaignID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"}, nil
}

type stubAudioLeadRepo struct{ lead.Repository }

func (stubAudioLeadRepo) FindByID(workspaceID, id string) (*lead.Lead, error) {
	return &lead.Lead{ID: id, WorkspaceID: workspaceID, Number: "5511999999999"}, nil
}

type stubAudioWindowRepo struct{ lmw.Repository }

func (stubAudioWindowRepo) IsWindowOpen(string, string) (bool, error) { return true, nil }

type stubAudioMediaRepo struct {
	conversation.ConversationMediaRepository
	url string
}

func (m stubAudioMediaRepo) GetByID(id string) (*conversation.ConversationMedia, error) {
	return &conversation.ConversationMedia{
		ID:               id,
		URL:              m.url,
		MimeType:         "audio/ogg",
		OriginalFilename: "voice.ogg",
	}, nil
}

// stubAudioClient returns exactly what each case configures, so the (nil, nil) shape
// that the defensive guard exists for can be exercised too.
type stubAudioClient struct {
	conversation.WhatsAppClient
	out *conversation.SendTextMessageOutput
	err error
}

func (c stubAudioClient) SendAudioBytes(context.Context, string, []byte, string, string) (*conversation.SendTextMessageOutput, error) {
	return c.out, c.err
}

type stubAudioClientFactory struct{ client conversation.WhatsAppClient }

func (f stubAudioClientFactory) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}

func (f stubAudioClientFactory) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}

func (stubAudioClientFactory) WABAIdForPhone(string) (string, error) { return "waba-1", nil }

type stubAudioMessageRepo struct {
	conversation.MessageRepository
	created []*conversation.Message
}

func (r *stubAudioMessageRepo) Create(m *conversation.Message) error {
	r.created = append(r.created, m)
	return nil
}

// newAudioSender wires the smallest service that reaches the audio branch, with the
// ffmpeg call stubbed out (the box running these tests has no ffmpeg, and the
// conversion is not what is under test — only what happens to its result).
func newAudioSender(t *testing.T, out *conversation.SendTextMessageOutput, sendErr error) (*MessageSenderService, *stubAudioMessageRepo) {
	t.Helper()

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		_, _ = w.Write([]byte("source-audio-bytes"))
	}))
	t.Cleanup(cdn.Close)

	original := convertAudioToOGGOpusFn
	convertAudioToOGGOpusFn = func([]byte) ([]byte, error) { return []byte("converted-ogg-opus"), nil }
	t.Cleanup(func() { convertAudioToOGGOpusFn = original })

	msgRepo := &stubAudioMessageRepo{}
	return &MessageSenderService{
		whatsappRepo:          stubAudioEntryRepo{},
		leadRepo:              stubAudioLeadRepo{},
		messageWindowRepo:     stubAudioWindowRepo{},
		mediaRepo:             stubAudioMediaRepo{url: cdn.URL},
		whatsappClientFactory: stubAudioClientFactory{client: stubAudioClient{out: out, err: sendErr}},
		messageRepo:           msgRepo,
	}, msgRepo
}

// TestSendMediaMessage_AudioSendFailure_ReturnsErrorWithoutPanic is the outage itself.
// Before the fix this panics with "invalid memory address or nil pointer dereference".
func TestSendMediaMessage_AudioSendFailure_ReturnsErrorWithoutPanic(t *testing.T) {
	sendErr := errors.New("whatsapp send audio failed: status=400")
	svc, msgRepo := newAudioSender(t, nil, sendErr)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SendMediaMessage panicked instead of returning an error: %v", r)
		}
	}()

	msg, err := svc.SendMediaMessage("entry-1", "whatsapp", "media-1", "audio", "user-1", "", "")

	if err == nil {
		t.Fatal("expected the audio send failure to surface as an error, got nil")
	}
	if !errors.Is(err, sendErr) {
		t.Errorf("expected the underlying send error to be returned, got %v", err)
	}
	if msg != nil {
		t.Errorf("expected no message on a failed send, got %+v", msg)
	}
	if len(msgRepo.created) != 0 {
		t.Errorf("a failed send must not persist a message, got %d", len(msgRepo.created))
	}
}

// TestSendMediaMessage_NilOutputWithNilError_ReturnsErrorWithoutPanic covers the
// defensive guard: no provider path may take the process down by returning (nil, nil).
func TestSendMediaMessage_NilOutputWithNilError_ReturnsErrorWithoutPanic(t *testing.T) {
	svc, msgRepo := newAudioSender(t, nil, nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SendMediaMessage panicked on a nil output: %v", r)
		}
	}()

	msg, err := svc.SendMediaMessage("entry-1", "whatsapp", "media-1", "audio", "user-1", "", "")

	if err == nil {
		t.Fatal("expected an error when the client returns no output, got nil")
	}
	if msg != nil {
		t.Errorf("expected no message, got %+v", msg)
	}
	if len(msgRepo.created) != 0 {
		t.Errorf("nothing should be persisted, got %d messages", len(msgRepo.created))
	}
}

// TestSendMediaMessage_AudioSuccess_StillSends guards the happy path, so the fix above
// cannot be satisfied by simply failing every audio send.
func TestSendMediaMessage_AudioSuccess_StillSends(t *testing.T) {
	out := &conversation.SendTextMessageOutput{MessageID: "wamid.TEST123"}
	svc, msgRepo := newAudioSender(t, out, nil)

	msg, err := svc.SendMediaMessage("entry-1", "whatsapp", "media-1", "audio", "user-1", "", "caption here")
	if err != nil {
		t.Fatalf("expected a successful audio send, got error: %v", err)
	}
	if msg == nil {
		t.Fatal("expected a persisted message, got nil")
	}
	if msg.WhatsAppMessageID == nil || *msg.WhatsAppMessageID != "wamid.TEST123" {
		t.Errorf("expected the WhatsApp message id to be carried onto the message, got %v", msg.WhatsAppMessageID)
	}
	if len(msgRepo.created) != 1 {
		t.Fatalf("expected exactly one persisted message, got %d", len(msgRepo.created))
	}
}

// TestSendMediaMessage_AudioConversionFailure_StillReturnsError pins the branch that
// already worked, so the var-instead-of-:= change is proven not to have broken it.
func TestSendMediaMessage_AudioConversionFailure_StillReturnsError(t *testing.T) {
	svc, _ := newAudioSender(t, &conversation.SendTextMessageOutput{MessageID: "unused"}, nil)

	convErr := errors.New("ffmpeg conversion failed")
	original := convertAudioToOGGOpusFn
	convertAudioToOGGOpusFn = func([]byte) ([]byte, error) { return nil, convErr }
	t.Cleanup(func() { convertAudioToOGGOpusFn = original })

	msg, err := svc.SendMediaMessage("entry-1", "whatsapp", "media-1", "audio", "user-1", "", "")
	if err == nil {
		t.Fatal("expected a conversion failure to surface as an error, got nil")
	}
	if !errors.Is(err, convErr) {
		t.Errorf("expected the conversion error to be wrapped, got %v", err)
	}
	if msg != nil {
		t.Errorf("expected no message, got %+v", msg)
	}
}
