package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/pkg/bounded"
)

const (
	healthCheckEvery       = 24 * time.Hour
	healthCheckPage        = 200
	healthCheckConcurrency = 4
	healthCheckMaxAccounts = 10000
)

type HealthCheckDeps struct {
	Accounts mldomain.AccountRepository
	Contacts mldomain.ContactRepository
	Services Services
	Tokens   *TokenSource
	Alerts   AccountAlerts
	Metrics  Metrics
}

type HealthCheck struct {
	accounts mldomain.AccountRepository
	contacts mldomain.ContactRepository
	svc      Services
	tokens   *TokenSource
	alerts   AccountAlerts
	metrics  Metrics
	now      Clock
}

func NewHealthCheck(d HealthCheckDeps) *HealthCheck {
	if d.Accounts == nil || d.Contacts == nil || d.Tokens == nil || d.Alerts == nil || d.Metrics == nil ||
		d.Services.Users == nil || d.Services.Questions == nil || d.Services.Messages == nil || d.Services.Errors == nil {
		panic("mercadolivre: health check requires accounts, contacts, users, questions, messages, an error classifier, tokens, alerts and metrics")
	}
	return &HealthCheck{accounts: d.Accounts, contacts: d.Contacts, svc: d.Services, tokens: d.Tokens, alerts: d.Alerts, metrics: d.Metrics, now: utcNow}
}

func (h *HealthCheck) Execute(ctx context.Context) error {
	ctx = mldomain.WithBackgroundPriority(ctx)
	before := h.now().Add(-healthCheckEvery)
	seen := map[string]bool{}
	for len(seen) < healthCheckMaxAccounts {
		due, err := h.accounts.ListDueForHealthCheck(ctx, before, healthCheckPage)
		if err != nil {
			return err
		}
		var fresh []*mldomain.Account
		for _, a := range due {
			if !seen[a.ID] {
				seen[a.ID] = true
				fresh = append(fresh, a)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		bounded.ForEach(ctx, fresh, healthCheckConcurrency, func(ctx context.Context, a *mldomain.Account) {
			checked, err := h.Check(ctx, a)
			if err != nil {
				log.Printf("[mercadolivre][health] account %s: %v", a.ID, err)
				return
			}
			if err := h.syncQuestionBlacklist(ctx, checked); err != nil {
				log.Printf("[mercadolivre][health] question blacklist of %s not mirrored: %v", a.ID, err)
			}
		})
		if len(due) < healthCheckPage {
			return nil
		}
	}
	return nil
}

func (h *HealthCheck) Check(ctx context.Context, account *mldomain.Account) (*mldomain.Account, error) {
	checkErr := h.check(ctx, account)
	if err := h.accounts.MarkHealthChecked(ctx, account.ID, h.now()); err != nil {
		return nil, err
	}
	outcome := "healthy"
	if checkErr != nil {
		outcome = "inconclusive"
	}
	h.metrics.AddMercadoLivreEvent("health", outcome)
	fresh, err := h.accounts.FindByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	return fresh, checkErr
}

func (h *HealthCheck) check(ctx context.Context, account *mldomain.Account) error {
	switch account.Status {
	case mldomain.StatusNeedsReconnect, mldomain.StatusRevoked:
		return h.alerts.NeedsReconnect(ctx, account)
	case mldomain.StatusConnected:
	default:
		return nil
	}
	creds, err := h.tokens.Credentials(ctx, account)
	if err != nil {
		if errors.Is(err, mldomain.ErrNeedsReconnect) {
			return nil
		}
		return err
	}
	me, err := h.svc.Users.Me(ctx, creds)
	if err != nil {
		if h.svc.Errors.IsAuth(err) {
			_, recoverErr := h.tokens.RecoverFromAuthFailure(ctx, account)
			return recoverErr
		}
		return fmt.Errorf("%w: %v", ErrProbeInconclusive, err)
	}
	if nickname := strings.TrimSpace(me.Nickname); nickname != "" && nickname != account.Nickname {
		if err := h.accounts.UpdateNickname(ctx, account.ID, nickname); err != nil {
			return err
		}
	}
	caps, err := probeCapabilities(ctx, h.svc, creds, account.GrantedScopes)
	if err != nil {
		if h.svc.Errors.IsAuth(err) {
			_, recoverErr := h.tokens.RecoverFromAuthFailure(ctx, account)
			return recoverErr
		}
		return err
	}
	if caps != account.Capabilities() {
		log.Printf("[mercadolivre][health] account %s capabilities changed to %+v", account.ID, caps)
		return h.accounts.UpdateCapabilities(ctx, account.ID, caps)
	}
	return nil
}

func (h *HealthCheck) syncQuestionBlacklist(ctx context.Context, account *mldomain.Account) error {
	if !account.CanReadQuestionsNow() {
		return nil
	}
	ctx = mldomain.WithBackgroundPriority(ctx)
	creds, err := h.tokens.Credentials(ctx, account)
	if err != nil {
		return err
	}
	started := h.now()
	scan := mldomain.NewBlacklistScan()
	for offset, more := 0, true; more; {
		page, err := h.svc.Questions.QuestionBlacklist(ctx, creds, offset, mldomain.MaxBlacklistLimit)
		if err != nil {
			h.metrics.AddMercadoLivreEvent("questions", "blacklist_failed")
			return err
		}
		offset, more = scan.Add(*page)
	}
	if err := h.contacts.MarkQuestionsBlocked(ctx, account.ID, scan.UserIDs(), started, started); err != nil {
		return err
	}
	if !scan.Complete() {
		h.metrics.AddMercadoLivreEvent("questions", "blacklist_partial")
		return nil
	}
	if err := h.contacts.ClearQuestionsBlockedExcept(ctx, account.ID, scan.UserIDs(), started); err != nil {
		return err
	}
	h.metrics.AddMercadoLivreEvent("questions", "blacklist_synced")
	return nil
}

func (h *HealthCheck) CheckInWorkspace(ctx context.Context, workspaceID, id string) (*mldomain.Account, error) {
	account, err := h.accounts.FindByIDInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return h.Check(ctx, account)
}
