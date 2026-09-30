package unofficial_whatsapp

import (
	"context"
	"fmt"
	"log"

	uw "vozko/domain/unofficial_whatsapp"
)

const (
	reasonRelinkedElsewhere   = "número reconectado em outra instância deste workspace"
	reasonNumberLiveElsewhere = "este número já está conectado em outro workspace ou departamento; desconecte-o lá antes de conectar aqui"
)

type LinkGate interface {
	Admit(ctx context.Context, candidate *uw.Instance, jid string) error
}

type LinkGuard struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	provider  uw.InstanceAPI
}

func NewLinkGuard(instances uw.InstanceRepository, servers uw.ServerRepository, provider uw.InstanceAPI) *LinkGuard {
	return &LinkGuard{instances: instances, servers: servers, provider: provider}
}

func (g *LinkGuard) Admit(ctx context.Context, candidate *uw.Instance, jid string) error {
	holders, err := g.instances.ListLiveByJID(ctx, jid)
	if err != nil {
		return fmt.Errorf("unofficial whatsapp: check which instances hold %s: %w", jid, err)
	}

	for _, holder := range holders {
		if holder.ID == candidate.ID {
			continue
		}
		if uw.SameLine(candidate, holder) && candidate.Status == uw.StatusAwaitingScan {
			if err := g.supersede(ctx, holder, candidate); err != nil {
				return err
			}
			continue
		}
		live, err := g.stillLive(ctx, holder)
		if err != nil {
			return err
		}
		if live {
			return g.refuse(ctx, candidate, holder)
		}
	}
	return nil
}

func (g *LinkGuard) supersede(ctx context.Context, old, successor *uw.Instance) error {
	if err := g.disconnect(ctx, old); err != nil {
		return fmt.Errorf("unofficial whatsapp: close the previous link %s of this number: %w", old.ID, err)
	}
	if err := g.markDisconnected(ctx, old, reasonRelinkedElsewhere); err != nil {
		return err
	}
	log.Printf("[unofficial-whatsapp][line] instance %s replaced instance %s as the live link of number %s",
		successor.ID, old.ID, successor.PhoneNumber)
	return nil
}

func (g *LinkGuard) refuse(ctx context.Context, candidate, holder *uw.Instance) error {
	if err := g.disconnect(ctx, candidate); err != nil {
		log.Printf("[unofficial-whatsapp][line] instance %s: could not close the refused link: %v", candidate.ID, err)
	}
	if err := g.markDisconnected(ctx, candidate, reasonNumberLiveElsewhere); err != nil {
		return err
	}
	log.Printf("[unofficial-whatsapp][line] instance %s refused: number %s is live on instance %s (workspace %s)",
		candidate.ID, candidate.PhoneNumber, holder.ID, holder.WorkspaceID)
	return uw.ErrNumberAlreadyLinked
}

func (g *LinkGuard) stillLive(ctx context.Context, holder *uw.Instance) (bool, error) {
	server, err := g.servers.FindByID(ctx, holder.ServerID)
	if err != nil {
		return false, fmt.Errorf("unofficial whatsapp: server of instance %s: %w", holder.ID, err)
	}
	session, err := g.provider.Status(ctx, uw.RefFor(server, holder))
	if err != nil {
		if provErr, ok := uw.AsProviderError(err); ok && provErr.NeedsReconnect() {
			return false, g.markDisconnected(ctx, holder, provErr.Error())
		}
		return false, fmt.Errorf("unofficial whatsapp: probe instance %s: %w", holder.ID, err)
	}
	if _, err := (sessionSync{instances: g.instances}).apply(ctx, holder, session); err != nil {
		return false, err
	}
	return holder.SessionLive(), nil
}

func (g *LinkGuard) disconnect(ctx context.Context, instance *uw.Instance) error {
	server, err := g.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return err
	}
	err = g.provider.Disconnect(ctx, uw.RefFor(server, instance))
	if provErr, ok := uw.AsProviderError(err); ok && provErr.NeedsReconnect() {
		return nil
	}
	return err
}

func (g *LinkGuard) markDisconnected(ctx context.Context, instance *uw.Instance, reason string) error {
	return markInstanceDisconnected(ctx, g.instances, instance, reason)
}
