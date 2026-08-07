package unofficial_whatsapp

import (
	"context"
	"fmt"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// ConnectInstanceUseCase starts a linking attempt and reports its progress.
//
// Connect and Status are one use case rather than two because they answer the
// same question from opposite ends — "give me something to scan" and "has it
// been scanned yet" — and share the reconciliation that turns a provider
// snapshot into a stored state.
type ConnectInstanceUseCase struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	provider  uw.ProviderAPI
	sync      sessionSync
}

func NewConnectInstanceUseCase(
	instances uw.InstanceRepository,
	servers uw.ServerRepository,
	provider uw.ProviderAPI,
) *ConnectInstanceUseCase {
	return &ConnectInstanceUseCase{
		instances: instances,
		servers:   servers,
		provider:  provider,
		sync:      sessionSync{instances: instances},
	}
}

// LinkChallenge is what the customer must act on to link their phone.
//
// ExpiresAt is populated from the provider's documented deadlines rather than
// left to the UI to guess: a screen that stalls past an expiry with no
// explanation is indistinguishable from one that is broken.
type LinkChallenge struct {
	Instance  *uw.Instance `json:"instance"`
	QRCode    string       `json:"qrCode,omitempty"`
	PairCode  string       `json:"pairCode,omitempty"`
	ExpiresAt *time.Time   `json:"expiresAt,omitempty"`
}

// ConnectRequest is one linking attempt.
type ConnectRequest struct {
	InstanceID  string
	WorkspaceID string
	Mode        uw.ConnectMode
	// Phone is required for pairing mode and ignored for QR.
	Phone      string
	SystemName string
	// Scope limits which numbers this caller may link. Linking is what turns a
	// provisioned slot into a live WhatsApp session, so it belongs to the
	// department that owns the number.
	Scope uw.DepartmentScope
}

// Connect asks the host for a QR code or a pairing code.
func (uc *ConnectInstanceUseCase) Connect(ctx context.Context, req ConnectRequest) (*LinkChallenge, error) {
	if !req.Mode.Valid() {
		req.Mode = uw.ConnectModeQR
	}

	instance, server, err := uc.load(ctx, req.InstanceID, req.WorkspaceID, req.Scope)
	if err != nil {
		return nil, err
	}
	// A banned number cannot be relinked, and offering a QR that can only fail
	// wastes the operator's time and teaches them to distrust the screen.
	if instance.Status.Terminal() {
		return nil, fmt.Errorf("%w: %s", uw.ErrStatusTransition, instance.StatusReason)
	}

	session, err := uc.provider.Connect(ctx, uw.RefFor(server, instance), uw.ConnectInput{
		Mode:       req.Mode,
		Phone:      req.Phone,
		SystemName: req.SystemName,
	})
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: start connection: %w", err)
	}

	// The provider reports "connecting" while a code is live; the sync maps
	// that onto AWAITING_SCAN so the row and the screen agree.
	if _, err := uc.sync.apply(ctx, instance, session); err != nil {
		return nil, err
	}
	return challengeFrom(instance, session, req.Mode), nil
}

// Status polls the host and reconciles what it reports.
//
// This is what the connect screen calls on a timer: the QR rotates, and the
// provider hands out the current one here rather than pushing it.
func (uc *ConnectInstanceUseCase) Status(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) (*LinkChallenge, error) {
	instance, server, err := uc.load(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return nil, err
	}

	session, err := uc.provider.Status(ctx, uw.RefFor(server, instance))
	if err != nil {
		if provErr, ok := uw.AsProviderError(err); ok && provErr.NeedsReconnect() {
			// The instance is gone from the host. Recording that is the whole
			// point of polling: the alternative is an inbox that is quiet for a
			// reason nobody can see.
			if markErr := uc.markDisconnected(ctx, instance, provErr.Error()); markErr != nil {
				return nil, markErr
			}
			return challengeFrom(instance, nil, ""), nil
		}
		return nil, fmt.Errorf("unofficial whatsapp: read status: %w", err)
	}

	if _, err := uc.sync.apply(ctx, instance, session); err != nil {
		return nil, err
	}
	// Mode is unknown on a poll, so the challenge carries whichever material
	// the host still considers live.
	return challengeFrom(instance, session, ""), nil
}

// Disconnect ends the session without removing the instance, so the same slot
// can be relinked to a different number.
func (uc *ConnectInstanceUseCase) Disconnect(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) error {
	instance, server, err := uc.load(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return err
	}
	if err := uc.provider.Disconnect(ctx, uw.RefFor(server, instance)); err != nil {
		// A host that has already forgotten the session reports 401, which is
		// the state we were asking for. Treating it as a failure would leave the
		// row claiming to be connected.
		if provErr, ok := uw.AsProviderError(err); !ok || !provErr.NeedsReconnect() {
			return fmt.Errorf("unofficial whatsapp: disconnect: %w", err)
		}
	}
	return uc.markDisconnected(ctx, instance, "disconnected by operator")
}

// Reset restarts a wedged runtime on the host without dropping the session.
//
// The host enforces a cooldown between resets and refuses inside it. That
// refusal is a normal answer, not a failure, so it is surfaced as one.
func (uc *ConnectInstanceUseCase) Reset(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) error {
	instance, server, err := uc.load(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return err
	}
	if err := uc.provider.Reset(ctx, uw.RefFor(server, instance)); err != nil {
		return fmt.Errorf("unofficial whatsapp: reset: %w", err)
	}
	return nil
}

func (uc *ConnectInstanceUseCase) markDisconnected(ctx context.Context, instance *uw.Instance, reason string) error {
	if !instance.Status.CanTransitionTo(uw.StatusDisconnected) {
		return nil
	}
	if err := uc.instances.UpdateStatus(ctx, instance.ID, uw.StatusDisconnected, reason); err != nil {
		return err
	}
	instance.Status = uw.StatusDisconnected
	instance.StatusReason = reason
	return nil
}

// load resolves an instance and its host, enforcing workspace ownership AND
// department scope.
//
// Both checks are here rather than in the handler because all four linking
// verbs — connect, poll, disconnect, reset — need them, and one that forgot
// would let an operator outside the department relink or drop another team's
// number.
func (uc *ConnectInstanceUseCase) load(
	ctx context.Context,
	instanceID, workspaceID string,
	scope uw.DepartmentScope,
) (*uw.Instance, *uw.Server, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if workspaceID != "" {
		// Not found rather than forbidden: confirming existence would let a
		// caller enumerate other tenants' instance ids, or discover which
		// numbers another department runs.
		if err := EnsureVisible(instance, workspaceID, scope); err != nil {
			return nil, nil, err
		}
	}
	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, nil, err
	}
	return instance, server, nil
}

// challengeFrom assembles the screen's view, attaching the provider's own
// deadline to whichever material is live.
func challengeFrom(instance *uw.Instance, session *uw.Session, mode uw.ConnectMode) *LinkChallenge {
	challenge := &LinkChallenge{Instance: instance}
	if session == nil {
		return challenge
	}
	challenge.QRCode = session.QRCode
	challenge.PairCode = session.PairCode

	ttl := time.Duration(0)
	switch {
	case session.PairCode != "" || mode == uw.ConnectModePairing:
		ttl = uw.PairingCodeTTL
	case session.QRCode != "":
		ttl = uw.QRCodeTTL
	}
	if ttl > 0 {
		expires := time.Now().UTC().Add(ttl)
		challenge.ExpiresAt = &expires
	}
	return challenge
}
