package unofficial_whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type channelAdapter struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	messaging     uw.MessagingAPI

	caps   channel.Capabilities
	voice  uw.VoiceTranscoder
	jitter func(minMS, maxMS int) int
}

func NewChannelAdapter(
	instances uw.InstanceRepository,
	servers uw.ServerRepository,
	contacts uw.ContactRepository,
	conversations uw.ConversationRepository,
	messaging uw.MessagingAPI,
) conversation.ChannelAdapter {
	return &channelAdapter{
		instances:     instances,
		servers:       servers,
		contacts:      contacts,
		conversations: conversations,
		messaging:     messaging,
		caps:          uw.Descriptor().Capabilities,
		jitter:        defaultJitter,
	}
}

func (a *channelAdapter) SetVoiceTranscoder(v uw.VoiceTranscoder) { a.voice = v }

var (
	_ conversation.ChannelAdapter     = (*channelAdapter)(nil)
	_ conversation.ReactingAdapter    = (*channelAdapter)(nil)
	_ conversation.PresenceAdapter    = (*channelAdapter)(nil)
	_ conversation.EditingAdapter     = (*channelAdapter)(nil)
	_ conversation.RetractingAdapter  = (*channelAdapter)(nil)
	_ conversation.InteractiveAdapter = (*channelAdapter)(nil)
)

func (a *channelAdapter) EntryType() shared.EntryType {
	return shared.EntryTypeUnofficialWhatsApp
}

