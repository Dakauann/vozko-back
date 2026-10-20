package tiktok

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	ttdomain "vozko/domain/tiktok"
	"vozko/usecases/shared/oauthstate"
)

type ConnectDeps struct {
	Accounts ttdomain.AccountRepository
	Services Services
	States   *oauthstate.Issuer
	Webhooks *WebhookSubscriptions
	Images   *ImageStore
	Alerts   AccountAlerts
	Metrics  Metrics
}

type ConnectUseCase struct {
	d   ConnectDeps
	now Clock
}

func NewConnectUseCase(d ConnectDeps) *ConnectUseCase {
	if d.Accounts == nil || d.States == nil || d.Webhooks == nil || d.Images == nil || d.Alerts == nil || d.Metrics == nil ||
		d.Services.OAuth == nil || d.Services.Profiles == nil || d.Services.Messaging == nil || d.Services.Errors == nil {
		panic("tiktok: connect requires accounts, oauth, profiles, messaging, an error classifier, state, webhooks, images, alerts and metrics")
	}
	return &ConnectUseCase{d: d, now: utcNow}
}

type StartConnectInput struct {
	WorkspaceID string
	UserID      string
	ReturnPath  string
	Popup       bool
}

type StartConnectOutput struct {
	AuthorizeURL string
}

func (uc *ConnectUseCase) Start(_ context.Context, in StartConnectInput) (*StartConnectOutput, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, ttdomain.ErrWorkspaceIDRequired
	}
	state, err := uc.d.States.Issue(oauthstate.IssueInput{WorkspaceID: in.WorkspaceID, UserID: in.UserID, ReturnPath: in.ReturnPath, Popup: in.Popup})
	if err != nil {
		return nil, err
	}
	target, err := uc.d.Services.OAuth.AuthorizeURL(state)
	if err != nil {
		return nil, err
	}
	return &StartConnectOutput{AuthorizeURL: target}, nil
}

type CompleteConnectInput struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

type CompleteConnectOutput struct {
	Account     *ttdomain.Account
	ReturnPath  string
	Popup       bool
	Reconnected bool
}

func (uc *ConnectUseCase) Complete(ctx context.Context, in CompleteConnectInput) (*CompleteConnectOutput, error) {
	state, err := uc.d.States.Redeem(in.State)
	if err != nil {
		uc.d.Metrics.AddTikTokEvent("connect", "state_refused")
		return nil, err
	}
	out := &CompleteConnectOutput{ReturnPath: uc.d.States.ReturnPath(state), Popup: state.Popup}
	account, reconnected, err := uc.complete(ctx, state.WorkspaceID, in)
	if err != nil {
		uc.d.Metrics.AddTikTokEvent("connect", "refused")
		log.Printf("[tiktok] connect refused for workspace %s: %v", state.WorkspaceID, err)
		return out, err
	}
	uc.d.Metrics.AddTikTokEvent("connect", "connected")
	out.Account, out.Reconnected = account, reconnected
	if err := uc.d.Alerts.Healthy(ctx, account); err != nil {
		log.Printf("[tiktok] health alerts of %s not resolved: %v", account.ID, err)
	}
	return out, nil
}

func (uc *ConnectUseCase) complete(ctx context.Context, workspaceID string, in CompleteConnectInput) (*ttdomain.Account, bool, error) {
	if failure := strings.TrimSpace(in.Error + " " + in.ErrorDescription); failure != "" {
		return nil, false, fmt.Errorf("%w: %s", ErrAuthorizationDeclined, failure)
	}
	if strings.TrimSpace(in.Code) == "" {
		return nil, false, ErrAuthorizationCode
	}
	grant, err := uc.d.Services.OAuth.ExchangeCode(ctx, in.Code)
	if err != nil {
		return nil, false, fmt.Errorf("tiktok: exchange code: %w", err)
	}
	if err := grant.Validate(); err != nil {
		return nil, false, err
	}
	info, err := uc.d.Services.OAuth.TokenInfo(ctx, grant.AccessToken)
	if err != nil {
		return nil, false, fmt.Errorf("tiktok: read the granted scopes: %w", err)
	}
	if info.OpenID != "" && info.OpenID != grant.OpenID {
		return nil, false, ttdomain.ErrIdentityMismatch
	}
	creds := ttdomain.Credentials{OpenID: grant.OpenID, AccessToken: grant.AccessToken}
	profile, err := uc.d.Services.Profiles.Profile(ctx, creds)
	if err != nil {
		return nil, false, fmt.Errorf("tiktok: read the connected account: %w", err)
	}
	caps, reason, err := ProbeCapabilities(ctx, uc.d.Services, creds, info.Scopes, profile)
	if err != nil {
		return nil, false, err
	}
	if !caps.Useful() {
		if !ttdomain.MessagingEligible(profile.HasAdvancedAccess, profile.IsBusinessAccount) && hasMessagingScopes(info.Scopes) {
			return nil, false, ttdomain.ErrNotEligibleAccount
		}
		return nil, false, ttdomain.ErrScopeMissing
	}

	now := uc.now()
	accessExpires := now.Add(grant.ExpiresIn)
	refreshExpires := now.Add(grant.RefreshExpiresIn)
	account := &ttdomain.Account{
		WorkspaceID:       workspaceID,
		OpenID:            grant.OpenID,
		Username:          profile.Username,
		DisplayName:       profile.DisplayName,
		HasAdvancedAccess: profile.HasAdvancedAccess,
		IsBusinessAccount: profile.IsBusinessAccount,
		AccessToken:       grant.AccessToken,
		RefreshToken:      grant.RefreshToken,
		AccessExpiresAt:   &accessExpires,
		RefreshExpiresAt:  &refreshExpires,
		GrantedScopes:     info.Scopes,
		Capabilities:      caps,
		Status:            ttdomain.StatusConnected,
		StatusReason:      reason,
	}
	reconnected, err := uc.persist(ctx, account)
	if err != nil {
		return nil, false, err
	}
	uc.rehostAvatar(ctx, account, profile.AvatarURL)
	uc.ensureWebhooks(ctx, account)
	return account, reconnected, nil
}

