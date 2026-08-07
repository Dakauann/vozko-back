package unofficial_whatsapp

import (
	"context"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// sessionSync turns one provider session snapshot into a persisted state change.
//
// It exists because four callers need exactly the same reconciliation, and any
// drift between them would be a bug that shows up on one path only: the connect
// flow, the status poll the connect screen drives, the health backstop, and —
// once Phase 2 lands — the handler for the provider's `connection` event.
// Writing it four times is how "the UI says connected but the cron says
// otherwise" happens.
//
// It also stamps PolledAt on every application, and that stamp is load-bearing
// beyond bookkeeping: CheckInstanceHealthUseCase selects instances whose stamp
// has gone stale, so an instance the `connection` webhook just reported for is
// skipped by the backstop automatically. The webhook handler gets that backoff
// for free by calling this — and would silently lose it by writing the status
// itself instead.
type sessionSync struct {
	instances uw.InstanceRepository
}

// apply reconciles a snapshot onto an instance, returning the instance as it now
// stands so a caller can answer a request without re-reading it.
//
// The instance argument is mutated in place, which is what lets the connect and
// status handlers return a fresh view without a second query.
func (s sessionSync) apply(ctx context.Context, instance *uw.Instance, session *uw.Session) (*uw.Instance, error) {
	if session == nil {
		return instance, nil
	}

	now := time.Now().UTC()
	update := uw.SessionUpdate{
		PolledAt:             now,
		JID:                  session.JID,
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
		// An unrecognised provider state deliberately leaves the status alone
		// rather than guessing: reporting a live session as disconnected because
		// the vendor added a state would close every composer on the channel.
		if instance.Status.CanTransitionTo(next) {
			update.Status = &next
			if next == uw.StatusConnected && instance.ConnectedAt == nil {
				update.ConnectedAt = &now
			}
		} else if next != instance.Status {
			// A refused transition is worth a line: it is either a provider
			// surprise or a lifecycle rule that needs revisiting, and silently
			// dropping it would hide both.
			log.Printf("[unofficial-whatsapp] instance %s: refusing %s → %s",
				instance.ID, instance.Status, next)
		}
	} else if session.State != "" {
		log.Printf("[unofficial-whatsapp] instance %s: unrecognised provider state %q; status left at %s",
			instance.ID, session.State, instance.Status)
	}

	if err := s.instances.UpdateSession(ctx, instance.ID, update); err != nil {
		return nil, err
	}

	applyToDomain(instance, update, session)
	return instance, nil
}

// applyToDomain mirrors onto the in-memory entity exactly what the repository
// wrote, so the value a caller returns matches the row.
//
// It follows the same "only when present" rule as the repository: a poll that
// came back empty must not blank an identity we already knew, or an operator
// looking at a dropped session cannot tell which number dropped.
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
