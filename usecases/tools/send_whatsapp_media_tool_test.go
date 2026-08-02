package tools_usecase

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	agent_domain "vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/tools"
)

type fakeMediaRepoForMediaTool struct {
	byID map[string]*media.Media
}

func (r *fakeMediaRepoForMediaTool) CreateMedia(*media.Media) error { return nil }
func (r *fakeMediaRepoForMediaTool) ListMediasByWorkspace(string) ([]media.Media, error) {
	return nil, nil
}
func (r *fakeMediaRepoForMediaTool) DeleteMedia(string) error                         { return nil }
func (r *fakeMediaRepoForMediaTool) CountWorkspaceUploadsToday(string) (int64, error) { return 0, nil }
func (r *fakeMediaRepoForMediaTool) MediaExists(string) (bool, error)                 { return true, nil }
func (r *fakeMediaRepoForMediaTool) CountByWorkspaceID(string) (int64, error)         { return 0, nil }
func (r *fakeMediaRepoForMediaTool) CountByWorkspaceIDAndType(string, media.MediaType) (int64, error) {
	return 0, nil
}

func (r *fakeMediaRepoForMediaTool) GetMediaByID(id string) (*media.Media, error) {
	if r == nil {
		return nil, nil
	}
	if m, ok := r.byID[id]; ok {
		return m, nil
	}
	return nil, nil
}

func (r *fakeMediaRepoForMediaTool) GetMediasByIDs(ids []string) ([]media.Media, error) {
	out := make([]media.Media, 0, len(ids))
	for _, id := range ids {
		if m, ok := r.byID[id]; ok && m != nil {
			out = append(out, *m)
		}
	}
	return out, nil
}

type recordingWhatsAppClient struct {
	mu sync.Mutex

	uploadImageCalls    int
	uploadMediaCalls    int
	uploadAudioCalls    int
	lastUploadFileName  string
	lastUploadMimeType  string
	lastUploadByteCount int

	sendImageInput    *conversation.SendImageMessageInput
	sendVideoInput    *conversation.SendVideoMessageInput
	sendAudioInput    *conversation.SendAudioMessageInput
	sendDocumentInput *conversation.SendDocumentMessageInput
	sendStickerInput  *conversation.SendStickerMessageInput

	uploadImageErr  error
	uploadMediaErr  error
	uploadAudioErr  error
	sendImageErr    error
	sendVideoErr    error
	sendAudioErr    error
	sendDocumentErr error
	sendStickerErr  error
}

func (c *recordingWhatsAppClient) UploadImage(_ context.Context, data []byte, fileName, mimeType string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uploadImageCalls++
	c.lastUploadFileName = fileName
	c.lastUploadMimeType = mimeType
	c.lastUploadByteCount = len(data)
	if c.uploadImageErr != nil {
		return "", c.uploadImageErr
	}
	return "wa-image-id", nil
}

func (c *recordingWhatsAppClient) UploadMedia(_ context.Context, data []byte, fileName, mimeType string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uploadMediaCalls++
	c.lastUploadFileName = fileName
	c.lastUploadMimeType = mimeType
	c.lastUploadByteCount = len(data)
	if c.uploadMediaErr != nil {
		return "", c.uploadMediaErr
	}
	return "wa-media-id", nil
}

func (c *recordingWhatsAppClient) UploadAudio(_ context.Context, data []byte, fileName string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uploadAudioCalls++
	c.lastUploadFileName = fileName
	c.lastUploadByteCount = len(data)
	if c.uploadAudioErr != nil {
		return "", c.uploadAudioErr
	}
	return "wa-audio-id", nil
}

func (c *recordingWhatsAppClient) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return nil, "", errors.New("not implemented")
}

func (c *recordingWhatsAppClient) SendImageMessage(_ context.Context, in conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.mu.Lock()
	c.sendImageInput = &in
	err := c.sendImageErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid.image"}, nil
}

func (c *recordingWhatsAppClient) SendVideoMessage(_ context.Context, in conversation.SendVideoMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.mu.Lock()
	c.sendVideoInput = &in
	err := c.sendVideoErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid.video"}, nil
}