func (a *channelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	instance, err := a.instances.FindByID(ctx, conv.InstanceID)
	if err != nil {
		return nil, err
	}
	contact, err := a.contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}

	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeUnofficialWhatsApp,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     instance.ID,
		ContactID:     contact.ID,
		ContactRef:    conv.ChatID,
		ContactHandle: contact.Handle(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *channelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	instance, err := a.instances.FindByID(ctx, ec.AccountID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}

	if !instance.SessionLive() {
		return conversation.ClosedWindow(conversation.WindowReasonSessionDown), nil
	}
	if instance.Restriction.Active(time.Now().UTC()) {
		return conversation.ClosedWindowUntil(conversation.WindowReasonAccountRestricted, instance.Restriction.Until), nil
	}

	contact, err := a.contacts.FindByID(ctx, ec.ContactID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	if contact.Blocked {
		return conversation.ClosedWindow(conversation.WindowReasonContactBlocked), nil
	}
	return conversation.OpenWindow(nil), nil
}

func (a *channelAdapter) SendText(
	ctx context.Context,
	ec *conversation.EntryContext,
	req conversation.SendTextRequest,
) (*conversation.SendOutcome, error) {
	instance, ref, err := a.prepareSend(ctx, ec, req.Body)
	if err != nil {
		return nil, err
	}

	result, err := a.messaging.SendText(ctx, ref, uw.SendTextInput{
		ChatID:                   ec.ContactRef,
		Text:                     uw.SanitizeOutboundText(req.Body),
		ReplyToProviderMessageID: req.ReplyToProviderMessageID,
		DelayMS:                  a.pacingDelay(instance, req.HumanInitiated),
		TrackSource:              uw.TrackSource,
		TrackID:                  ec.EntryID,
	})
	return a.outcome(ctx, instance, ec, result, err)
}

func (a *channelAdapter) SendMedia(
	ctx context.Context,
	ec *conversation.EntryContext,
	req conversation.SendMediaRequest,
) (*conversation.SendOutcome, error) {
	instance, ref, err := a.prepareSend(ctx, ec, req.Caption)
	if err != nil {
		return nil, err
	}
	if req.URL == "" && len(req.Bytes) == 0 {
		return nil, conversation.ErrCapabilityUnsupported
	}

	kind := mediaKindFromRequest(req)

	if kind == uw.MediaVoice || kind == uw.MediaAudio {
		if a.voice == nil {
			return nil, fmt.Errorf("unofficial whatsapp: audio cannot be sent, no transcoder is configured")
		}
		source := req.URL
		if source == "" {
			return nil, conversation.ErrCapabilityUnsupported
		}
		converted, err := a.voice.ToVoiceNote(ctx, source)
		if err != nil {
			return nil, err
		}
		req.URL = ""
		req.Bytes = converted
		req.MIMEType = "audio/ogg"
		req.FileName = "voice.ogg"
		kind = uw.MediaVoice
	}

	if limit, ok := a.caps.MediaLimits[channelMediaKind(kind)]; ok {
		if req.MIMEType != "" && !limit.Allows(req.MIMEType) {
			return nil, fmt.Errorf("unofficial whatsapp: %s is not accepted for %s media", req.MIMEType, kind)
		}
		if limit.MaxBytes > 0 && int64(len(req.Bytes)) > limit.MaxBytes {
			return nil, fmt.Errorf("unofficial whatsapp: attachment exceeds the %d byte limit", limit.MaxBytes)
		}
	}

	in := uw.SendMediaInput{
		ChatID:                   ec.ContactRef,
		Kind:                     kind,
		URL:                      req.URL,
		MIMEType:                 req.MIMEType,
		FileName:                 req.FileName,
		Caption:                  uw.SanitizeOutboundText(req.Caption),
		ReplyToProviderMessageID: req.ReplyToProviderMessageID,
		DelayMS:                  a.pacingDelay(instance, req.HumanInitiated),
		TrackSource:              uw.TrackSource,
		TrackID:                  ec.EntryID,
	}
	if in.URL == "" {
		in.Base64 = encodeBase64(req.Bytes)
	}

	result, err := a.messaging.SendMedia(ctx, ref, in)
	return a.outcome(ctx, instance, ec, result, err)
}

func (a *channelAdapter) SendInteractive(
	ctx context.Context,
	ec *conversation.EntryContext,
	req conversation.SendInteractiveRequest,
) (*conversation.SendOutcome, error) {
	instance, ref, err := a.prepareSend(ctx, ec, req.Body)
	if err != nil {
		return nil, err
	}
	if len(req.Options) == 0 {
		return nil, conversation.ErrCapabilityUnsupported
	}

	style := req.Style
	if style != uw.InteractiveStyleList {
		style = uw.InteractiveStyleButtons
	}
	if len(req.Options) > uw.MaxButtonOptions {
		style = uw.InteractiveStyleList
	}

	options := make([]uw.InteractiveOption, 0, len(req.Options))
	for i, opt := range req.Options {
		if i >= uw.MaxOptionsFor(style) {
			break
		}
		options = append(options, uw.InteractiveOption{ID: opt.ID, Title: opt.Title})
	}

	body := req.Body
	if req.Header != "" {
		body = req.Header + "\n\n" + body
	}

	result, err := a.messaging.SendMenu(ctx, ref, uw.SendMenuInput{
		ChatID:      ec.ContactRef,
		Style:       style,
		Body:        uw.SanitizeOutboundText(body),
		Footer:      uw.SanitizeOutboundText(req.Footer),
		Button:      "Ver opções",
		Options:     options,
		DelayMS:     a.pacingDelay(instance, false),
		TrackSource: uw.TrackSource,
		TrackID:     ec.EntryID,
	})
	return a.outcome(ctx, instance, ec, result, err)
}

func (a *channelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

func (a *channelAdapter) SendReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID, reaction string) error {
	_, ref, err := a.prepareSend(ctx, ec, "")
	if err != nil {
		return err
	}
	return a.messaging.React(ctx, ref, ec.ContactRef, targetProviderMessageID, reaction)
}

func (a *channelAdapter) RemoveReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID string) error {
	return a.SendReaction(ctx, ec, targetProviderMessageID, "")
}

func (a *channelAdapter) SendTyping(ctx context.Context, ec *conversation.EntryContext, on bool) error {
	_, ref, err := a.prepareSend(ctx, ec, "")
	if err != nil {
		return err
	}
	presence, delay := uw.PresenceTyping, typingHoldMS
	if !on {
		presence, delay = uw.PresencePaused, 0
	}
	return a.messaging.SendPresence(ctx, ref, ec.ContactRef, presence, delay)
}

