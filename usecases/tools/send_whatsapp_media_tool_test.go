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
	"vozko/domain/shared"
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

func (r *fakeMediaRepoForMediaTool) GetMediaForWorkspace(workspaceID, id string) (*media.Media, error) {
	if m, ok := r.byID[id]; ok && m.WorkspaceID == workspaceID {
		return m, nil
	}
	return nil, media.ErrMediaNotFound
}

const toolWorkspace = "ws-1"

func inToolWorkspace(m media.Media) *media.Media {
	if m.WorkspaceID == "" {
		m.WorkspaceID = toolWorkspace
	}
	return &m
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
func (c *recordingWhatsAppClient) SendFlowMessage(context.Context, conversation.SendFlowMessageInput) (*conversation.SendTextMessageOutput, error) {
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
func (c *recordingWhatsAppClient) EditTemplate(context.Context, string, conversation.EditTemplateInput) error {
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
	for _, m := range medias {
		repo.byID[m.ID] = inToolWorkspace(m)
	}
	return NewSendWhatsappMediaToolUseCase(context.Background(), &stubFactory{c: client}, repo, loopbackDownloads())
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
	return map[string]interface{}{"__business_phone_id": "phone-1", "__workspace_id": toolWorkspace}
}

func TestSendWhatsappMediaTool_DefinitionWithContext_PopulatesEnumAcrossAllTypes(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "m-img", Description: "Brochure", URL: "https://cdn/b.png", Type: media.MediaTypeProductImage},
		media.Media{ID: "m-vid", Description: "Demo Video", URL: "https://cdn/d.mp4", Type: media.MediaTypeProductVideo},
		media.Media{ID: "m-aud", Description: "Welcome Audio", URL: "https://cdn/w.ogg", Type: media.MediaTypeAudio},
		media.Media{ID: "m-pdf", Description: "Price List", URL: "https://cdn/p.pdf", Type: media.MediaTypeDocumentPdf},
		media.Media{ID: "m-stk", Description: "Logo Sticker", URL: "https://cdn/l.webp", Type: media.MediaTypeSticker},
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

func TestSendWhatsappMediaTool_Execute_Audio_FallsBackToLink(t *testing.T) {
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
		t.Errorf("the fallback must not upload; got uploadAudio=%d uploadMedia=%d",
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

	_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511999", "media_id": "ghost",
	})
	assertToolMediaRefused(t, err)
}

func TestSendWhatsappMediaTool_Execute_MediaWithoutURLFails(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{},
		media.Media{ID: "broken", URL: "   ", Type: media.MediaTypeProductImage})

	_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"to": "5511", "media_id": "broken",
	})
	assertToolMediaRefused(t, err)
}

func assertToolMediaRefused(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, conversation.ErrMediaNotSendable) {
		t.Fatalf("err = %v, want ErrMediaNotSendable", err)
	}
	if code := conversation.SendFailureCode(err, "send_failed"); code != conversation.MediaNotSendableCode {
		t.Fatalf("code = %q, want %q", code, conversation.MediaNotSendableCode)
	}
}

func TestSendWhatsappMediaTool_MediaOfAnotherWorkspaceIsRefused(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "foreign", WorkspaceID: "ws-2", URL: "https://cdn.example.com/a.png", Type: media.MediaTypeProductImage})

	_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{"to": "5511999999999", "media_id": "foreign"})
	assertToolMediaRefused(t, err)
	if !nothingSent(client) {
		t.Fatal("another workspace's file reached the contact")
	}
}

func TestSendWhatsappMediaTool_WithoutAWorkspaceNoLibraryMediaIsSent(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "m1", URL: "https://cdn.example.com/a.png", Type: media.MediaTypeProductImage})

	_, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{"__business_phone_id": "phone-1"}, map[string]interface{}{"to": "5511999999999", "media_id": "m1"})
	assertToolMediaRefused(t, err)
	if !nothingSent(client) {
		t.Fatal("library media went out without knowing whose library it is")
	}
}

func TestSendWhatsappMediaTool_ANoteImageIdIsNoLibraryMedia(t *testing.T) {
	client := &recordingWhatsAppClient{}
	tool := newMediaToolWith(t, client,
		media.Media{ID: "m1", URL: "https://cdn.example.com/a.png", Type: media.MediaTypeProductImage})

	_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{"to": "5511999999999", "media_id": "note-image-1"})
	assertToolMediaRefused(t, err)
	if !nothingSent(client) {
		t.Fatal("an id outside the library was sent")
	}
}