func (c *recordingWhatsAppClient) SendAudioMessage(_ context.Context, in conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.mu.Lock()
	c.sendAudioInput = &in
	err := c.sendAudioErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid.audio"}, nil
}

func (c *recordingWhatsAppClient) SendAudioBytes(context.Context, string, []byte, string, string) (*conversation.SendTextMessageOutput, error) {
	return nil, errors.New("not used in this tool")
}

func (c *recordingWhatsAppClient) SendDocumentMessage(_ context.Context, in conversation.SendDocumentMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.mu.Lock()
	c.sendDocumentInput = &in
	err := c.sendDocumentErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid.doc"}, nil
}

func (c *recordingWhatsAppClient) SendStickerMessage(_ context.Context, in conversation.SendStickerMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.mu.Lock()
	c.sendStickerInput = &in
	err := c.sendStickerErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid.sticker"}, nil
}

func (c *recordingWhatsAppClient) SendTextMessage(context.Context, conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) SendButtonMessage(context.Context, conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) SendListMessage(context.Context, conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) SendTypingIndicator(context.Context, string) error { return nil }
func (c *recordingWhatsAppClient) MarkMessageAsRead(context.Context, string) error   { return nil }
func (c *recordingWhatsAppClient) SendTemplateMessage(context.Context, conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) ListTemplates(context.Context, conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) GetTemplate(context.Context, string) (*conversation.Template, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) CreateTemplate(context.Context, conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	return nil, errors.New("not used")
}
func (c *recordingWhatsAppClient) UpdateTemplate(context.Context, string, conversation.UpdateTemplateInput) error {
	return errors.New("not used")
}
func (c *recordingWhatsAppClient) DeleteTemplate(context.Context, conversation.DeleteTemplateInput) error {
	return errors.New("not used")
}
func (c *recordingWhatsAppClient) UploadMediaForTemplate(context.Context, conversation.UploadMediaForTemplateInput) (string, error) {
	return "", errors.New("not used")
}

type stubFactory struct {
	c *recordingWhatsAppClient
}

func (f *stubFactory) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	return f.c, nil
}
func (f *stubFactory) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return f.c, nil
}
func (f *stubFactory) WABAIdForPhone(string) (string, error) { return "waba-stub", nil }

func newMediaToolWith(t *testing.T, client *recordingWhatsAppClient, medias ...media.Media) *SendWhatsappMediaTool {
	t.Helper()
	repo := &fakeMediaRepoForMediaTool{byID: make(map[string]*media.Media, len(medias))}
	for i := range medias {
		m := medias[i]
		repo.byID[m.ID] = &m
	}
	return NewSendWhatsappMediaToolUseCase(context.Background(), &stubFactory{c: client}, repo)
}

func servePNG(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}))
}

func serveBytes(payload []byte, contentType string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(payload)
	}))
}

func cfg() map[string]interface{} {
	return map[string]interface{}{"__business_phone_id": "phone-1"}
}

func TestSendWhatsappMediaTool_DefinitionWithContext_PopulatesEnumAcrossAllTypes(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "m-img", Description: "Brochure", Type: media.MediaTypeProductImage},
		media.Media{ID: "m-vid", Description: "Demo Video", Type: media.MediaTypeProductVideo},
		media.Media{ID: "m-aud", Description: "Welcome Audio", Type: media.MediaTypeAudio},
		media.Media{ID: "m-pdf", Description: "Price List", Type: media.MediaTypeDocumentPdf},
		media.Media{ID: "m-stk", Description: "Logo Sticker", Type: media.MediaTypeSticker},
	)

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID: "ws-1",
		Agent:       &agent_domain.Agent{WorkspaceID: "ws-1", MediaIDs: []string{"m-img", "m-vid", "m-aud", "m-pdf", "m-stk"}},
	})

	mp, ok := def.Parameters["media_id"]
	if !ok {
		t.Fatalf("media_id missing")
	}
	if len(mp.Enum) != 5 {
		t.Fatalf("expected 5 enum entries, got %d (%v)", len(mp.Enum), mp.Enum)
	}
	for _, want := range []string{"m-img", "m-vid", "m-aud", "m-pdf", "m-stk"} {
		if !sliceContains(mp.Enum, want) {
			t.Errorf("expected enum to include %q, got %v", want, mp.Enum)
		}
	}
	for _, want := range []string{"imagem", "vídeo", "áudio", "documento", "sticker"} {
		if !strings.Contains(mp.Description, want) {
			t.Errorf("expected description to mention pretty type %q, got %q", want, mp.Description)
		}
	}
}

