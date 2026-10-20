package tiktok

import (
	"context"
	"log"
	"time"

	ttdomain "vozko/domain/tiktok"
	"vozko/pkg/bounded"
)

const (
	HealthCheckInterval    = 24 * time.Hour
	healthCheckConcurrency = 4
	healthCheckPage        = 200
)

type HealthCheckDeps struct {
	Accounts ttdomain.AccountRepository
	Services Services
	Tokens   *TokenSource
	Webhooks *WebhookSubscriptions
	Alerts   AccountAlerts
	Metrics  Metrics
}

type HealthCheckJob struct {
	d   HealthCheckDeps
	now Clock
}

func NewHealthCheckJob(d HealthCheckDeps) *HealthCheckJob {
	if d.Accounts == nil || d.Tokens == nil || d.Webhooks == nil || d.Alerts == nil || d.Metrics == nil ||
		d.Services.OAuth == nil || d.Services.Profiles == nil || d.Services.Messaging == nil || d.Services.Errors == nil {
		panic("tiktok: the health check requires accounts, oauth, profiles, messaging, an error classifier, tokens, webhooks, alerts and metrics")
	}
	return &HealthCheckJob{d: d, now: utcNow}
}

func (j *HealthCheckJob) Execute(ctx context.Context) error {
	webhooksHealthy, err := j.d.Webhooks.Ensure(ctx)
	if err != nil {
		log.Printf("[tiktok][health] webhook configuration unconfirmed: %v", err)
		webhooksHealthy = false
	}
	due, err := j.d.Accounts.ListDueForHealthCheck(ctx, j.now().Add(-HealthCheckInterval), healthCheckPage)
	if err != nil {
		return err
	}
	bounded.ForEach(ctx, due, healthCheckConcurrency, func(ctx context.Context, a *ttdomain.Account) {
		j.check(ctx, a, webhooksHealthy)
	})
	return nil
}

func (j *HealthCheckJob) check(ctx context.Context, account *ttdomain.Account, webhooksHealthy bool) {
	defer func() {
		if err := j.d.Accounts.MarkHealthChecked(ctx, account.ID, j.now()); err != nil {
			log.Printf("[tiktok][health] %s not marked as checked: %v", account.ID, err)
		}
	}()
	if account.Status != ttdomain.StatusConnected {
		return
	}
	creds, err := j.d.Tokens.Credentials(ctx, account)
	if err != nil {
		j.d.Metrics.AddTikTokEvent("health", "token_unavailable")
		return
	}
	info, err := j.d.Services.OAuth.TokenInfo(ctx, creds.AccessToken)
	if err != nil {
		if j.d.Services.Errors.IsAuth(err) {
			j.revoke(ctx, account, "TikTok no longer accepts the access token")
			return
		}
		j.d.Metrics.AddTikTokEvent("health", "token_info_failed")
		return
	}
	profile, err := j.d.Services.Profiles.Profile(ctx, creds)
	if err != nil {
		j.d.Metrics.AddTikTokEvent("health", "profile_failed")
		return
	}
	caps, reason, err := ProbeCapabilities(ctx, j.d.Services, creds, info.Scopes, profile)
	if err != nil {
		j.d.Metrics.AddTikTokEvent("health", "probe_inconclusive")
		return
	}
	update := ttdomain.ProfileUpdate{
		Username: profile.Username, DisplayName: profile.DisplayName, AvatarKey: account.AvatarKey,
		HasAdvancedAccess: profile.HasAdvancedAccess, IsBusinessAccount: profile.IsBusinessAccount,
		GrantedScopes: info.Scopes, Capabilities: caps,
	}
	if err := j.d.Accounts.UpdateProfile(ctx, account.ID, update); err != nil {
		log.Printf("[tiktok][health] profile of %s not saved: %v", account.ID, err)
		return
	}
	if !caps.Useful() {
		if err := j.d.Tokens.MarkLost(ctx, account, "the TikTok permissions for comments and messages were removed"); err != nil {
			log.Printf("[tiktok][health] %s not marked for reconnect: %v", account.ID, err)
		}
		return
	}
	if reason != "" {
		j.d.Metrics.AddTikTokEvent("health", "messaging_refused")
	}
	if err := j.d.Accounts.SetWebhooksHealthy(ctx, account.ID, webhooksHealthy); err != nil {
		log.Printf("[tiktok][health] webhook health of %s not saved: %v", account.ID, err)
	}
	switch {
	case !webhooksHealthy:
		j.alert(j.d.Alerts.WebhooksUnhealthy(ctx, account))
	case account.ReconnectSoon(j.now()):
		j.alert(j.d.Alerts.ReconnectSoon(ctx, account))
	default:
		j.alert(j.d.Alerts.Healthy(ctx, account))
	}
	j.d.Metrics.AddTikTokEvent("health", "checked")
}

func (j *HealthCheckJob) revoke(ctx context.Context, account *ttdomain.Account, reason string) {
	purgeAfter := ttdomain.PurgeScheduleFrom(j.now())
	if err := j.d.Accounts.Revoke(ctx, account.ID, reason, purgeAfter); err != nil {
		log.Printf("[tiktok][health] %s not marked as revoked: %v", account.ID, err)
		return
	}
	j.d.Metrics.AddTikTokEvent("health", "revoked")
	revoked := *account
	revoked.Status = ttdomain.StatusRevoked
	revoked.PurgeAfter = &purgeAfter
	j.alert(j.d.Alerts.NeedsReconnect(ctx, &revoked))
}

func (j *HealthCheckJob) alert(err error) {
	if err != nil {
		log.Printf("[tiktok][health] alert not published: %v", err)
	}
}

func (j *HealthCheckJob) CheckAccount(ctx context.Context, workspaceID, accountID string) (*ttdomain.Account, error) {
	account, err := j.d.Accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	webhooksHealthy, err := j.d.Webhooks.Ensure(ctx)
	if err != nil {
		log.Printf("[tiktok][health] webhook configuration unconfirmed: %v", err)
		webhooksHealthy = false
	}
	j.check(ctx, account, webhooksHealthy)
	return j.d.Accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
}
