package mercadopago

import (
	"context"
	"errors"
	"fmt"
	"log"

	"vozko/domain/payment"
)

// webhookResolver turns a queued Mercado Pago notification into a canonical event.
//
// Mercado Pago's notification is a doorbell, not a letter: it says "payment 123
// changed" and nothing more. Resolving therefore means fetching the payment and
// deriving the event from its current state, which has the pleasant side effect of
// making redelivery idempotent — two deliveries of the same notification resolve
// against the same payment and produce the same event.
type webhookResolver struct {
	client Client
}

// NewWebhookResolver builds the resolver the queue consumer depends on.
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
		// Nothing about this payload will improve on a retry.
		return nil, fmt.Errorf("%w: %v", payment.ErrWebhookMalformed, err)
	}

	if !notification.IsPayment() {
		// Mercado Pago also notifies about plans, subscriptions, chargebacks and POS
		// events on the same URL. Acknowledge them so it stops retrying.
		return nil, fmt.Errorf("%w: notification type %q", payment.ErrWebhookIgnored, notification.NormalizedType())
	}

	chargeID := notification.ResourceID()
	fetched, err := r.client.GetPayment(ctx, chargeID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidPaymentID):
			return nil, fmt.Errorf("%w: %v", payment.ErrWebhookMalformed, err)
		case errors.Is(err, ErrNotFound):
			// A payment that belongs to another account or environment (a test
			// notification against production credentials, most often). Retrying will
			// keep 404ing, so drop it rather than filling the queue.
			return nil, fmt.Errorf("%w: payment %s not found", payment.ErrWebhookIgnored, chargeID)
		case errors.Is(err, ErrUnauthorized):
			// Credentials are wrong or rotated. This IS worth retrying: the message
			// must survive until the operator fixes the token, or paid invoices are
			// silently lost.
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

// notificationEventID picks the id used for logging and event correlation. The
// notification's own id is preferred; the resource id is the fallback for the legacy
// IPN envelope, which has no notification id.
func notificationEventID(n *Notification) string {
	if id := n.ID.String(); id != "" && id != "0" {
		return id
	}
	return n.ResourceID()
}

var _ payment.WebhookResolver = (*webhookResolver)(nil)
