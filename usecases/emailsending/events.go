package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	es "vozko/domain/emailsending"
)

type Events struct {
	c *core

	mu           sync.RWMutex
	observers    []es.MessageObserver
	unsubscriber es.Unsubscriber
	inbound      es.InboundHandler
}

func (e *Events) AddObserver(o es.MessageObserver) {
	if o == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observers = append(e.observers, o)
}

func (e *Events) SetUnsubscriber(u es.Unsubscriber) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unsubscriber = u
}

func (e *Events) SetInboundHandler(h es.InboundHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inbound = h
}

func (e *Events) Apply(ctx context.Context, n es.Notification) error {
	switch {
	case n.Delivery != nil:
		return e.applyDelivery(ctx, *n.Delivery)
	case n.TenantStatus != nil:
		return e.applyTenantStatus(ctx, *n.TenantStatus)
	case n.Finding != nil:
		return e.applyFinding(ctx, *n.Finding)
	case n.Inbound != nil:
		e.mu.RLock()
		handler := e.inbound
		e.mu.RUnlock()
		if handler == nil {
			return ErrNoInboundHandler
		}
		return handler.HandleInbound(ctx, *n.Inbound)
	case n.Notice != nil:
		log.Printf("[emailsending] provider notice %s received, nothing to apply", n.Notice.Kind)
		return nil
	}
	return ErrEmptyNotification
}

func (e *Events) message(ctx context.Context, ev es.DeliveryEvent) (*es.Message, error) {
	c := e.c
	m, err := c.Messages.GetByProviderID(ctx, ev.ProviderMessageID)
	if err == nil || !errors.Is(err, es.ErrMessageNotFound) {
		return m, err
	}
	id := ev.Tags[es.MessageIDTag]
	if id == "" {
		return nil, es.ErrMessageNotFound
	}
	m, err = c.Messages.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.ProviderMessageID != "" && m.ProviderMessageID != ev.ProviderMessageID {
		return nil, fmt.Errorf("%w: tag points to message %s of another provider id", es.ErrMessageNotFound, id)
	}
	if m.ProviderMessageID == "" {
		m.ProviderMessageID = ev.ProviderMessageID
		if m.Status == es.MessageQueued || m.Status == es.MessageFailed {
			m.Status = es.MessageSent
			m.SentAt = ev.OccurredAt
		}
		if err := c.Messages.Update(ctx, m); err != nil {
			return nil, err
		}
		log.Printf("[emailsending] message %s healed with provider id %s", m.ID, ev.ProviderMessageID)
	}
	return m, nil
}

func (e *Events) applyDelivery(ctx context.Context, ev es.DeliveryEvent) error {
	c := e.c
	m, err := e.message(ctx, ev)
	if errors.Is(err, es.ErrMessageNotFound) {
		log.Printf("[emailsending] %s event for unknown provider message %s dropped", ev.Kind, ev.ProviderMessageID)
		return nil
	}
	if err != nil {
		return err
	}
	appended, err := c.Messages.AppendEvent(ctx, m.ID, ev)
	if err != nil {
		return err
	}
	effect := appended.Effect
	if !appended.New {
		scratch := appended.Message
		effect = es.ApplyEvent(&scratch, ev)
	}
	if effect.Suppress {
		if err := c.Suppressions.Suppress(ctx, es.Suppression{
			WorkspaceID:   appended.Message.WorkspaceID,
			Address:       appended.Message.Address,
			Reason:        effect.SuppressReason,
			SourceMessage: appended.Message.ID,
			SuppressedAt:  ev.OccurredAt,
		}); err != nil {
			return err
		}
	}
	reputation, err := c.Reputation.Record(ctx, appended.Message, ev, effect)
	if err != nil {
		return err
	}
	if reputation != "" {
		if err := c.Suppressions.Suppress(ctx, es.Suppression{
			WorkspaceID:   appended.Message.WorkspaceID,
			Address:       appended.Message.Address,
			Reason:        reputation,
			SourceMessage: appended.Message.ID,
			SuppressedAt:  ev.OccurredAt,
		}); err != nil {
			return err
		}
	}
	if effect.Unsubscribe {
		if err := e.unsubscribe(ctx, appended.Message); err != nil {
			return err
		}
	}
	if appended.New {
		e.notifyObservers(ctx, appended.Message, ev, effect)
	}
	return nil
}

