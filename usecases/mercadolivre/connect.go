package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	mldomain "vozko/domain/mercadolivre"
	"vozko/usecases/shared/oauthstate"
)

const operatorUserMarker = "invalid_operator_user_id"

type ConnectDeps struct {
	Accounts mldomain.AccountRepository
	Services Services
	States   *oauthstate.SealedIssuer
	PKCE     mldomain.PKCE
	Alerts   AccountAlerts
	Metrics  Metrics
}

type ConnectUseCase struct {
	accounts mldomain.AccountRepository
	svc      Services
	states   *oauthstate.SealedIssuer
	pkce     mldomain.PKCE
	alerts   AccountAlerts
	metrics  Metrics
	now      Clock
}

func NewConnectUseCase(d ConnectDeps) *ConnectUseCase {
	if d.Accounts == nil || d.States == nil || d.PKCE == nil || d.Alerts == nil || d.Metrics == nil ||
		d.Services.OAuth == nil || d.Services.Users == nil || d.Services.Questions == nil || d.Services.Messages == nil || d.Services.Errors == nil {
		panic("mercadolivre: connect requires accounts, oauth, users, questions, messages, an error classifier, sealed state, pkce, alerts and metrics")
	}
	return &ConnectUseCase{accounts: d.Accounts, svc: d.Services, states: d.States, pkce: d.PKCE, alerts: d.Alerts, metrics: d.Metrics, now: utcNow}
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
		return nil, mldomain.ErrWorkspaceIDRequired
	}
	verifier, err := uc.pkce.NewVerifier()
	if err != nil {
		return nil, err
	}
	challenge, err := uc.pkce.Challenge(verifier)
	if err != nil {
		return nil, err
	}
	state, err := uc.states.Issue(oauthstate.IssueInput{
		WorkspaceID: in.WorkspaceID,
		UserID:      in.UserID,
		ReturnPath:  in.ReturnPath,
		Popup:       in.Popup,
	}, verifier)
	if err != nil {
		return nil, err
	}
	target, err := uc.svc.OAuth.AuthorizeURL(state, challenge)
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
	Account     *mldomain.Account
	ReturnPath  string
	Popup       bool
	Reconnected bool
}

func (uc *ConnectUseCase) Complete(ctx context.Context, in CompleteConnectInput) (*CompleteConnectOutput, error) {
	state, verifier, err := uc.states.Redeem(in.State)
	if err != nil {
		uc.metrics.AddMercadoLivreEvent("connect", "state_refused")
		return nil, err
	}
	out := &CompleteConnectOutput{ReturnPath: uc.states.ReturnPath(state), Popup: state.Popup}
	account, reconnected, err := uc.complete(ctx, state.WorkspaceID, verifier, in)
	if err != nil {
		uc.metrics.AddMercadoLivreEvent("connect", "refused")
		log.Printf("[mercadolivre] connect refused for workspace %s: %v", state.WorkspaceID, err)
		return out, err
	}
	uc.metrics.AddMercadoLivreEvent("connect", "connected")
	out.Account, out.Reconnected = account, reconnected
	if err := uc.alerts.Reconnected(ctx, account); err != nil {
		log.Printf("[mercadolivre] reconnect alert of %s not resolved: %v", account.ID, err)
	}
	return out, nil
}

func (uc *ConnectUseCase) complete(ctx context.Context, workspaceID, verifier string, in CompleteConnectInput) (*mldomain.Account, bool, error) {
	if failure := strings.TrimSpace(in.Error + " " + in.ErrorDescription); failure != "" {
		if strings.Contains(failure, operatorUserMarker) {
			return nil, false, mldomain.ErrNotAccountAdmin
		}
		return nil, false, fmt.Errorf("%w: %s", ErrAuthorizationDeclined, failure)
	}
	if strings.TrimSpace(in.Code) == "" {
		return nil, false, ErrAuthorizationCode
	}
	grant, err := uc.svc.OAuth.ExchangeCode(ctx, in.Code, verifier)
	if err != nil {
		if uc.svc.Errors.IsOperatorUser(err) {
			return nil, false, mldomain.ErrNotAccountAdmin
		}
		return nil, false, fmt.Errorf("mercadolivre: exchange code: %w", err)
	}
	if err := grant.Validate(); err != nil {
		if errors.Is(err, mldomain.ErrScopeMissing) {
			return nil, false, fmt.Errorf("%w: %w", mldomain.ErrPermissionMissing, err)
		}
		return nil, false, err
	}
	creds := mldomain.Credentials{UserID: grant.UserID, AccessToken: grant.AccessToken}
	me, err := uc.svc.Users.Me(ctx, creds)
	if err != nil {
		if uc.svc.Errors.IsOperatorUser(err) {
			return nil, false, mldomain.ErrNotAccountAdmin
		}
		return nil, false, fmt.Errorf("mercadolivre: read the connected user: %w", err)
	}
	if me.ID != grant.UserID {
		return nil, false, ErrIdentityMismatch
	}
	if !mldomain.SupportedSite(me.SiteID) {
		return nil, false, fmt.Errorf("%w: %q", mldomain.ErrSiteNotSupported, me.SiteID)
	}
	caps, err := probeCapabilities(ctx, uc.svc, creds, grant.Scopes)
	if err != nil {
		return nil, false, err
	}
	if !requiredCapabilitiesGranted(caps) {
		return nil, false, fmt.Errorf("%w: %+v", mldomain.ErrPermissionMissing, caps)
	}

	now := uc.now()
	accessExpires := now.Add(grant.ExpiresIn)
	refreshExpires := now.Add(mldomain.RefreshTokenLifetime)
	account := &mldomain.Account{
		WorkspaceID:      workspaceID,
		MLUserID:         grant.UserID,
		SiteID:           me.SiteID,
		AccessToken:      grant.AccessToken,
		RefreshToken:     grant.RefreshToken,
		AccessExpiresAt:  &accessExpires,
		RefreshExpiresAt: &refreshExpires,
		GrantedScopes:    grant.Scopes,
		Status:           mldomain.StatusConnected,
	}
	account.RefreshNames(*me)
	account.SetCapabilities(caps)
	reconnected, err := uc.persist(ctx, account)
	if err != nil {
		return nil, false, err
	}
	return account, reconnected, nil
}

func (uc *ConnectUseCase) persist(ctx context.Context, account *mldomain.Account) (bool, error) {
	live, err := uc.accounts.FindLiveByMLUserID(ctx, account.MLUserID)
	switch {
	case err == nil && live.WorkspaceID != account.WorkspaceID:
		return false, mldomain.ErrAccountAlreadyLinked
	case err == nil:
		return true, uc.reconnect(ctx, live, account)
	case !errors.Is(err, mldomain.ErrAccountNotFound):
		return false, err
	}
	previous, err := uc.accounts.FindByWorkspaceAndMLUserIDUnscoped(ctx, account.WorkspaceID, account.MLUserID)
	switch {
	case err == nil:
		return true, uc.reconnect(ctx, previous, account)
	case !errors.Is(err, mldomain.ErrAccountNotFound):
		return false, err
	}
	return false, uc.accounts.Create(ctx, account)
}

func (uc *ConnectUseCase) reconnect(ctx context.Context, existing, account *mldomain.Account) error {
	account.ID = existing.ID
	account.CreatedAt = existing.CreatedAt
	if err := uc.accounts.Reconnect(ctx, account); err != nil {
		return err
	}
	fresh, err := uc.accounts.FindByID(ctx, account.ID)
	if err != nil {
		return err
	}
	*account = *fresh
	return nil
}
