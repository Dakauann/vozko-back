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

// channelAdapter is this channel's implementation of conversation.ChannelAdapter.
//
// It is what makes a reply leave from the SAME number the message arrived on:
// ResolveEntry walks entry → conversation → instance, and every send uses that
// instance's own credential. With several numbers connected to one workspace,
// nothing else keeps them apart.
//
// It implements every optional capability interface the conversation layer
// defines — reacting, presence, editing, retracting and interactive — which no
// previous channel does. That is not ambition: a linked-device session genuinely
// supports all five, and declaring less would hide features the customer's own
// WhatsApp already has.
type channelAdapter struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	messaging     uw.MessagingAPI

	caps channel.Capabilities
	// voice converts an audio attachment into the ogg/opus a WhatsApp voice note
	// must be. Optional: without it, audio sends fail with a named reason rather
	// than the whole channel refusing to start.
	voice uw.VoiceTranscoder
	// jitter picks the human-pacing delay. Injectable so a test can assert the
	// range without depending on randomness.
	jitter func(minMS, maxMS int) int
}

// NewChannelAdapter builds the send adapter.
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

// SetVoiceTranscoder wires audio conversion.
//
// Separate from the constructor because it is genuinely optional and because it
// is the only dependency that shells out to a binary: a deployment without
// ffmpeg keeps every other media kind working.
func (a *channelAdapter) SetVoiceTranscoder(v uw.VoiceTranscoder) { a.voice = v }

// Compile-time proof of every capability this channel claims. A method that
// drifts out of shape fails here rather than at wiring time, where a failed type
// assertion silently means "the channel cannot do this".
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

// ResolveEntry loads the instance and contact behind an entry id.
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
		EntryID:     conv.ID,
		EntryType:   shared.EntryTypeUnofficialWhatsApp,
		WorkspaceID: conv.WorkspaceID,
		AccountID:   instance.ID,
		ContactID:   contact.ID,
		// The CHAT id, not the contact's JID: they are the same in a private
		// chat but diverge for a group, and addressing a group by a
		// participant's JID would send the reply to the wrong place.
		ContactRef:    conv.ChatID,
		ContactHandle: contact.Handle(),
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

// WindowState reports whether an outbound message is allowed right now.
//
// There is NO clock on this channel — no template, no 24h rule — which is both
// its selling point and the reason this function matters more than its
// equivalents elsewhere. Three distinct things close the composer, and each needs
// different words and a different remedy in the UI, so they are distinguished
// here rather than collapsed into a bare false:
//
//   - the session is down       → "reconnect"        (no expiry to show)
//   - WhatsApp restricted us    → "wait until …"     (an expiry to show)
//   - the contact blocked us    → nothing to be done (no expiry)
//
// Consulted by BOTH the send path and the composer's UI state, so this is the
// single definition of "can I reply right now".
func (a *channelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	instance, err := a.instances.FindByID(ctx, ec.AccountID)
	if err != nil {
		// Most often a number that was REMOVED: the row is soft-deleted, so the
		// lookup misses. Naming it beats a bare refusal that reads like a clock.
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}

	if !instance.SessionLive() {
		return conversation.ClosedWindow(conversation.WindowReasonSessionDown), nil
	}
	if instance.Restriction.Active(time.Now().UTC()) {
		// The only case with a time attached, and it is a countdown to when the
		// restriction LIFTS — never a deadline to schedule against.
		return conversation.ClosedWindowUntil(conversation.WindowReasonAccountRestricted, instance.Restriction.Until), nil
	}

	contact, err := a.contacts.FindByID(ctx, ec.ContactID)
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	if contact.Blocked {
		return conversation.ClosedWindow(conversation.WindowReasonContactBlocked), nil
	}
	// No clock on this channel: no template, no 24h rule. Groups included.
	return conversation.OpenWindow(nil), nil
}

// SendText delivers a text message.
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
		ChatID: ec.ContactRef,
		// Neutralised, never trusted: the provider substitutes {{...}} from ITS
		// own lead store, which is not ours, so an operator typing {{name}} or
		// an agent emitting a stray brace would silently leak another record's
		// data into a customer's chat.
		Text:                     uw.SanitizeOutboundText(req.Body),
		ReplyToProviderMessageID: req.ReplyToProviderMessageID,
		DelayMS:                  a.pacingDelay(instance, req.HumanInitiated),
		TrackSource:              uw.TrackSource,
		TrackID:                  ec.EntryID,
	})
	return a.outcome(ctx, instance, ec, result, err)
}

// SendMedia delivers an attachment.
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

	// Audio is CONVERTED, not validated and refused.
	//
	// WhatsApp voice notes are ogg/opus, and the CRM's recorder produces WAV —
	// it records opus in the browser then transcodes so the waveform and
	// playback work everywhere. Checking the incoming MIME against this
	// channel's accepted list therefore rejected every voice note the product
	// can actually produce ("audio/wav is not accepted for audio media"). The
	// official WhatsApp path has always converted here; this one now does too,
	// through the same encoder.
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
		// Rewritten to what will actually go on the wire, so the limit check
		// below and the provider both see the same file.
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
	// A URL is preferred: the provider fetches it directly, so the bytes do not
	// travel through our process a second time.
	if in.URL == "" {
		in.Base64 = encodeBase64(req.Bytes)
	}

	result, err := a.messaging.SendMedia(ctx, ref, in)
	return a.outcome(ctx, instance, ec, result, err)
}

