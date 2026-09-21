package mercadopago

import (
	"context"
	"errors"
	"fmt"
	"log"

	"vozko/domain/payment"
)

type webhookResolver struct {
	client Client
}

func NewWebhookResolver(c Client) payment.WebhookResolver {
	return &webhookResolver{client: c}
}

func (r *webhookResolver) Provider() payment.Provider { return payment.ProviderMercadoPago }

func (r *webhookResolver) Resolve(ctx context.Context, raw []byte) (*payment.WebhookEvent, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("mercadopago resolver: client not configured")
	}

	notification, err := ParseNotification(raw, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", payment.ErrWebhookMalformed, err)
	}

	if !notification.IsPayment() {
		return nil, fmt.Errorf("%w: notification type %q", payment.ErrWebhookIgnored, notification.NormalizedType())
	}

	chargeID := notification.ResourceID()
	fetched, err := r.client.GetPayment(ctx, chargeID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidPaymentID):
			return nil, fmt.Errorf("%w: %v", payment.ErrWebhookMalformed, err)
		case errors.Is(err, ErrNotFound):
			return nil, fmt.Errorf("%w: payment %s not found", payment.ErrWebhookIgnored, chargeID)
		case errors.Is(err, ErrUnauthorized):
			return nil, fmt.Errorf("mercadopago resolver: unauthorized fetching payment %s: %w", chargeID, err)
		default:
			return nil, fmt.Errorf("mercadopago resolver: fetch payment %s: %w", chargeID, err)
		}
	}

	event, ok := ToWebhookEvent(notificationEventID(notification), fetched)
	if !ok {
		log.Printf("[mercadopago-resolver] payment %s in state %s/%s needs no local change",
			chargeID, fetched.Status, fetched.StatusDetail)
		return nil, fmt.Errorf("%w: payment %s status %s", payment.ErrWebhookIgnored, chargeID, fetched.Status)
	}

	return event, nil
}

func notificationEventID(n *Notification) string {
	if id := n.ID.String(); id != "" && id != "0" {
		return id
	}
	return n.ResourceID()
}

var _ payment.WebhookResolver = (*webhookResolver)(nil)