func TestSendWhatsappMediaTool_DefinitionListsOnlySendableMediaOfTheAgentsWorkspace(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{},
		media.Media{ID: "own", Description: "Catalogo", URL: "https://cdn/own.png", Type: media.MediaTypeProductImage},
		media.Media{ID: "foreign", WorkspaceID: "ws-2", Description: "Segredo", URL: "https://cdn/foreign.png", Type: media.MediaTypeProductImage},
		media.Media{ID: "sheet", Description: "Planilha", URL: "https://cdn/leads.csv", Type: media.MediaTypeLeadImport},
	)

	def := tool.DefinitionWithContext(tools.ToolContext{
		WorkspaceID: toolWorkspace,
		Agent:       &agent_domain.Agent{WorkspaceID: toolWorkspace, MediaIDs: []string{"own", "foreign", "sheet"}},
	})

	enum := def.Parameters["media_id"].Enum
	if len(enum) != 1 || enum[0] != "own" {
		t.Fatalf("enum = %v, want only the agent workspace's sendable media", enum)
	}
	if strings.Contains(def.Parameters["media_id"].Description, "Segredo") {
		t.Fatal("another workspace's media description was shown to the model")
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

	if d.Name != ToolNameSendMedia {
		t.Errorf("expected name %q, got %q", ToolNameSendMedia, d.Name)
	}
	for _, k := range []string{"to", "media_id", "media_url", "caption"} {
		if _, ok := d.Parameters[k]; !ok {
			t.Errorf("expected parameter %q", k)
		}
	}
	if sliceContains(d.Required, "media_id") || sliceContains(d.Required, "media_url") {
		t.Errorf("neither media field may be required, got %v", d.Required)
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

const toolCDN = "https://cdn.vozkoia.com"

type toolCDNHosts struct{}

func (toolCDNHosts) OwnsURL(rawURL string) bool {
	return strings.HasPrefix(strings.ToLower(rawURL), toolCDN+"/") || strings.HasPrefix(strings.ToLower(rawURL), "http://cdn.vozkoia.com/")
}

func (r *fakeMediaRepoForMediaTool) GetMediaForWorkspaceByURL(workspaceID, url string) (*media.Media, error) {
	for _, m := range r.byID {
		if m.URL == url && m.WorkspaceID == workspaceID {
			return m, nil
		}
	}
	return nil, media.ErrMediaNotFound
}

type toolUploads map[string]*conversation.ConversationMedia

func (u toolUploads) GetInEntryByURL(entryID string, entryType shared.EntryType, url string) (*conversation.ConversationMedia, error) {
	row, ok := u[url]
	if !ok || row.EntryID != entryID || row.EntryType != entryType {
		return nil, conversation.ErrMediaNotFound
	}
	return row, nil
}

func telegramUpload(url, entryID string, purpose conversation.MediaPurpose) *conversation.ConversationMedia {
	return &conversation.ConversationMedia{ID: "up-" + entryID, EntryID: entryID, EntryType: shared.EntryTypeTelegram, Type: conversation.MediaTypeImage, URL: url, Purpose: purpose}
}

func mediaToolOnTelegram(t *testing.T, uploads toolUploads, medias ...media.Media) (*SendWhatsappMediaTool, *toolAdapter) {
	t.Helper()
	adapter := &toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}
	tool := newMediaToolWith(t, &recordingWhatsAppClient{}, medias...)
	tool.SetAdapters(conversation.NewAdapterRegistry(adapter))
	tool.SetMediaURLs(&conversation.MediaURLResolver{Storage: toolCDNHosts{}, Library: tool.mediaRepo.(*fakeMediaRepoForMediaTool), Uploads: uploads})
	return tool, adapter
}

func TestSendWhatsappMediaTool_MediaURL_SendsOurFileOfThisWorkspaceOrConversation(t *testing.T) {
	library := toolCDN + "/eventobaleia/84994409624.jpeg"
	upload := toolCDN + "/conversations/conv-1/foto.jpg"
	for name, url := range map[string]string{"library item": library, "upload of this conversation": upload} {
		t.Run(name, func(t *testing.T) {
			tool, adapter := mediaToolOnTelegram(t,
				toolUploads{upload: telegramUpload(upload, "conv-1", conversation.MediaPurposeMessage)},
				media.Media{ID: "lib-1", URL: library, Type: media.MediaTypeProductImage})

			if _, err := tool.ExecuteWithConfig(context.Background(), telegramSeeds(), map[string]interface{}{
				"media_url": url, "caption": "sua foto",
			}); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if adapter.sentMedia == nil || adapter.sentMedia.URL != url || adapter.sentMedia.Kind != "image" || adapter.sentMedia.Caption != "sua foto" {
				t.Fatalf("sent %+v, want %s as an image", adapter.sentMedia, url)
			}
		})
	}
}

func TestSendWhatsappMediaTool_MediaURL_OnlyASendableCDNFileIsAccepted(t *testing.T) {
	note := toolCDN + "/conversations/conv-1/nota.jpg"
	elsewhere := toolCDN + "/conversations/conv-2/foto.jpg"
	foreign := toolCDN + "/ws-2/segredo.png"
	uploads := toolUploads{
		note:      telegramUpload(note, "conv-1", conversation.MediaPurposeNote),
		elsewhere: telegramUpload(elsewhere, "conv-2", conversation.MediaPurposeMessage),
	}
	other := media.Media{ID: "lib-2", WorkspaceID: "ws-2", URL: foreign, Type: media.MediaTypeProductImage}
	cases := map[string]string{
		"foreign host":                   "https://evil.example.com/x.jpeg",
		"internal address":               "https://169.254.169.254/latest/meta-data",
		"loopback":                       "https://127.0.0.1:8080/admin",
		"plain http":                     "http://cdn.vozkoia.com/x.jpeg",
		"host is a prefix of ours":       "https://cdn.vozkoia.com.evil.net/x.jpeg",
		"our file with no row":           toolCDN + "/guessed.png",
		"note image":                     note,
		"upload of another conversation": elsewhere,
		"another workspace's file":       foreign,
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			tool, adapter := mediaToolOnTelegram(t, uploads, other)

			_, err := tool.ExecuteWithConfig(context.Background(), telegramSeeds(), map[string]interface{}{"media_url": url})
			if err == nil {
				t.Fatalf("%s must be refused", url)
			}
			if adapter.sentMedia != nil {
				t.Errorf("refused URL must not reach the channel, got %q", adapter.sentMedia.URL)
			}
		})
	}
}

