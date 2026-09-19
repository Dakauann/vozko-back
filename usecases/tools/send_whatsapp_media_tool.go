package tools_usecase

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/tools"
	media_infra "vozko/infra/media"
)

type SendWhatsappMediaTool struct {
	ctx                   context.Context
	whatsappClientFactory conversation.WhatsAppClientFactory
	mediaRepo             media.MediaRepository
	// adapters routes the send on every channel that is not WhatsApp. Optional:
	// unset keeps the tool WhatsApp-only, which is what it was before.
	adapters conversation.AdapterRegistry
}

// SetAdapters wires the channel registry so the tool can send anywhere.
func (uc *SendWhatsappMediaTool) SetAdapters(r conversation.AdapterRegistry) {
	uc.adapters = r
}

const (
	maxImageBytes    = 5 * 1024 * 1024
	maxVideoBytes    = 16 * 1024 * 1024
	maxAudioBytes    = 16 * 1024 * 1024
	maxDocumentBytes = 100 * 1024 * 1024
	maxStickerBytes  = 500 * 1024
)

// ToolNameSendMedia is deliberately channel-neutral. The name is part of the
// prompt the model reads, and a tool called "send_whatsapp_media" offered inside
// a Telegram conversation reads as belonging to another channel, a model that
// declines to use it is behaving sensibly. Saved bindings under the old name
// keep working through CanonicalToolName.
const ToolNameSendMedia = "send_media"

const LegacyToolNameSendWhatsappImage = "send_whatsapp_image"

func NewSendWhatsappMediaToolUseCase(
	ctx context.Context,
	whatsappClientFactory conversation.WhatsAppClientFactory,
	mediaRepo media.MediaRepository,
) *SendWhatsappMediaTool {
	return &SendWhatsappMediaTool{
		ctx:                   ctx,
		whatsappClientFactory: whatsappClientFactory,
		mediaRepo:             mediaRepo,
	}
}

func (uc *SendWhatsappMediaTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        ToolNameSendMedia,
		DisplayName: "Enviar Mídia",
		Description: "Envia uma mídia (imagem, vídeo, áudio, documento ou sticker) ao contato da conversa atual. " +
			"O tipo de envio é determinado automaticamente pelo tipo da mídia escolhida.",
		DisplayDescription: "Envia uma mídia (imagem, vídeo, áudio, documento ou sticker) ao contato.",
		Parameters: map[string]tools.Parameter{
			"to": {
				Type: "string",
				// Optional: the recipient is the conversation the agent is
				// already in. It remains accepted because saved WhatsApp agents
				// were taught to pass a number, and because the WhatsApp path
				// still addresses by number.
				Description:        "Opcional. Destinatário no WhatsApp (formato 5511999999999). Deixe vazio para enviar ao contato da conversa atual.",
				DisplayName:        "Destinatário",
				DisplayDescription: "Opcional, por padrão, o contato da conversa atual",
			},
			"media_id": {
				Type:               "string",
				Description:        "ID da mídia a ser enviada. Escolha um dos valores listados no enum. Use este OU media_url, nunca os dois.",
				DisplayName:        "ID da Mídia",
				DisplayDescription: "ID da mídia a ser enviada",
			},
			"media_url": {
				Type: "string",
				Description: "URL direta da mídia, para arquivos que não estão na biblioteca do agente " +
					"(por exemplo, um endereço descoberto com http_request). Aceita apenas https no domínio do próprio CDN. " +
					"Use este OU media_id, nunca os dois.",
				DisplayName:        "URL da Mídia",
				DisplayDescription: "URL direta da mídia (apenas do CDN)",
			},
			"caption": {
				Type:               "string",
				Description:        "Legenda opcional. Aplicada apenas para imagem, vídeo e documento; ignorada para áudio e sticker.",
				DisplayName:        "Legenda",
				DisplayDescription: "Legenda opcional (apenas para imagem, vídeo e documento)",
			},
		},
		// Neither media field is listed: the tool takes media_id OR media_url, and a
		// schema cannot express that choice. ExecuteWithConfig rejects a call that
		// brings neither, with a message that names both. Requiring "to" would make
		// the tool unusable on every channel that has no phone number.
		Required: []string{},
		Visibility: []tools.ToolVisibility{
			tools.VisibilityMessaging,
		},
		Category: tools.CategoryMessaging,
	}
}