// SendInteractive delivers a "pick one" prompt.
//
// The adapter applies WhatsApp's own caps rather than trusting the caller: an
// author's option list is not bounded by anything upstream, and a list of twelve
// sent as three buttons is silently truncated by the provider with no error.
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
	// More options than the button style can render means a list is the only
	// honest way to show them all.
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

	// Header and footer are best-effort everywhere; here the header folds into
	// the body because WhatsApp's interactive header is a separate slot the
	// menu endpoint does not expose.
	body := req.Body
	if req.Header != "" {
		body = req.Header + "\n\n" + body
	}

	result, err := a.messaging.SendMenu(ctx, ref, uw.SendMenuInput{
		ChatID:  ec.ContactRef,
		Style:   style,
		Body:    uw.SanitizeOutboundText(body),
		Footer:  uw.SanitizeOutboundText(req.Footer),
		Button:  "Ver opções",
		Options: options,
		// Always paced: an interactive prompt only ever originates from a
		// workflow node or an agent tool, never from an operator typing.
		DelayMS:     a.pacingDelay(instance, false),
		TrackSource: uw.TrackSource,
		TrackID:     ec.EntryID,
	})
	return a.outcome(ctx, instance, ec, result, err)
}

// InteractiveLimits reports what this channel will actually render, so the
// workflow editor can warn an author before they publish rather than after.
func (a *channelAdapter) InteractiveLimits() channel.InteractiveLimits {
	return a.caps.Interactive
}

// SendReaction attaches an emoji to a message.
func (a *channelAdapter) SendReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID, reaction string) error {
	_, ref, err := a.prepareSend(ctx, ec, "")
	if err != nil {
		return err
	}
	return a.messaging.React(ctx, ref, ec.ContactRef, targetProviderMessageID, reaction)
}

// RemoveReaction clears ours. An empty emoji is the provider's documented
// removal, which is why this is the same call with a different argument.
func (a *channelAdapter) RemoveReaction(ctx context.Context, ec *conversation.EntryContext, targetProviderMessageID string) error {
	return a.SendReaction(ctx, ec, targetProviderMessageID, "")
}

// SendTyping shows or clears the typing indicator.
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

// MarkSeen marks the contact's messages read.
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

// EditText corrects an already-sent message.
//
// Only Telegram could do this before. An operator who sent the wrong boleto to
// the wrong debtor can now fix it rather than sending a correction the customer
// may never read.
func (a *channelAdapter) EditText(ctx context.Context, ec *conversation.EntryContext, providerMessageID, body string) error {
	_, ref, err := a.prepareSend(ctx, ec, body)
	if err != nil {
		return err
	}
	_, err = a.messaging.EditMessage(ctx, ref, providerMessageID, uw.SanitizeOutboundText(body))
	return err
}

// Retract deletes a sent message for everyone.
//
// sentAt is accepted for the interface's sake and deliberately unused: WhatsApp
// enforces its own deletion window server-side and its exact bound has moved
// more than once, so refusing locally against a number we invented would block
// deletions the platform would have accepted.
func (a *channelAdapter) Retract(ctx context.Context, ec *conversation.EntryContext, providerMessageID string, _ time.Time) error {
	_, ref, err := a.prepareSend(ctx, ec, "")
	if err != nil {
		return err
	}
	return a.messaging.DeleteMessage(ctx, ref, providerMessageID)
}

// ---------------------------------------------------------------- internals

// typingHoldMS is how long a typing indicator persists without renewal.
//
// Chosen to outlast a normal AI generation without approaching the provider's
// five-minute ceiling: an indicator that expires mid-thought looks like the
// business walked away.
const typingHoldMS = 20000

// prepareSend resolves the instance and host, and enforces everything that must
// be true before a byte leaves.
//
// Centralised because every send path needs the identical checks, and a path
// that skipped the restriction gate would keep blasting a number WhatsApp has
// already begun limiting — the behaviour that turns a warning into a ban.
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

// outcome normalizes a send result and reacts to a provider refusal.
//
// A WhatsApp restriction is CACHED on the instance the moment it is seen, so
// every later send — including a running broadcast — is refused by WindowState
// before it reaches the provider. Learning the same lesson once per message is
// how a limited number becomes a banned one.
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
	// The provider id is written as external_message_id BEFORE the echo webhook
	// can arrive, which is what makes the echo reconcile instead of inserting a
	// duplicate.
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

// pacingDelay picks a jittered human delay for one AUTOMATED send.
//
// Randomised inside the instance's configured range rather than fixed: a
// constant cadence is the most legible automation signature there is, and on
// this channel looking automated costs the customer their number.
//
// An operator's own message is exempt, and that exemption is the point. The
// delay is not a local wait — it is handed to the host, which renders
// "Digitando…" for its full duration BEFORE dispatching. Applied to a human
// send it bought nothing (a person typing in the CRM is already human-paced)
// and cost 3-12 seconds of an operator staring at a message that had not left
// yet. Ban protection is about the cadence of machine traffic; a human reply is
// the traffic it is trying to imitate.
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

// mediaKindFromRequest maps the channel-neutral kind onto ours, defaulting to a
// document: an unrecognised kind is still deliverable as a file, which is much
// better than refusing to send it at all.
func mediaKindFromRequest(req conversation.SendMediaRequest) uw.MediaKind {
	switch req.Kind {
	case "image":
		return uw.MediaImage
	case "video":
		return uw.MediaVideo
	case "audio":
		// Always a voice note. Every audio this product sends is either a
		// recording an operator made or one an agent generated, and both are
		// voice — and the send path converts to opus regardless, so the old
		// "music-player attachment" branch only ever produced a worse-looking
		// bubble for the same bytes.
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
