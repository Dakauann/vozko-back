package workflow_usecase

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const (
	simContactNumber            = "5511999990000"
	maxSandboxEffectsPerStep    = 100
	simUploadedMediaPrefix      = "sim-media-"
	simChannelMessagePrefix     = "sim-"
	simChannelMessageTypeText   = "text"
	simChannelMessageTypeMedia  = "media"
	simChannelMessageTypeAudio  = "audio"
	simChannelMessageTypeButton = "button"
	simChannelMessageTypeList   = "list"
)

var _ conversation.MessageHistoryManager = (*simChannel)(nil)

type simChannelSend struct {
	msgType     string
	text        string
	summary     string
	extra       interface{}
	audioBase64 string
	audioMime   string
	mediaType   conversation.MediaType
	media       *conversation.AttachedMedia
}

type simUpload struct {
	filename string
	mime     string
	size     int64
}

type simEffects struct {
	mu       sync.Mutex
	messages []conversation.Message
	changes  []workflow.SimulatedChange
	channels []*simChannel
}

func newSimEffects() *simEffects {
	return &simEffects{}
}

func (e *simEffects) bind(ch *simChannel) {
	e.mu.Lock()
	e.channels = append(e.channels, ch)
	e.mu.Unlock()
}

func (e *simEffects) message(m conversation.Message) {
	e.mu.Lock()
	if len(e.messages) < maxSandboxEffectsPerStep {
		e.messages = append(e.messages, m)
	}
	e.mu.Unlock()
}

func (e *simEffects) change(c workflow.SimulatedChange) {
	e.mu.Lock()
	if len(e.changes) < maxSandboxEffectsPerStep {
		e.changes = append(e.changes, c)
	}
	e.mu.Unlock()
}

func (e *simEffects) drain() workflow.SandboxEffects {
	if e == nil {
		return workflow.SandboxEffects{}
	}
	e.mu.Lock()
	channels := append([]*simChannel(nil), e.channels...)
	e.mu.Unlock()
	for _, ch := range channels {
		ch.flush()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := workflow.SandboxEffects{Messages: e.messages, Changes: e.changes}
	e.messages, e.changes = nil, nil
	return out
}

type simChannel struct {
	outbound func(SimOutboundMessage)
	effects  *simEffects

	mu      sync.Mutex
	pending map[string]simChannelSend
	order   []string
	uploads map[string]simUpload
}

func newSimChannel(outbound func(SimOutboundMessage), effects *simEffects) *simChannel {
	if effects == nil {
		effects = newSimEffects()
	}
	ch := &simChannel{outbound: outbound, effects: effects, pending: map[string]simChannelSend{}, uploads: map[string]simUpload{}}
	effects.bind(ch)
	return ch
}

func (c *simChannel) sent(send simChannelSend) string {
	id := simChannelMessagePrefix + uuid.New().String()
	c.mu.Lock()
	c.pending[id] = send
	c.order = append(c.order, id)
	c.mu.Unlock()
	return id
}

func (c *simChannel) upload(data []byte, filename, mime string) string {
	id := simUploadedMediaPrefix + uuid.New().String()
	c.mu.Lock()
	c.uploads[id] = simUpload{filename: filename, mime: mime, size: int64(len(data))}
	c.mu.Unlock()
	return id
}

func (c *simChannel) uploaded(id string) (simUpload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	up, ok := c.uploads[id]
	return up, ok
}

func (c *simChannel) attached(link, uploadID, filename string) *conversation.AttachedMedia {
	media := &conversation.AttachedMedia{URL: link, Filename: filename}
	if up, ok := c.uploaded(uploadID); ok {
		media.MimeType, media.Size = up.mime, up.size
		if media.Filename == "" {
			media.Filename = up.filename
		}
	}
	return media
}

func (c *simChannel) take(id string) (simChannelSend, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	send, ok := c.pending[id]
	if !ok {
		return simChannelSend{}, false
	}
	delete(c.pending, id)
	for i, pendingID := range c.order {
		if pendingID == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	return send, true
}

func (c *simChannel) Record(_ context.Context, record conversation.MessageHistoryRecord) error {
	send, _ := c.take(record.MessageID)
	c.deliver(record, send)
	return nil
}

func (c *simChannel) flush() {
	c.mu.Lock()
	ids := c.order
	sends := make([]simChannelSend, 0, len(ids))
	for _, id := range ids {
		sends = append(sends, c.pending[id])
	}
	c.pending, c.order = map[string]simChannelSend{}, nil
	c.mu.Unlock()
	for i, id := range ids {
		c.deliver(conversation.MessageHistoryRecord{
			SentBy: conversation.SentBySystem(), EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
			MessageType: conversation.MessageTypeAIResponse, MessageID: id, To: simContactNumber, Text: sends[i].text,
		}, sends[i])
	}
}

func (c *simChannel) deliver(record conversation.MessageHistoryRecord, send simChannelSend) {
	message := simWireMessage(record, send)
	c.effects.message(message)
	if c.outbound == nil {
		return
	}
	msgType, summary := send.msgType, send.summary
	if msgType == "" {
		msgType = string(message.MessageType)
	}
	if summary == "" {
		summary = message.Text
	}
	c.outbound(SimOutboundMessage{
		Type: msgType, Text: summary, To: record.To, MessageID: record.MessageID, Extra: send.extra,
		AudioBase64: send.audioBase64, AudioMime: send.audioMime, Message: &message,
	})
}

func sandboxStep(row *workflow.WorkflowRunLog, effects workflow.SandboxEffects, pin *workflow.NodePin) *workflow.WorkflowRunLog {
	step := workflow.WithSandboxEffects(row, effects)
	notice := pin.WithheldNotice()
	if step == nil || notice == "" || step.Pinned {
		return step
	}
	if step == row {
		cp := *row
		step = &cp
	}
	step.Notice = notice
	return step
}

func simWireMessage(record conversation.MessageHistoryRecord, send simChannelSend) conversation.Message {
	message := record.Message()
	message.ID = record.MessageID
	message.DeliveryStatus = conversation.DeliveryStatusSent
	if send.media != nil {
		media := *send.media
		if media.URL == "" {
			media.URL = record.MediaURL
		}
		message.MediaType = send.mediaType
		message.Media = &media
	}
	message.Normalize()
	return *message
}