func (e *Events) unsubscribe(ctx context.Context, m es.Message) error {
	e.mu.RLock()
	u := e.unsubscriber
	e.mu.RUnlock()
	if u == nil {
		return ErrNoUnsubscriber
	}
	if err := u.UnsubscribeByComplaint(ctx, m.WorkspaceID, m.Address, m.ID); err != nil {
		return err
	}
	if m.UnsubscribedAt != nil {
		return nil
	}
	now := e.c.Now()
	m.UnsubscribedAt = &now
	return e.c.Messages.Update(ctx, &m)
}

func (e *Events) notifyObservers(ctx context.Context, m es.Message, ev es.DeliveryEvent, effect es.EventEffect) {
	e.mu.RLock()
	observers := append([]es.MessageObserver(nil), e.observers...)
	e.mu.RUnlock()
	for _, o := range observers {
		if err := o.OnMessageEvent(ctx, m, ev, effect); err != nil {
			log.Printf("[emailsending] observer %T failed on %s of message %s: %v", o, ev.Kind, m.ID, err)
		}
	}
}

func (e *Events) applyTenantStatus(ctx context.Context, ev es.TenantStatusEvent) error {
	c := e.c
	acct, err := c.Accounts.GetByTenantName(ctx, ev.TenantName)
	if errors.Is(err, es.ErrAccountNotFound) {
		log.Printf("[emailsending] tenant status for unknown tenant %q dropped", ev.TenantName)
		return nil
	}
	if err != nil {
		return err
	}
	disabled := ev.Status == es.TenantDisabled
	switch {
	case ev.Origin == es.OriginProvider && disabled:
		if acct.Status == es.AccountActive {
			acct.Status, acct.StatusReason = es.AccountPausedByProvider, ev.Cause
			if err := c.Accounts.Update(ctx, acct); err != nil {
				return err
			}
		}
		c.alertTenantPaused(ctx, acct, ev.Cause)
	case ev.Origin == es.OriginProvider:
		if acct.Status == es.AccountPausedByProvider {
			acct.Status, acct.StatusReason = es.AccountActive, ""
			if err := c.Accounts.Update(ctx, acct); err != nil {
				return err
			}
		}
		c.resolve(ctx, acct.WorkspaceID, tenantPausedKey(acct.WorkspaceID))
	case disabled:
		if acct.Status == es.AccountActive {
			acct.Status, acct.StatusReason = es.AccountPausedByPlatform, ev.Cause
			return c.Accounts.Update(ctx, acct)
		}
	default:
		if acct.Status == es.AccountPausedByPlatform {
			acct.Status, acct.StatusReason = es.AccountActive, ""
			return c.Accounts.Update(ctx, acct)
		}
	}
	return nil
}

func (e *Events) applyFinding(ctx context.Context, f es.FindingEvent) error {
	c := e.c
	acct, err := c.Accounts.GetByTenantName(ctx, f.TenantName)
	if errors.Is(err, es.ErrAccountNotFound) {
		log.Printf("[emailsending] finding for unknown tenant %q dropped", f.TenantName)
		return nil
	}
	if err != nil {
		return err
	}
	if !f.Open {
		c.resolve(ctx, acct.WorkspaceID, findingKey(acct.WorkspaceID, f.Type))
		return nil
	}
	if f.Impact != es.FindingHigh {
		return nil
	}
	c.alertFinding(ctx, acct, f)
	return nil
}