func TestSendWhatsappMediaTool_DefinitionWithContext_AgentMissingFallsBackToBase(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{},
		media.Media{ID: "m-1", Type: media.MediaTypeProductImage})

	def := tool.DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"})

	if len(def.Parameters["media_id"].Enum) != 0 {
		t.Fatalf("expected no enum when Agent missing, got %v", def.Parameters["media_id"].Enum)
	}
}

func TestSendWhatsappMediaTool_DefinitionWithContext_AgentWithEmptyMediaIDsFallsBack(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{})

	def := tool.DefinitionWithContext(tools.ToolContext{
		Agent: &agent_domain.Agent{WorkspaceID: "ws-1", MediaIDs: nil},
	})
	if len(def.Parameters["media_id"].Enum) != 0 {
		t.Fatalf("agent without MediaIDs must not produce an enum")
	}
}

func TestSendWhatsappMediaTool_Execute_Image_UploadsAndSends(t *testing.T) {
	client := &recordingWhatsAppClient{}
	srv := servePNG(t)
	defer srv.Close()

	tool := newMediaToolWith(t, client,
		media.Media{ID: "m1", URL: srv.URL + "/img.png", Type: media.MediaTypeProductImage})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "m1", "caption": "hello",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "Image sent successfully") {
		t.Errorf("unexpected result %q", res.Result)
	}
	if client.uploadImageCalls != 1 {
		t.Errorf("expected UploadImage called once, got %d", client.uploadImageCalls)
	}
	if client.sendImageInput == nil {
		t.Fatalf("SendImageMessage was not called")
	}
	if client.sendImageInput.ImageID != "wa-image-id" {
		t.Errorf("expected ImageID=wa-image-id, got %q", client.sendImageInput.ImageID)
	}
	if client.sendImageInput.Caption != "hello" {
		t.Errorf("expected caption 'hello', got %q", client.sendImageInput.Caption)
	}
	if client.sendImageInput.Link != "" {
		t.Errorf("expected Link empty on upload path, got %q", client.sendImageInput.Link)
	}
}

func TestSendWhatsappMediaTool_Execute_Image_FetchFailsFallsBackToLink(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "m1", URL: "http://127.0.0.1:1/missing.png", Type: media.MediaTypeProductImage})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "m1", "caption": "hi",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "via link") {
		t.Errorf("expected fallback message, got %q", res.Result)
	}
	if client.uploadImageCalls != 0 {
		t.Errorf("UploadImage should NOT be called when fetch fails, got %d", client.uploadImageCalls)
	}
	if client.sendImageInput == nil || client.sendImageInput.Link == "" {
		t.Fatalf("SendImageMessage(link) was not called: %+v", client.sendImageInput)
	}
}

func TestSendWhatsappMediaTool_Execute_Image_UploadFailsFallsBackToLink(t *testing.T) {
	client := &recordingWhatsAppClient{uploadImageErr: errors.New("boom")}
	srv := servePNG(t)
	defer srv.Close()

	tool := newMediaToolWith(t, client,
		media.Media{ID: "m1", URL: srv.URL + "/img.png", Type: media.MediaTypeProductImage})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "m1",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "via link") {
		t.Errorf("expected fallback, got %q", res.Result)
	}
	if client.sendImageInput == nil || client.sendImageInput.Link == "" {
		t.Fatalf("expected SendImageMessage with link, got %+v", client.sendImageInput)
	}
}

