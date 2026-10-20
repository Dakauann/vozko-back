package workflow_usecase

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/geo"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const (
	simReplyText      = "reply"
	simReplySelection = "reply_selection"
	simReplyLocation  = "reply_location"
	simReplyMedia     = "reply_media"

	simBusinessPhone = "sim-business-phone"
)

var (
	errSimReplyEmpty     = errors.New("a resposta precisa de texto")
	errSimSelection      = errors.New("a escolha precisa de kind (button ou list), id e title")
	errSimLocation       = errors.New("a localização precisa de latitude e longitude válidas")
	errSimMedia          = errors.New("a mídia precisa de kind (image, video, audio, document ou sticker) e url")
	errSimReplyMalformed = errors.New("dados da resposta inválidos")
)

type simSelectionData struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

type simMediaData struct {
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
	Caption  string `json:"caption"`
}

type simInbound struct {
	data     map[string]interface{}
	text     string
	kind     conversation.MessageType
	media    *conversation.AttachedMedia
	mediaFor conversation.MediaType
	metadata json.RawMessage
	typed    bool
}

func isSimReply(kind string) bool {
	switch kind {
	case simReplyText, simReplySelection, simReplyLocation, simReplyMedia:
		return true
	}
	return false
}

func parseSimInbound(msg simClientMsg) (simInbound, error) {
	switch msg.Type {
	case simReplyText:
		var d replyData
		if json.Unmarshal(msg.Data, &d) != nil {
			return simInbound{}, errSimReplyMalformed
		}
		if strings.TrimSpace(d.Text) == "" {
			return simInbound{}, errSimReplyEmpty
		}
		return textInbound(d.Text), nil
	case simReplySelection:
		var d simSelectionData
		if json.Unmarshal(msg.Data, &d) != nil {
			return simInbound{}, errSimReplyMalformed
		}
		if (d.Kind != "button" && d.Kind != "list") || strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Title) == "" {
			return simInbound{}, errSimSelection
		}
		selection := &workflow.OptionSelection{ID: d.ID, Title: d.Title, Kind: d.Kind}
		return simInbound{
			data: workflow.InboundMessageData(d.Title, "text", "", selection),
			text: d.Title, kind: conversation.MessageTypeUserMessage,
		}, nil
	case simReplyLocation:
		var d conversation.WhatsAppLocation
		if json.Unmarshal(msg.Data, &d) != nil {
			return simInbound{}, errSimReplyMalformed
		}
		if (geo.Point{Lat: d.Latitude, Lng: d.Longitude}).Validate() != nil {
			return simInbound{}, errSimLocation
		}
		text := d.Text()
		return simInbound{
			data: workflow.InboundMessageData(text, "text", "", nil),
			text: text, kind: conversation.MessageTypeUserMessage, metadata: d.Metadata(),
		}, nil
	case simReplyMedia:
		var d simMediaData
		if json.Unmarshal(msg.Data, &d) != nil {
			return simInbound{}, errSimReplyMalformed
		}
		return mediaInbound(d)
	}
	return simInbound{}, errSimReplyMalformed
}

func textInbound(text string) simInbound {
	return simInbound{data: workflow.InboundMessageData(text, "text", "", nil), text: text, kind: conversation.MessageTypeUserMessage, typed: true}
}

func mediaInbound(d simMediaData) (simInbound, error) {
	kind := conversation.MediaType(strings.TrimSpace(d.Kind))
	if !kind.Valid() || strings.TrimSpace(d.URL) == "" {
		return simInbound{}, errSimMedia
	}
	text, messageType, recorded := d.Caption, "media", conversation.MessageTypeMedia
	switch kind {
	case conversation.MediaTypeDocument:
		text = d.Filename
	case conversation.MediaTypeSticker:
		text = ""
	case conversation.MediaTypeAudio:
		messageType, recorded = "audio", conversation.MessageTypeAudio
	}
	return simInbound{
		data: workflow.InboundMessageData(text, messageType, string(kind), nil),
		text: text, kind: recorded, mediaFor: kind,
		media: &conversation.AttachedMedia{URL: strings.TrimSpace(d.URL), MimeType: d.Mime, Filename: d.Filename},
	}, nil
}

func (in simInbound) replyData(waitNode *workflow.Node) map[string]interface{} {
	data := make(map[string]interface{}, len(in.data)+3)
	for k, v := range in.data {
		data[k] = v
	}
	if in.typed && waitNode != nil && waitNode.Type.IsInteractivePrompt() {
		data[workflow.DataKeySelectedOptionID] = in.text
		data[workflow.DataKeySelectedOptionTitle] = in.text
		data[workflow.DataKeySelectedOptionKind] = "button"
	}
	return data
}

func (in simInbound) message(entryID string) conversation.Message {
	now := time.Now().UTC()
	return conversation.Message{
		ID: "sim-msg-" + uuid.New().String(), EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		MessageType: in.kind, Direction: conversation.MessageDirectionInbound, SentBy: conversation.SentByContact(simContactNumber),
		From: simContactNumber, To: simBusinessPhone, Text: in.text, MediaType: in.mediaFor, Media: in.media, Metadata: in.metadata,
		CreatedAt: now, UpdatedAt: now,
	}
}