func hasMessagingScopes(scopes []string) bool {
	for _, s := range scopes {
		if s == ttdomain.ScopeMessageRead {
			return true
		}
	}
	return false
}

func ProbeCapabilities(ctx context.Context, svc Services, creds ttdomain.Credentials, scopes []string, profile ttdomain.BusinessProfile) (ttdomain.AccountCapabilities, string, error) {
	caps := ttdomain.CapabilitiesFrom(scopes, ttdomain.MessagingEligible(profile.HasAdvancedAccess, profile.IsBusinessAccount))
	if !caps.CanReceiveMessages {
		return caps, "", nil
	}
	err := svc.Messaging.ProbeInbox(ctx, creds)
	switch {
	case err == nil:
		return caps, "", nil
	case svc.Errors.IsPermission(err) || svc.Errors.IsEligibility(err):
		caps.CanReceiveMessages, caps.CanSendMessages, caps.CanManageInboxProfile = false, false, false
		return caps, "TikTok refused direct messages for this account (region or account type)", nil
	case svc.Errors.IsAuth(err):
		return caps, "", fmt.Errorf("%w: %w", ttdomain.ErrNeedsReconnect, err)
	}
	return caps, "", fmt.Errorf("%w: %w", ErrProbeInconclusive, err)
}

func (uc *ConnectUseCase) persist(ctx context.Context, account *ttdomain.Account) (bool, error) {
	live, err := uc.d.Accounts.FindLiveByOpenID(ctx, account.OpenID)
	switch {
	case err == nil && live.WorkspaceID != account.WorkspaceID:
		return false, ttdomain.ErrAccountAlreadyLinked
	case err == nil:
		return true, uc.reconnect(ctx, live, account)
	case !errors.Is(err, ttdomain.ErrAccountNotFound):
		return false, err
	}
	previous, err := uc.d.Accounts.FindByWorkspaceAndOpenIDUnscoped(ctx, account.WorkspaceID, account.OpenID)
	switch {
	case err == nil:
		return true, uc.reconnect(ctx, previous, account)
	case !errors.Is(err, ttdomain.ErrAccountNotFound):
		return false, err
	}
	return false, uc.d.Accounts.Create(ctx, account)
}

func (uc *ConnectUseCase) reconnect(ctx context.Context, existing, account *ttdomain.Account) error {
	account.ID = existing.ID
	account.CreatedAt = existing.CreatedAt
	account.AvatarKey = existing.AvatarKey
	if err := uc.d.Accounts.Reconnect(ctx, account); err != nil {
		return err
	}
	fresh, err := uc.d.Accounts.FindByID(ctx, account.ID)
	if err != nil {
		return err
	}
	*account = *fresh
	return nil
}

func (uc *ConnectUseCase) rehostAvatar(ctx context.Context, account *ttdomain.Account, sourceURL string) {
	key, err := uc.d.Images.Rehost(ctx, AccountAvatarKey(account.ID), sourceURL)
	if err != nil {
		log.Printf("[tiktok] avatar of %s not stored: %v", account.ID, err)
		return
	}
	if key == "" {
		return
	}
	account.AvatarKey = key
	if err := uc.d.Accounts.UpdateProfile(ctx, account.ID, profileUpdateOf(account)); err != nil {
		log.Printf("[tiktok] avatar key of %s not saved: %v", account.ID, err)
	}
}

func (uc *ConnectUseCase) ensureWebhooks(ctx context.Context, account *ttdomain.Account) {
	healthy, err := uc.d.Webhooks.Ensure(ctx)
	if err != nil {
		log.Printf("[tiktok] webhooks not confirmed while connecting %s: %v", account.ID, err)
	}
	account.WebhooksHealthy = healthy && err == nil
	if err := uc.d.Accounts.SetWebhooksHealthy(ctx, account.ID, account.WebhooksHealthy); err != nil {
		log.Printf("[tiktok] webhook health of %s not saved: %v", account.ID, err)
	}
	if !account.WebhooksHealthy {
		if err := uc.d.Alerts.WebhooksUnhealthy(ctx, account); err != nil {
			log.Printf("[tiktok] webhook alert of %s not published: %v", account.ID, err)
		}
	}
}

func profileUpdateOf(a *ttdomain.Account) ttdomain.ProfileUpdate {
	return ttdomain.ProfileUpdate{
		Username:          a.Username,
		DisplayName:       a.DisplayName,
		AvatarKey:         a.AvatarKey,
		HasAdvancedAccess: a.HasAdvancedAccess,
		IsBusinessAccount: a.IsBusinessAccount,
		GrantedScopes:     a.GrantedScopes,
		Capabilities:      a.Capabilities,
	}
}