func (uc *SendWhatsappMediaTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	base := uc.Definition()

	ag, ok := ctx.Agent.(*agent.Agent)
	if !ok || ag == nil || len(ag.MediaIDs) == 0 {
		return base
	}

	medias, err := uc.mediaRepo.GetMediasByIDs(ag.MediaIDs)
	if err != nil || len(medias) == 0 {
		return base
	}

	ids := make([]string, 0, len(medias))
	descParts := make([]string, 0, len(medias))
	for _, m := range medias {
		ids = append(ids, m.ID)
		desc := m.Description
		if desc == "" {
			desc = string(m.Type)
		}
		descParts = append(descParts, fmt.Sprintf("  • %s (%s) → %s", m.ID, prettyTypeLabel(m.Type), desc))
	}

	result := base
	result.Parameters = make(map[string]tools.Parameter, len(base.Parameters))
	for k, v := range base.Parameters {
		result.Parameters[k] = v
	}

	result.Parameters["media_id"] = tools.Parameter{
		Type: "string",
		Description: fmt.Sprintf(
			"ID da mídia a ser enviada. Escolha UMA das %d mídias disponíveis. "+
				"O tipo de envio (imagem/vídeo/áudio/documento/sticker) é determinado automaticamente:\n\n%s",
			len(ids), strings.Join(descParts, "\n"),
		),
		DisplayName:        "ID da Mídia",
		DisplayDescription: "ID da mídia a ser enviada",
		Enum:               ids,
	}

	return result
}

var _ tools.ContextualHandler = (*SendWhatsappMediaTool)(nil)

func (uc *SendWhatsappMediaTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, fmt.Errorf("%s requires ExecuteWithConfig with __business_phone_id", ToolNameSendMedia)
}

func (uc *SendWhatsappMediaTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	to, _ := params["to"].(string)
	caption, _ := params["caption"].(string)

	mediaItem, err := uc.resolveMediaItem(params)
	if err != nil {
		return tools.ExecutionResult{}, err
	}

	// Every channel but WhatsApp sends through its adapter, addressed by the
	// conversation rather than by a phone number.
	if adapter, ec, ok := resolveToolAdapter(ctx, uc.adapters, config); ok {
		return sendMediaViaAdapter(ctx, adapter, ec, mediaItem, caption)
	}

	whatsappClient, err := uc.resolveClient(config)
	if err != nil {
		return tools.ExecutionResult{}, err
	}

	switch mediaKindFor(mediaItem.Type) {
	case kindImage:
		return uc.sendImage(ctx, whatsappClient, to, caption, mediaItem)
	case kindVideo:
		return uc.sendVideo(ctx, whatsappClient, to, caption, mediaItem)
	case kindAudio:
		return uc.sendAudio(ctx, whatsappClient, to, mediaItem)
	case kindDocument:
		return uc.sendDocument(ctx, whatsappClient, to, caption, mediaItem)
	case kindSticker:
		return uc.sendSticker(ctx, whatsappClient, to, mediaItem)
	default:
		return tools.ExecutionResult{}, fmt.Errorf("unsupported media type %q for WhatsApp send", mediaItem.Type)
	}
}

// resolveMediaItem picks what to send from either source. Everything downstream
// already works off a URL alone, so a direct URL only has to skip the library
// lookup and carry a type; nothing about fetching, normalizing or uploading changes.
func (uc *SendWhatsappMediaTool) resolveMediaItem(params map[string]interface{}) (*media.Media, error) {
	mediaID, _ := params["media_id"].(string)
	mediaID = strings.TrimSpace(mediaID)
	mediaURL, _ := params["media_url"].(string)
	mediaURL = strings.TrimSpace(mediaURL)

	// media_id wins when both arrive: it names a curated row, so it is the more
	// deliberate of the two.
	if mediaID != "" {
		item, err := uc.mediaRepo.GetMediaByID(mediaID)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, fmt.Errorf("media with ID %s not found", mediaID)
		}
		if strings.TrimSpace(item.URL) == "" {
			return nil, fmt.Errorf("media with ID %s is missing a URL", mediaID)
		}
		return item, nil
	}

	if mediaURL != "" {
		if err := allowedMediaURL(mediaURL); err != nil {
			return nil, err
		}
		return &media.Media{URL: mediaURL, Type: mediaTypeFromURL(mediaURL)}, nil
	}

	return nil, fmt.Errorf("%s requires media_id (from the agent's library) or media_url (a direct CDN link)", ToolNameSendMedia)
}