func TestSendWhatsappMediaTool_Execute_Video_UploadsAndSends(t *testing.T) {
	client := &recordingWhatsAppClient{}
	srv := serveBytes([]byte("\x00\x00\x00\x18ftypmp42fakebytes"), "video/mp4")
	defer srv.Close()

	tool := newMediaToolWith(t, client,
		media.Media{ID: "v1", URL: srv.URL + "/clip.mp4", Type: media.MediaTypeProductVideo})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "v1", "caption": "watch this",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "Video sent successfully") {
		t.Errorf("unexpected result %q", res.Result)
	}
	if client.uploadMediaCalls != 1 {
		t.Errorf("expected UploadMedia 1x, got %d", client.uploadMediaCalls)
	}
	if client.lastUploadMimeType != "video/mp4" {
		t.Errorf("expected mime video/mp4, got %q", client.lastUploadMimeType)
	}
	if client.sendVideoInput == nil || client.sendVideoInput.VideoID != "wa-media-id" {
		t.Fatalf("SendVideoMessage not invoked correctly: %+v", client.sendVideoInput)
	}
	if client.sendVideoInput.Caption != "watch this" {
		t.Errorf("caption lost, got %q", client.sendVideoInput.Caption)
	}
}

func TestSendWhatsappMediaTool_Execute_VslVideo_TreatedAsVideo(t *testing.T) {
	client := &recordingWhatsAppClient{}
	srv := serveBytes([]byte("fakemp4"), "video/mp4")
	defer srv.Close()

	tool := newMediaToolWith(t, client,
		media.Media{ID: "vsl1", URL: srv.URL + "/x.mp4", Type: media.MediaTypeVslVideo})

	if _, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "vsl1",
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if client.sendVideoInput == nil {
		t.Fatalf("VSL must dispatch as video; SendVideoMessage was not called")
	}
}

func TestSendWhatsappMediaTool_Execute_Audio_SendsViaLink(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "a1", URL: "https://cdn.example.com/welcome.mp3", Type: media.MediaTypeAudio})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "a1", "caption": "ignored",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "Audio sent successfully") {
		t.Errorf("unexpected result %q", res.Result)
	}
	if client.uploadAudioCalls != 0 || client.uploadMediaCalls != 0 {
		t.Errorf("audio path must not upload (uses CDN link); got uploadAudio=%d uploadMedia=%d",
			client.uploadAudioCalls, client.uploadMediaCalls)
	}
	if client.sendAudioInput == nil {
		t.Fatalf("SendAudioMessage not called")
	}
	if client.sendAudioInput.AudioURL != "https://cdn.example.com/welcome.mp3" {
		t.Errorf("expected CDN URL forwarded, got %q", client.sendAudioInput.AudioURL)
	}
}

func TestSendWhatsappMediaTool_Execute_Document_UploadsAndSends(t *testing.T) {
	client := &recordingWhatsAppClient{}
	srv := serveBytes([]byte("%PDF-1.4 fake"), "application/pdf")
	defer srv.Close()

	tool := newMediaToolWith(t, client,
		media.Media{ID: "d1", URL: srv.URL + "/price-list.pdf", Description: "Price List", Type: media.MediaTypeDocumentPdf})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "d1", "caption": "pre├ºos",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "Document sent successfully") {
		t.Errorf("unexpected result %q", res.Result)
	}
	if client.uploadMediaCalls != 1 {
		t.Errorf("expected UploadMedia 1x, got %d", client.uploadMediaCalls)
	}
	if client.lastUploadMimeType != "application/pdf" {
		t.Errorf("expected pdf mime, got %q", client.lastUploadMimeType)
	}
	if client.sendDocumentInput == nil {
		t.Fatalf("SendDocumentMessage not called")
	}
	if client.sendDocumentInput.DocumentID != "wa-media-id" {
		t.Errorf("expected DocumentID=wa-media-id, got %q", client.sendDocumentInput.DocumentID)
	}
	if client.sendDocumentInput.Filename == "" {
		t.Errorf("expected non-empty Filename so WhatsApp shows a sane label")
	}
	if client.sendDocumentInput.Caption != "pre├ºos" {
		t.Errorf("caption lost: %q", client.sendDocumentInput.Caption)
	}
}

