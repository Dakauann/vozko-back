package container

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
	conversation_domain "vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	template_domain "vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type commentAlertDispatcher struct {
	official   wo.StartOfficialConversationUseCase
	unofficial *uwuc.StartConversationUseCase
	send       conversation_domain.OperatorSendUseCase
	templates  template_domain.Repository
	senders    ca.AlertSenderDirectory
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
		IsAdmin:         true,
	})

	switch {
	case err == nil:
		return nil
	case errors.Is(err, wo.ErrWindowAlreadyOpen) && started != nil && started.EntryID != "":
		return d.sendIntoEntry(ctx, in, started.EntryID, started.EntryType)
	case errors.Is(err, template_domain.ErrSendInProgress):
		return nil
	}
	return err
}

func (d commentAlertDispatcher) dispatchUnofficial(ctx context.Context, in ca.AlertDelivery, recipient string) error {
	if d.unofficial == nil {
		return errors.New("comment alerts: the unofficial WhatsApp channel is not configured")
	}
	instanceID, err := d.resolveInstance(ctx, in)
	if err != nil {
		return err
	}
	started, err := d.unofficial.Execute(ctx, uwuc.StartConversationInput{
		WorkspaceID: in.WorkspaceID,
		InstanceID:  instanceID,
		PhoneNumber: recipient,
	})
	if err != nil {
		return err
	}
	return d.sendIntoEntry(ctx, in, started.ConversationID, string(shared.EntryTypeUnofficialWhatsApp))
}

func (d commentAlertDispatcher) resolveInstance(ctx context.Context, in ca.AlertDelivery) (string, error) {
	if id := strings.TrimSpace(in.InstanceID); id != "" {
		return id, nil
	}
	if d.senders == nil {
		return "", errors.New("comment alerts: this rule does not say which number to send from")
	}
	list, err := d.senders.ChannelStatus(ctx, in.WorkspaceID)
	if err != nil {
		return "", err
	}
	status, _ := ca.ChannelStatusFor(list, ca.AlertChannelUnofficial)
	switch len(status.Senders) {
	case 0:
		return "", errors.New("comment alerts: this workspace has no connected number to send from")
	case 1:
		return status.Senders[0].ID, nil
	}
	return "", errors.New("comment alerts: this workspace has more than one connected number, so the rule has to name the one it sends from")
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

func normalizeAlertPhone(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if normalized := lead_domain.NormalizeNumber(trimmed); normalized != "" {
		return normalized
	}
	return lead_domain.NormalizeNumber(lead_domain.NormalizeRawNumber(trimmed))
}