func TestSendWhatsappMediaTool_MediaURL_WithoutTheResolverNothingIsSent(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}
	tool := newMediaToolWith(t, &recordingWhatsAppClient{}, media.Media{ID: "lib-1", URL: toolCDN + "/a.png", Type: media.MediaTypeProductImage})
	tool.SetAdapters(conversation.NewAdapterRegistry(adapter))

	if _, err := tool.ExecuteWithConfig(context.Background(), telegramSeeds(), map[string]interface{}{"media_url": toolCDN + "/a.png"}); err == nil || adapter.sentMedia != nil {
		t.Fatalf("err = %v sent = %+v, want a refusal", err, adapter.sentMedia)
	}
}

func TestSendWhatsappMediaTool_NeitherMediaIDNorURL_NamesBoth(t *testing.T) {
	tool := newMediaToolWith(t, &recordingWhatsAppClient{})

	_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{
		"caption": "sem mídia",
	})
	if err == nil {
		t.Fatal("expected a refusal when no media is named")
	}
	for _, want := range []string{"media_id", "media_url"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name %q so the model can retry, got %q", want, err)
		}
	}
}

func TestSendWhatsappMediaTool_MediaIDWinsOverURL(t *testing.T) {
	tool, adapter := mediaToolOnTelegram(t, toolUploads{},
		media.Media{ID: "m1", URL: "https://cdn.vozkoia.com/library.png", Type: media.MediaTypeProductImage})

	if _, err := tool.ExecuteWithConfig(context.Background(), telegramSeeds(), map[string]interface{}{
		"media_id": "m1", "media_url": "https://cdn.vozkoia.com/guessed.png",
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if adapter.sentMedia == nil || adapter.sentMedia.URL != "https://cdn.vozkoia.com/library.png" {
		t.Errorf("expected the library URL, got %+v", adapter.sentMedia)
	}
}

func TestMediaTypeFromURL(t *testing.T) {
	cases := map[string]media.MediaType{
		"https://c/x.jpeg":           media.MediaTypeProductImage,
		"https://c/x.PNG":            media.MediaTypeProductImage,
		"https://c/x.webp":           media.MediaTypeProductImage,
		"https://c/x.mp4":            media.MediaTypeProductVideo,
		"https://c/x.ogg":            media.MediaTypeAudio,
		"https://c/x.pdf":            media.MediaTypeDocumentPdf,
		"https://c/x.docx":           media.MediaTypeDocumentDoc,
		"https://c/x.zip":            media.MediaTypeDocument,
		"https://c/x":                media.MediaTypeDocument,
		"https://c/x.jpeg?v=2&w=100": media.MediaTypeProductImage,
	}
	for url, want := range cases {
		if got := mediaTypeFromURL(url); got != want {
			t.Errorf("mediaTypeFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}