// allowedMediaURL is the boundary that a media_id never needed. A library id is
// chosen by an operator; a URL is chosen by the model from whatever it just read in
// a conversation or an API response. Without this, a crafted message could steer
// the fetch below at an internal address and have the reply mailed to the contact.
// Fails closed: no configured CDN means no URL sends at all.
func allowedMediaURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid media_url: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("media_url must use https, got %q", parsed.Scheme)
	}

	allowed := mediaURLAllowedHost()
	if allowed == "" {
		return fmt.Errorf("media_url is unavailable: no CDN host is configured")
	}
	if !strings.EqualFold(parsed.Hostname(), allowed) {
		return fmt.Errorf("media_url host %q is not allowed, only %s may be sent by URL", parsed.Hostname(), allowed)
	}
	return nil
}

// mediaURLAllowedHost reads the host from the same setting that produces our media
// URLs, so the allowlist follows the CDN instead of being a second value to keep in
// sync with it.
func mediaURLAllowedHost() string {
	endpoint := strings.TrimSpace(os.Getenv("CLOUDFLARE_R2_ENDPOINT"))
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// mediaTypeFromURL infers the send path from the extension. A library row carries a
// declared type; a bare URL has only its name to go on, and the send switch needs
// one. Anything unrecognized goes out as a document, which every channel accepts.
func mediaTypeFromURL(raw string) media.MediaType {
	ext := strings.ToLower(path.Ext(strings.SplitN(raw, "?", 2)[0]))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return media.MediaTypeProductImage
	case ".mp4", ".mov", ".webm", ".mkv", ".avi", ".3gp":
		return media.MediaTypeProductVideo
	case ".mp3", ".ogg", ".opus", ".wav", ".aac", ".m4a", ".amr":
		return media.MediaTypeAudio
	case ".pdf":
		return media.MediaTypeDocumentPdf
	case ".doc", ".docx":
		return media.MediaTypeDocumentDoc
	default:
		return media.MediaTypeDocument
	}
}

func (uc *SendWhatsappMediaTool) resolveClient(config map[string]interface{}) (conversation.WhatsAppClient, error) {
	if config == nil {
		return nil, fmt.Errorf("no WhatsApp client available: missing __business_phone_id in config")
	}
	phoneID, ok := config["__business_phone_id"].(string)
	if !ok || phoneID == "" {
		return nil, fmt.Errorf("no WhatsApp client available: missing __business_phone_id in config")
	}
	c, err := uc.whatsappClientFactory.ClientForPhone(phoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve WhatsApp client: %w", err)
	}
	if c == nil {
		return nil, fmt.Errorf("no WhatsApp client available for phone %s", phoneID)
	}
	return c, nil
}

