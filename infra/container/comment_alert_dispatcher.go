package container

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ca "vozko/domain/comment_analysis"
	conversation_domain "vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	template_domain "vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

// Sending an alert, by composing the two outbound paths that already exist.
//
// It lives in the composition root for the same reason every other bridge here
// does: comment analysis must not import either WhatsApp channel, and this is
// the only place allowed to know all three. There is NO new send path in this
// file. The official half calls the billed outreach use case, the unofficial
// half opens the conversation and hands it to the same composer an operator
// types into, and both keep their own scope checks, mirrors and accounting.
//
// The parts that are specific to sending without a human in the loop:
//
//   - The idempotency key is the domain's, derived from the rule and the firing
//     instant. A retry after a timeout replays rather than charging twice, and
//     `Replayed` comes back true.
//   - A provider error is NEVER retried here. We cannot tell a refused send
//     from one that arrived just before the connection dropped, and a duplicate
//     alert at 3am is unrecoverable while a visible failure costs one click.
//   - ErrWindowAlreadyOpen is a SUCCESS route, not a failure: the recipient
//     already has an open conversation, so the alert goes down it for free
//     instead of buying a template.
type commentAlertDispatcher struct {
	official   wo.StartOfficialConversationUseCase
	unofficial *uwuc.StartConversationUseCase
	send       conversation_domain.OperatorSendUseCase
	// templates resolves the chosen template's own parameter shape. An
	// approved template belongs to the CUSTOMER: it may declare no variables,
	// two, or eight, and it may name them or number them. Sending a fixed four
	// is rejected by the provider for every template shaped differently, and a
	// rejection is an alert that never arrived.
	templates template_domain.Repository
}

func (d commentAlertDispatcher) Dispatch(ctx context.Context, in ca.AlertDelivery) error {
	recipient := normalizeAlertPhone(in.Recipient)
	if recipient == "" {
		return fmt.Errorf("%w: the alert recipient is not a valid number", ca.ErrInvalidFilter)
	}
	switch in.Channel {
	case ca.AlertChannelOfficial:
		return d.dispatchOfficial(ctx, in, recipient)
	case ca.AlertChannelUnofficial:
		return d.dispatchUnofficial(ctx, in, recipient)
	}
	return fmt.Errorf("%w: channel %q", ca.ErrInvalidFilter, in.Channel)
}

// dispatchOfficial buys and sends an approved template.
func (d commentAlertDispatcher) dispatchOfficial(ctx context.Context, in ca.AlertDelivery, recipient string) error {
	if d.official == nil {
		return errors.New("comment alerts: the official WhatsApp channel is not configured")
	}
	started, err := d.official.Execute(ctx, wo.StartConversationInput{
		WorkspaceID:     in.WorkspaceID,
		UserID:          in.ActorUserID,
		BusinessPhoneID: in.BusinessPhoneID,
		TemplateID:      in.TemplateID,
		PhoneNumber:     recipient,
		BodyParams:      d.bodyParamsFor(in),
		IdempotencyKey:  in.IdempotencyKey,
		// An automated alert is not scoped to a department: it answers to the
		// rule, not to whoever happens to be on shift.
		IsAdmin: true,
	})

	switch {
	case err == nil:
		return nil
	// The recipient already has an open conversation, so the free path reaches
	// them and the template does not have to be bought. The outreach use case
	// hands back the entry precisely so this can happen.
	case errors.Is(err, wo.ErrWindowAlreadyOpen) && started != nil && started.EntryID != "":
		return d.sendIntoEntry(ctx, in, started.EntryID, started.EntryType)
	// This firing is already in flight from another attempt. Not an error: the
	// message is on its way, and reporting a failure would only invite a retry
	// that the idempotency key would refuse anyway.
	case errors.Is(err, template_domain.ErrSendInProgress):
		return nil
	}
	return err
}

// dispatchUnofficial opens the conversation, then sends through the composer.
//
// Two existing use cases, in the order the channel itself insists on: starting
// a conversation deliberately does not send, so that sending stays in one place
// for operators and automations alike.
func (d commentAlertDispatcher) dispatchUnofficial(ctx context.Context, in ca.AlertDelivery, recipient string) error {
	if d.unofficial == nil {
		return errors.New("comment alerts: the unofficial WhatsApp channel is not configured")
	}
	started, err := d.unofficial.Execute(ctx, uwuc.StartConversationInput{
		WorkspaceID: in.WorkspaceID,
		InstanceID:  in.InstanceID,
		PhoneNumber: recipient,
	})
	if err != nil {
		return err
	}
	// The conversation id IS the inbox entry id for this channel.
	return d.sendIntoEntry(ctx, in, started.ConversationID, string(shared.EntryTypeUnofficialWhatsApp))
}

func (d commentAlertDispatcher) sendIntoEntry(ctx context.Context, in ca.AlertDelivery, entryID, entryType string) error {
	if d.send == nil {
		return errors.New("comment alerts: the composer is not available")
	}
	_, err := d.send.Execute(ctx, conversation_domain.OperatorSendInput{
		EntryID:      entryID,
		EntryType:    entryType,
		WorkspaceID:  in.WorkspaceID,
		SenderUserID: in.ActorUserID,
		Text:         in.Text,
	})
	return err
}

// bodyParamsFor fills exactly the variables the chosen template declares.
//
// If the template cannot be read, the canonical set is sent: a send that MIGHT
// be refused for a shape mismatch beats no attempt at all, and the failure is
// recorded on the rule either way.
func (d commentAlertDispatcher) bodyParamsFor(in ca.AlertDelivery) []string {
	if d.templates == nil || in.TemplateID == "" {
		return in.TemplateParams
	}
	tmpl, err := d.templates.FindByID(in.TemplateID)
	if err != nil || tmpl == nil {
		return in.TemplateParams
	}
	bodyNames, _ := tmpl.GetBodyAndHeaderParameterNames()
	return ca.FillTemplateParams(in.Facts, bodyNames)
}

// normalizeAlertPhone is tolerant of what an operator types and strict about
// what leaves: NormalizeRawNumber accepts formatting and a missing country
// code, NormalizeNumber then refuses anything that is not a real one.
func normalizeAlertPhone(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if normalized := lead_domain.NormalizeNumber(trimmed); normalized != "" {
		return normalized
	}
	return lead_domain.NormalizeNumber(lead_domain.NormalizeRawNumber(trimmed))
}