func TestSendWhatsappMediaTool_Execute_Document_FallsBackToLinkOnUploadError(t *testing.T) {
	client := &recordingWhatsAppClient{uploadMediaErr: errors.New("upload boom")}
	srv := serveBytes([]byte("%PDF-1.4 fake"), "application/pdf")
	defer srv.Close()

	tool := newMediaToolWith(t, client,
		media.Media{ID: "d1", URL: srv.URL + "/x.pdf", Type: media.MediaTypeDocument})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "d1",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "via link") {
		t.Errorf("expected fallback, got %q", res.Result)
	}
	if client.sendDocumentInput == nil || client.sendDocumentInput.Link == "" {
		t.Fatalf("expected SendDocumentMessage(link), got %+v", client.sendDocumentInput)
	}
}

func TestSendWhatsappMediaTool_Execute_Sticker_SendsViaLink(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "s1", URL: "https://cdn.example.com/logo.webp", Type: media.MediaTypeSticker})

	res, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999999999", "media_id": "s1",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(toStr(res.Result), "Sticker sent successfully") {
		t.Errorf("unexpected result %q", res.Result)
	}
	if client.sendStickerInput == nil {
		t.Fatalf("SendStickerMessage not called")
	}
	if client.sendStickerInput.Link != "https://cdn.example.com/logo.webp" {
		t.Errorf("expected CDN URL forwarded, got %q", client.sendStickerInput.Link)
	}
}

func TestSendWhatsappMediaTool_Execute_MissingBusinessPhoneIDFails(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{},
		media.Media{ID: "m1", URL: "http://x", Type: media.MediaTypeProductImage})

	if _, err := tool.ExecuteWithConfig(context.Background(), nil, map[string]interface{}{
		"to": "5511", "media_id": "m1",
	}); err == nil {
		t.Fatalf("expected error when config is nil")
	}
	if _, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{}, map[string]interface{}{
		"to": "5511", "media_id": "m1",
	}); err == nil {
		t.Fatalf("expected error when __business_phone_id missing")
	}
}

func TestSendWhatsappMediaTool_Execute_MediaNotFound(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{})

	if _, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999", "media_id": "ghost",
	}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestSendWhatsappMediaTool_Execute_MediaWithoutURLFails(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{},
		media.Media{ID: "broken", URL: "   ", Type: media.MediaTypeProductImage})

	if _, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511", "media_id": "broken",
	}); err == nil || !strings.Contains(err.Error(), "missing a URL") {
		t.Fatalf("expected missing-URL error, got %v", err)
	}
}

func sliceContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func toStr(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func TestSendWhatsappMediaTool_Definition_HasExpectedShape(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{})
	d := tool.Definition()

	// Channel-neutral: the name reaches the model, and one naming a channel
	// makes the model decline to use it in a conversation on another.
	if d.Name != ToolNameSendMedia {
		t.Errorf("expected name %q, got %q", ToolNameSendMedia, d.Name)
	}
	for _, k := range []string{"to", "media_id", "caption"} {
		if _, ok := d.Parameters[k]; !ok {
			t.Errorf("expected parameter %q", k)
		}
	}
	// Only the media is required. "to" is a phone number, which does not exist
	// on Telegram or Instagram — requiring it made the tool unusable there. It
	// remains accepted for saved WhatsApp agents that were taught to pass one.
	if !sliceContains(d.Required, "media_id") {
		t.Errorf("expected media_id in Required, got %v", d.Required)
	}
	if sliceContains(d.Required, "to") {
		t.Errorf("\"to\" must be optional so the tool works without a phone number, got %v", d.Required)
	}
	if sliceContains(d.Required, "caption") {
		t.Errorf("caption must be optional (image/video/document only)")
	}
}

func (f *recordingWhatsAppClient) SendCallPermissionRequest(context.Context, conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	return &conversation.SendTextMessageOutput{}, nil
}