func (uc *SendWhatsappMediaTool) sendImage(ctx context.Context, c conversation.WhatsAppClient, to, caption string, m *media.Media) (tools.ExecutionResult, error) {
	imageData, mimeType, fileName, err := fetchMedia(m.URL, maxImageBytes)
	if err != nil {
		log.Printf("[%s][image] fetch failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendImageByLink(ctx, c, to, caption, m.URL)
	}

	normalizedData, normalizedMime, normalizedExt, err := normalizeImageForWhatsApp(imageData, mimeType)
	if err != nil {
		log.Printf("[%s][image] normalize failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendImageByLink(ctx, c, to, caption, m.URL)
	}

	normalizedFileName := strings.TrimSuffix(fileName, path.Ext(fileName)) + normalizedExt

	waMediaID, err := c.UploadImage(ctx, normalizedData, normalizedFileName, normalizedMime)
	if err != nil {
		log.Printf("[%s][image] upload failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendImageByLink(ctx, c, to, caption, m.URL)
	}

	if _, err := c.SendImageMessage(ctx, conversation.SendImageMessageInput{
		To:      to,
		Caption: caption,
		ImageID: waMediaID,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Image sent successfully to %s", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendImageByLink(ctx context.Context, c conversation.WhatsAppClient, to, caption, link string) (tools.ExecutionResult, error) {
	if _, err := c.SendImageMessage(ctx, conversation.SendImageMessageInput{
		To: to, Caption: caption, Link: link,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Image sent successfully to %s (via link)", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendVideo(ctx context.Context, c conversation.WhatsAppClient, to, caption string, m *media.Media) (tools.ExecutionResult, error) {
	data, mimeType, fileName, err := fetchMedia(m.URL, maxVideoBytes)
	if err != nil {
		log.Printf("[%s][video] fetch failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendVideoByLink(ctx, c, to, caption, m.URL)
	}
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	waMediaID, err := c.UploadMedia(ctx, data, fileName, mimeType)
	if err != nil {
		log.Printf("[%s][video] upload failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendVideoByLink(ctx, c, to, caption, m.URL)
	}

	if _, err := c.SendVideoMessage(ctx, conversation.SendVideoMessageInput{
		To:      to,
		Caption: caption,
		VideoID: waMediaID,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Video sent successfully to %s", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendVideoByLink(ctx context.Context, c conversation.WhatsAppClient, to, caption, link string) (tools.ExecutionResult, error) {
	if _, err := c.SendVideoMessage(ctx, conversation.SendVideoMessageInput{
		To: to, Caption: caption, Link: link,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Video sent successfully to %s (via link)", to)}, nil
}

// convertAudioToOGGOpusFn indirects the transcode so a test can drive both the
// happy path and the fallback on a machine with no ffmpeg.
var convertAudioToOGGOpusFn = media_infra.ConvertToOGGOpus

// sendAudio delivers audio the way the Cloud API actually accepts it.
//
// Handing Meta a link let it fetch the file and decide, and for audio it accepts
// exactly one OGG: the OPUS one. A library file that is OGG/Vorbis — what most
// converters and DAWs export by default — has the right container, the right
// extension and the right Content-Type, and is still refused with 131053
// ("uploaded with mimetype as audio/ogg; codecs=opus, however on processing it
// is of type application/octet-stream"). The whole campaign fails, one
// undelivered message at a time, and the file looks perfectly fine.
//
// So transcode first and upload the bytes, which is what the operator send and
// the workflow send node already do. The link stays as the fallback: it is what
// this did before, so a download or ffmpeg failure is no worse than today.
func (uc *SendWhatsappMediaTool) sendAudio(ctx context.Context, c conversation.WhatsAppClient, to string, m *media.Media) (tools.ExecutionResult, error) {
	data, _, _, err := fetchMedia(m.URL, maxAudioBytes)
	if err != nil {
		log.Printf("[%s][audio] fetch failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendAudioByLink(ctx, c, to, m.URL)
	}

	ogg, err := convertAudioToOGGOpusFn(data)
	if err != nil {
		log.Printf("[%s][audio] transcode failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendAudioByLink(ctx, c, to, m.URL)
	}
	log.Printf("[%s][audio] transcoded for voice note: %d bytes -> %d bytes", ToolNameSendMedia, len(data), len(ogg))

	if _, err := c.SendAudioBytes(ctx, to, ogg, "voice.ogg", ""); err != nil {
		log.Printf("[%s][audio] send of transcoded bytes failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendAudioByLink(ctx, c, to, m.URL)
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Audio sent successfully to %s", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendAudioByLink(ctx context.Context, c conversation.WhatsAppClient, to, link string) (tools.ExecutionResult, error) {
	if _, err := c.SendAudioMessage(ctx, conversation.SendAudioMessageInput{
		To:       to,
		AudioURL: link,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Audio sent successfully to %s", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendDocument(ctx context.Context, c conversation.WhatsAppClient, to, caption string, m *media.Media) (tools.ExecutionResult, error) {
	data, mimeType, fileName, err := fetchMedia(m.URL, maxDocumentBytes)
	if err != nil {
		log.Printf("[%s][document] fetch failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendDocumentByLink(ctx, c, to, caption, m.URL, deriveDocumentFilename(m, ""))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if fileName == "" {
		fileName = deriveDocumentFilename(m, mimeType)
	}

	waMediaID, err := c.UploadMedia(ctx, data, fileName, mimeType)
	if err != nil {
		log.Printf("[%s][document] upload failed, falling back to link: %v", ToolNameSendMedia, err)
		return uc.sendDocumentByLink(ctx, c, to, caption, m.URL, fileName)
	}

	if _, err := c.SendDocumentMessage(ctx, conversation.SendDocumentMessageInput{
		To:         to,
		Caption:    caption,
		DocumentID: waMediaID,
		Filename:   fileName,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Document sent successfully to %s", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendDocumentByLink(ctx context.Context, c conversation.WhatsAppClient, to, caption, link, filename string) (tools.ExecutionResult, error) {
	if _, err := c.SendDocumentMessage(ctx, conversation.SendDocumentMessageInput{
		To: to, Caption: caption, Link: link, Filename: filename,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Document sent successfully to %s (via link)", to)}, nil
}

func (uc *SendWhatsappMediaTool) sendSticker(ctx context.Context, c conversation.WhatsAppClient, to string, m *media.Media) (tools.ExecutionResult, error) {
	if _, err := c.SendStickerMessage(ctx, conversation.SendStickerMessageInput{
		To:   to,
		Link: m.URL,
	}); err != nil {
		return tools.ExecutionResult{}, err
	}
	return tools.ExecutionResult{Result: fmt.Sprintf("Sticker sent successfully to %s", to)}, nil
}

type mediaKind int

const (
	kindUnknown mediaKind = iota
	kindImage
	kindVideo
	kindAudio
	kindDocument
	kindSticker
)

func mediaKindFor(t media.MediaType) mediaKind {
	switch t {
	case media.MediaTypeProductImage:
		return kindImage
	case media.MediaTypeProductVideo, media.MediaTypeVslVideo:
		return kindVideo
	case media.MediaTypeAudio:
		return kindAudio
	case media.MediaTypeDocument, media.MediaTypeDocumentPdf, media.MediaTypeDocumentDoc:
		return kindDocument
	case media.MediaTypeSticker:
		return kindSticker
	default:
		return kindUnknown
	}
}

func prettyTypeLabel(t media.MediaType) string {
	switch mediaKindFor(t) {
	case kindImage:
		return "imagem"
	case kindVideo:
		return "vídeo"
	case kindAudio:
		return "áudio"
	case kindDocument:
		return "documento"
	case kindSticker:
		return "sticker"
	default:
		return string(t)
	}
}

func fetchMedia(rawURL string, maxBytes int) ([]byte, string, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid media URL: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("failed to download media: status=%d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, int64(maxBytes)+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read media: %w", err)
	}
	if len(data) > maxBytes {
		return nil, "", "", fmt.Errorf("media size exceeds %d bytes limit", maxBytes)
	}

	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}

	mimeType = normalizeMimeFromURL(rawURL, mimeType)
	fileName := deriveFileName(rawURL, mimeType)
	return data, mimeType, fileName, nil
}

func deriveFileName(rawURL, mimeType string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil {
		base := strings.TrimSpace(path.Base(parsed.Path))
		if base != "" && base != "." && base != "/" {
			ext := path.Ext(base)
			if strings.TrimSpace(ext) == "" {
				return base + guessExt(mimeType)
			}
			return base
		}
	}
	return "media" + guessExt(mimeType)
}

func deriveDocumentFilename(m *media.Media, mimeType string) string {
	if m == nil {
		return "document"
	}
	if name := strings.TrimSpace(m.Description); name != "" {
		if path.Ext(name) == "" {
			return name + guessExt(mimeType)
		}
		return name
	}
	return deriveFileName(m.URL, mimeType)
}

func guessExt(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.Contains(mimeType, "png"):
		return ".png"
	case strings.Contains(mimeType, "webp"):
		return ".webp"
	case strings.Contains(mimeType, "jpeg"), strings.Contains(mimeType, "jpg"):
		return ".jpg"
	case strings.Contains(mimeType, "mp4"):
		return ".mp4"
	case strings.Contains(mimeType, "3gpp"):
		return ".3gp"
	case strings.Contains(mimeType, "mpeg"):
		return ".mp3"
	case strings.Contains(mimeType, "ogg"):
		return ".ogg"
	case strings.Contains(mimeType, "amr"):
		return ".amr"
	case strings.Contains(mimeType, "aac"):
		return ".aac"
	case strings.Contains(mimeType, "pdf"):
		return ".pdf"
	case strings.Contains(mimeType, "msword"):
		return ".doc"
	case strings.Contains(mimeType, "officedocument.wordprocessingml"):
		return ".docx"
	case strings.Contains(mimeType, "vnd.ms-excel"):
		return ".xls"
	case strings.Contains(mimeType, "spreadsheetml"):
		return ".xlsx"
	default:
		return ""
	}
}

func normalizeMimeFromURL(rawURL, mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	ext := strings.ToLower(strings.TrimSpace(path.Ext(rawURL)))

	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return "image/jpeg"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".3gp":
		return "video/3gpp"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".aac":
		return "audio/aac"
	case ".amr":
		return "audio/amr"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}

	if mimeType != "" {
		return mimeType
	}
	return "application/octet-stream"
}

func normalizeImageForWhatsApp(data []byte, mimeType string) ([]byte, string, string, error) {
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to decode image: %w", err)
	}
	bounds := src.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, src, bounds.Min, draw.Src)

	var buf bytes.Buffer
	var outputMime, outputExt string

	if format == "png" || len(data) < 100*1024 {
		if err := png.Encode(&buf, rgba); err != nil {
			return nil, "", "", fmt.Errorf("failed to encode to PNG: %w", err)
		}
		outputMime, outputExt = "image/png", ".png"
	} else {
		if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", "", fmt.Errorf("failed to encode to JPEG: %w", err)
		}
		outputMime, outputExt = "image/jpeg", ".jpg"
	}

	_ = mimeType
	return buf.Bytes(), outputMime, outputExt, nil
}
