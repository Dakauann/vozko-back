package unofficial_whatsapp

import (
	"context"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

type ConnectionListener interface {
	InstanceConnected(ctx context.Context, instance *uw.Instance)
}

type sessionSync struct {
	instances uw.InstanceRepository
	listener  ConnectionListener
	gate      LinkGate
}

func (s sessionSync) apply(ctx context.Context, instance *uw.Instance, session *uw.Session) (*uw.Instance, error) {
	if session == nil {
		return instance, nil
	}

	wasLive := instance.SessionLive()
	now := time.Now().UTC()
	update := uw.SessionUpdate{
		PolledAt:             now,
		JID:                  uw.BareJID(session.JID),
		LID:                  session.LID,
		PhoneNumber:          uw.PhoneFromJID(session.JID),
		ProfileName:          session.ProfileName,
		ProfilePicURL:        session.ProfilePicURL,
		IsBusinessAcct:       session.IsBusiness,
		Platform:             session.Platform,
		LastDisconnectAt:     session.LastDisconnectAt,
		LastDisconnectReason: session.LastDisconnectReason,
	}

	if next, ok := uw.MapState(session.State, session.Connected); ok {
		if instance.Status.CanTransitionTo(next) {
			update.Status = &next
			if next == uw.StatusConnected && instance.ConnectedAt == nil {
				update.ConnectedAt = &now
			}
		} else if next != instance.Status {
			log.Printf("[unofficial-whatsapp] instance %s: refusing %s → %s",
				instance.ID, instance.Status, next)
		}
	} else if session.State != "" {
		log.Printf("[unofficial-whatsapp] instance %s: unrecognised provider state %q; status left at %s",
			instance.ID, session.State, instance.Status)
	}

	if !wasLive && update.Status != nil && *update.Status == uw.StatusConnected && update.JID != "" && s.gate != nil {
		if err := s.gate.Admit(ctx, instance, update.JID); err != nil {
			return nil, err
		}
	}

	if err := s.instances.UpdateSession(ctx, instance.ID, update); err != nil {
		return nil, err
	}

	applyToDomain(instance, update, session)
	if !wasLive && instance.SessionLive() && s.listener != nil {
		s.listener.InstanceConnected(ctx, instance)
	}
	return instance, nil
}

func applyToDomain(instance *uw.Instance, update uw.SessionUpdate, session *uw.Session) {
	if update.Status != nil {
		instance.Status = *update.Status
		instance.StatusReason = update.StatusReason
	}
	if update.ConnectedAt != nil {
		instance.ConnectedAt = update.ConnectedAt
	}
	assignIfPresent(&instance.JID, update.JID)
	assignIfPresent(&instance.LID, update.LID)
	assignIfPresent(&instance.PhoneNumber, update.PhoneNumber)
	assignIfPresent(&instance.ProfileName, update.ProfileName)
	assignIfPresent(&instance.ProfilePicURL, update.ProfilePicURL)
	assignIfPresent(&instance.Platform, update.Platform)
	if update.JID != "" {
		instance.IsBusinessAcct = session.IsBusiness
	}
	if update.LastDisconnectAt != nil {
		instance.LastDisconnectAt = update.LastDisconnectAt
		instance.LastDisconnectWhy = update.LastDisconnectReason
	}
	instance.LastPolledAt = &update.PolledAt
}

func assignIfPresent(target *string, value string) {
	if value != "" {
		*target = value
	}
}

type sessionHost struct {
	sync sessionSync
}

func (h *sessionHost) SetConnectionListener(listener ConnectionListener) {
	h.sync.listener = listener
}

func (h *sessionHost) SetLinkGate(gate LinkGate) {
	h.sync.gate = gate
}

func markInstanceDisconnected(ctx context.Context, instances uw.InstanceRepository, instance *uw.Instance, reason string) error {
	if !instance.Status.CanTransitionTo(uw.StatusDisconnected) {
		return nil
	}
	if err := instances.UpdateStatus(ctx, instance.ID, uw.StatusDisconnected, reason); err != nil {
		return err
	}
	instance.Status = uw.StatusDisconnected
	instance.StatusReason = reason
	return nil
}
