package unofficial_whatsapp

import (
	"context"
	"fmt"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

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

type LinkChallenge struct {
	Instance  *uw.Instance `json:"instance"`
	QRCode    string       `json:"qrCode,omitempty"`
	PairCode  string       `json:"pairCode,omitempty"`
	ExpiresAt *time.Time   `json:"expiresAt,omitempty"`
}

type ConnectRequest struct {
	InstanceID  string
	WorkspaceID string
	Mode        uw.ConnectMode
	Phone       string
	SystemName  string
	Scope       uw.DepartmentScope
}

func (uc *ConnectInstanceUseCase) Connect(ctx context.Context, req ConnectRequest) (*LinkChallenge, error) {
	if !req.Mode.Valid() {
		req.Mode = uw.ConnectModeQR
	}

	instance, server, err := uc.load(ctx, req.InstanceID, req.WorkspaceID, req.Scope)
	if err != nil {
		return nil, err
	}
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

	if _, err := uc.sync.apply(ctx, instance, session); err != nil {
		return nil, err
	}
	return challengeFrom(instance, session, req.Mode), nil
}

func (uc *ConnectInstanceUseCase) Status(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) (*LinkChallenge, error) {
	instance, server, err := uc.load(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return nil, err
	}

	session, err := uc.provider.Status(ctx, uw.RefFor(server, instance))
	if err != nil {
		if provErr, ok := uw.AsProviderError(err); ok && provErr.NeedsReconnect() {
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
	return challengeFrom(instance, session, ""), nil
}

func (uc *ConnectInstanceUseCase) Disconnect(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) error {
	instance, server, err := uc.load(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return err
	}
	if err := uc.provider.Disconnect(ctx, uw.RefFor(server, instance)); err != nil {
		if provErr, ok := uw.AsProviderError(err); !ok || !provErr.NeedsReconnect() {
			return fmt.Errorf("unofficial whatsapp: disconnect: %w", err)
		}
	}
	return uc.markDisconnected(ctx, instance, "disconnected by operator")
}

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
