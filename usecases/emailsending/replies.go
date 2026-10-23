package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"sync"
	"time"

	es "vozko/domain/emailsending"
	"vozko/domain/notification"
)

const WalletTopUpPath = "/dashboard/invoices"

type SenderTraffic interface {
	IncrWithTTL(key string, ttl time.Duration) (int64, error)
}

func repliesPausedKey(workspaceID string) string { return "email_replies_paused:" + workspaceID }

func senderTrafficKey(ip netip.Addr) string { return "email:inbound:sender:" + ip.String() }

type Replies struct {
	c *core
}

func (r *Replies) Pause(ctx context.Context, workspaceID string) error {
	c := r.c
	ctx = es.WithProviderLane(ctx, es.LaneProtect)
	domains, err := c.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	now := c.Now()
	paused := 0
	var errs []error
	for _, d := range domains {
		if !d.ReceivesReplies() || d.RepliesPaused() {
			continue
		}
		if err := c.Routes.PauseReplies(ctx, d.ReplyDomain); err != nil {
			errs = append(errs, providerFailure("pause replies of "+d.ReplyDomain, err))
			continue
		}
		if err := c.ReplyPauses.MarkRepliesPaused(ctx, workspaceID, d.ID, now); err != nil {
			errs = append(errs, fmt.Errorf("emailsending: flag replies of %s as paused: %w", d.ReplyDomain, err))
			continue
		}
		paused++
	}
	if paused > 0 {
		c.alertRepliesPaused(ctx, workspaceID)
	}
	return errors.Join(errs...)
}

func (r *Replies) resume(ctx context.Context, workspaceID string) error {
	c := r.c
	ctx = es.WithProviderLane(ctx, es.LaneProtect)
	domains, err := c.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	var errs []error
	for _, d := range domains {
		if !d.RepliesPaused() {
			continue
		}
		if err := c.Routes.ResumeReplies(ctx, d.ReplyDomain); err != nil {
			errs = append(errs, providerFailure("resume replies of "+d.ReplyDomain, err))
			continue
		}
		if err := c.ReplyPauses.ClearRepliesPaused(ctx, workspaceID, d.ID); err != nil {
			errs = append(errs, fmt.Errorf("emailsending: clear the replies pause of %s: %w", d.ReplyDomain, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	c.resolve(ctx, workspaceID, repliesPausedKey(workspaceID))
	return nil
}

func (r *Replies) RecordSender(ctx context.Context, ip netip.Addr) {
	if !ip.IsValid() {
		return
	}
	c := r.c
	ctx = es.WithProviderLane(ctx, es.LaneProtect)
	count, err := c.Traffic.IncrWithTTL(senderTrafficKey(ip), es.InboundFloodWindow)
	if err != nil {
		c.opsAlert(ctx, "Contagem de remetentes de e-mail indisponível", fmt.Sprintf("a mensagem de %s foi processada sem contagem de flood: %v", ip, err))
		return
	}
	if !es.Flooding(int(count)) {
		return
	}
	created, err := c.Routes.BlockSender(ctx, ip, c.Now().Add(es.InboundBlockDuration))
	if err != nil {
		c.opsAlert(ctx, "Bloqueio de remetente de e-mail falhou", fmt.Sprintf("%s enviou %d mensagens em %s e não foi bloqueado: %v", ip, count, es.InboundFloodWindow, err))
		return
	}
	if created {
		c.opsAlert(ctx, "Remetente de e-mail bloqueado", fmt.Sprintf("%s enviou %d mensagens em %s; bloqueado no provedor por %s", ip, count, es.InboundFloodWindow, es.InboundBlockDuration))
	}
}

func (c *core) opsAlert(ctx context.Context, subject, detail string) {
	if err := c.Ops.Alert(ctx, subject, detail); err != nil {
		log.Printf("[emailsending] ops alert %q failed: %v (%s)", subject, err, detail)
	}
}

func (c *core) alertRepliesPaused(ctx context.Context, workspaceID string) {
	c.publish(ctx, notification.Draft{
		WorkspaceID: workspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityCritical,
		Title:       "Respostas por e-mail pausadas",
		Body:        "O saldo da carteira ficou negativo, então novas respostas por e-mail estão sendo recusadas. Pedidos de descadastro continuam sendo recebidos. Recarregue a carteira para voltar a receber respostas.",
		ActionLabel: "Recarregar carteira",
		ActionURL:   WalletTopUpPath,
		EpisodeKey:  repliesPausedKey(workspaceID),
	})
}

type ReplySweep struct {
	c       *core
	mu      sync.Mutex
	adopted bool
}

func (s *ReplySweep) Execute(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.c
	errs := []error{s.adoptLegacy(es.WithProviderLane(ctx, es.LaneAccount))}
	ctx = es.WithProviderLane(ctx, es.LaneProtect)
	holds, err := c.ReplyPauses.ListReplyHoldMismatches(ctx)
	errs = append(errs, err)
	replies := &Replies{c: c}
	for _, hold := range holds {
		if es.InboundPaused(hold.BalanceMicros) {
			errs = append(errs, replies.Pause(ctx, hold.WorkspaceID))
			continue
		}
		errs = append(errs, replies.resume(ctx, hold.WorkspaceID))
	}
	if err := c.Routes.UnblockExpired(ctx, c.Now()); err != nil {
		errs = append(errs, providerFailure("unblock expired senders", err))
	}
	return errors.Join(errs...)
}

func (s *ReplySweep) adoptLegacy(ctx context.Context) error {
	if s.adopted {
		return nil
	}
	domains, err := s.c.ReplyPauses.ListAll(ctx)
	if err != nil {
		return err
	}
	routes := make([]es.ReplyRoute, 0, len(domains))
	for _, d := range domains {
		if d.ReceivesReplies() {
			routes = append(routes, es.ReplyRoute{ReplyDomain: d.ReplyDomain, Paused: d.RepliesPaused()})
		}
	}
	if err := s.c.Routes.AdoptLegacyRoutes(ctx, routes); err != nil {
		return providerFailure("adopt legacy reply routes", err)
	}
	s.adopted = true
	return nil
}