func (a *channelAdapter) MarkSeen(ctx context.Context, ec *conversation.EntryContext, upToProviderMessageID string) error {
	if upToProviderMessageID == "" {
		return nil
	}
	_, ref, err := a.prepareSend(ctx, ec, "")
	if err != nil {
		return err
	}
	return a.messaging.MarkRead(ctx, ref, []string{upToProviderMessageID})
}

func (a *channelAdapter) EditText(ctx context.Context, ec *conversation.EntryContext, providerMessageID, body string) error {
	_, ref, err := a.prepareSend(ctx, ec, body)
	if err != nil {
		return err
	}
	_, err = a.messaging.EditMessage(ctx, ref, providerMessageID, uw.SanitizeOutboundText(body))
	return err
}

func (a *channelAdapter) Retract(ctx context.Context, ec *conversation.EntryContext, providerMessageID string, _ time.Time) error {
	_, ref, err := a.prepareSend(ctx, ec, "")
	if err != nil {
		return err
	}
	return a.messaging.DeleteMessage(ctx, ref, providerMessageID)
}

const typingHoldMS = 20000

func (a *channelAdapter) prepareSend(
	ctx context.Context,
	ec *conversation.EntryContext,
	body string,
) (*uw.Instance, uw.InstanceRef, error) {
	if ec == nil {
		return nil, uw.InstanceRef{}, conversation.ErrNoAdapterForEntryType
	}
	if uw.TextTooLong(body) {
		return nil, uw.InstanceRef{}, uw.ErrTextTooLong
	}

	instance, err := a.instances.FindByID(ctx, ec.AccountID)
	if err != nil {
		return nil, uw.InstanceRef{}, err
	}
	if _, err := instance.CanSend(time.Now().UTC()); err != nil {
		return nil, uw.InstanceRef{}, err
	}

	server, err := a.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, uw.InstanceRef{}, err
	}
	return instance, uw.RefFor(server, instance), nil
}

func (a *channelAdapter) outcome(
	ctx context.Context,
	instance *uw.Instance,
	ec *conversation.EntryContext,
	result *uw.SendResult,
	err error,
) (*conversation.SendOutcome, error) {
	if err != nil {
		a.recordSendFailure(ctx, instance, ec, err)
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("unofficial whatsapp: provider acknowledged the send without a message id")
	}
	return &conversation.SendOutcome{ProviderMessageID: result.ProviderMessageID}, nil
}

func (a *channelAdapter) recordSendFailure(ctx context.Context, instance *uw.Instance, ec *conversation.EntryContext, err error) {
	provErr, ok := uw.AsProviderError(err)
	if !ok {
		return
	}

	if provErr.IsRestriction() && provErr.Restriction != nil {
		if updateErr := a.instances.UpdateRestriction(ctx, instance.ID, *provErr.Restriction); updateErr == nil {
			instance.Restriction = *provErr.Restriction
		}
		return
	}
	if provErr.NeedsReconnect() && instance.Status.CanTransitionTo(uw.StatusDisconnected) {
		_ = a.instances.UpdateStatus(ctx, instance.ID, uw.StatusDisconnected,
			"the host rejected a send: the session is no longer valid")
	}
}

func (a *channelAdapter) pacingDelay(instance *uw.Instance, humanInitiated bool) int {
	if humanInitiated {
		return 0
	}
	minMS, maxMS := instance.SendDelayRange()
	return a.jitter(minMS, maxMS)
}

func defaultJitter(minMS, maxMS int) int {
	if maxMS <= minMS {
		return minMS
	}
	return minMS + rand.Intn(maxMS-minMS+1)
}

func mediaKindFromRequest(req conversation.SendMediaRequest) uw.MediaKind {
	switch req.Kind {
	case "image":
		return uw.MediaImage
	case "video":
		return uw.MediaVideo
	case "audio":
		return uw.MediaVoice
	default:
		return uw.MediaDocument
	}
}

func channelMediaKind(kind uw.MediaKind) channel.MediaKind {
	switch kind {
	case uw.MediaImage:
		return channel.MediaImage
	case uw.MediaVideo:
		return channel.MediaVideo
	case uw.MediaAudio, uw.MediaVoice:
		return channel.MediaAudio
	default:
		return channel.MediaDocument
	}
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
