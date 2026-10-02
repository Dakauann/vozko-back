package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	ads "vozko/domain/advertising"
	fbdomain "vozko/domain/facebook"
	"vozko/usecases/shared/oauthstate"
)

type OAuth interface {
	BuildAuthorizeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*fbdomain.TokenGrant, error)
	DebugToken(ctx context.Context, token string) (*fbdomain.TokenDebug, error)
	Identify(ctx context.Context, token string, kind fbdomain.TokenKind) (*fbdomain.GrantIdentity, error)
}

type ConnectOutcome string

const (
	OutcomeConnected       ConnectOutcome = "connected"
	OutcomeLinkedElsewhere ConnectOutcome = "already_linked_elsewhere"
)

type StartConnectInput struct {
	WorkspaceID string
	UserID      string
	ReturnPath  string
	Popup       bool
}

type CompleteConnectInput struct {
	Code        string
	State       string
	Error       string
	ErrorReason string
}

type AccountOutcome struct {
	AccountID     string
	MetaAccountID string
	Name          string
	Outcome       ConnectOutcome
}

type CompleteConnectOutput struct {
	Accounts   []AccountOutcome
	ReturnPath string
	Popup      bool
}

func (o *CompleteConnectOutput) ConnectedCount() int {
	n := 0
	for _, a := range o.Accounts {
		if a.Outcome == OutcomeConnected {
			n++
		}
	}
	return n
}

type ConnectError struct {
	Popup      bool
	ReturnPath string
	Err        error
}

func (e *ConnectError) Error() string { return e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

type connectGateway interface {
	ListAdAccounts(ctx context.Context, token string) ([]ads.RemoteAdAccount, error)
	SubscribeAccount(ctx context.Context, token, metaAccountID string) error
}

type ConnectUseCase struct {
	oauth    OAuth
	gateway  connectGateway
	grants   ads.GrantRepository
	accounts ads.AccountRepository
	states   *oauthstate.Issuer
	now      func() time.Time
}

func NewConnectUseCase(oauth OAuth, gateway connectGateway, grants ads.GrantRepository, accounts ads.AccountRepository, states *oauthstate.Issuer) *ConnectUseCase {
	return &ConnectUseCase{
		oauth: oauth, gateway: gateway, grants: grants, accounts: accounts, states: states,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (uc *ConnectUseCase) Start(in StartConnectInput) (string, error) {
	state, err := uc.states.Issue(oauthstate.IssueInput{
		WorkspaceID: in.WorkspaceID, UserID: in.UserID, ReturnPath: in.ReturnPath, Popup: in.Popup,
	})
	if err != nil {
		return "", err
	}
	return uc.oauth.BuildAuthorizeURL(state), nil
}

func (uc *ConnectUseCase) DefaultReturnPath() string { return uc.states.DefaultReturnPath() }

func (uc *ConnectUseCase) Complete(ctx context.Context, in CompleteConnectInput) (*CompleteConnectOutput, error) {
	state, err := uc.states.Redeem(in.State)
	if err != nil {
		return nil, err
	}
	out, err := uc.complete(ctx, state, in)
	if err != nil {
		return nil, &ConnectError{Popup: state.Popup, ReturnPath: uc.states.ReturnPath(state), Err: err}
	}
	return out, nil
}

func (uc *ConnectUseCase) complete(ctx context.Context, state *oauthstate.OAuthState, in CompleteConnectInput) (*CompleteConnectOutput, error) {
	if in.Error != "" || in.ErrorReason != "" {
		return nil, fmt.Errorf("%w (%s/%s)", ads.ErrAuthorizationDenied, in.Error, in.ErrorReason)
	}
	if strings.TrimSpace(in.Code) == "" {
		return nil, fmt.Errorf("ads: authorization code is required")
	}
	token, err := uc.oauth.ExchangeCode(ctx, in.Code)
	if err != nil {
		return nil, fmt.Errorf("ads: exchange code: %w", err)
	}
	debug, err := uc.oauth.DebugToken(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("ads: inspect token: %w", err)
	}
	if !debug.Valid {
		return nil, fmt.Errorf("ads: the returned token is not valid")
	}
	identity, err := uc.oauth.Identify(ctx, token.AccessToken, debug.Kind())
	if err != nil {
		return nil, fmt.Errorf("ads: identify grant: %w", err)
	}
	now := uc.now()
	grant := &ads.Grant{
		WorkspaceID:      state.WorkspaceID,
		ConnectedBy:      state.UserID,
		TokenKind:        debug.Kind(),
		AccessToken:      token.AccessToken,
		TokenExpiresAt:   token.ExpiresAt,
		AppScopedUserID:  identity.AppScopedUserID,
		ClientBusinessID: identity.ClientBusinessID,
		Scopes:           debug.Scopes,
		GranularScopes:   debug.GranularScopes,
		Status:           ads.GrantActive,
		CheckedAt:        &now,
	}
	if err := grant.Usable(now); err != nil {
		return nil, err
	}
	remote, err := uc.gateway.ListAdAccounts(ctx, grant.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("ads: list ad accounts: %w", err)
	}
	if err := uc.grants.Upsert(ctx, grant); err != nil {
		return nil, err
	}
	out := &CompleteConnectOutput{ReturnPath: uc.states.ReturnPath(state), Popup: state.Popup}
	for _, r := range remote {
		if !grant.Allows(ads.ScopeAdsManagement, r.MetaAccountID) {
			continue
		}
		outcome, err := uc.connectAccount(ctx, grant, r)
		if err != nil {
			return nil, err
		}
		out.Accounts = append(out.Accounts, outcome)
	}
	if len(out.Accounts) == 0 {
		return nil, ads.ErrNoAccountsGranted
	}
	log.Printf("[ads] grant %s connected %d of %d ad account(s) to workspace %s",
		grant.ID, out.ConnectedCount(), len(out.Accounts), state.WorkspaceID)
	return out, nil
}

func (uc *ConnectUseCase) connectAccount(ctx context.Context, grant *ads.Grant, remote ads.RemoteAdAccount) (AccountOutcome, error) {
	account := &ads.AdAccount{WorkspaceID: grant.WorkspaceID, GrantID: grant.ID, Connection: ads.ConnectionConnected}
	applyRemote(account, remote)
	outcome := AccountOutcome{MetaAccountID: account.MetaAccountID, Name: account.Name}
	err := uc.accounts.Upsert(ctx, account)
	switch {
	case errors.Is(err, ads.ErrAccountLinkedElsewhere):
		outcome.Outcome = OutcomeLinkedElsewhere
		return outcome, nil
	case err != nil:
		return outcome, err
	}
	outcome.AccountID = account.ID
	outcome.Outcome = OutcomeConnected
	if account.CanManage() != nil {
		return outcome, nil
	}
	if err := uc.gateway.SubscribeAccount(ctx, grant.AccessToken, account.MetaAccountID); err != nil {
		log.Printf("[ads] account %s connected without review webhooks (the sync still refreshes it): %v", account.MetaAccountID, err)
	}
	return outcome, nil
}

func applyRemote(account *ads.AdAccount, remote ads.RemoteAdAccount) {
	account.MetaAccountID = ads.NormalizeAccountID(remote.MetaAccountID)
	account.Name = remote.Name
	account.BusinessID = remote.BusinessID
	account.BusinessName = remote.BusinessName
	account.Currency = remote.Currency
	account.Timezone = remote.Timezone
	account.MetaStatus = remote.Status
	account.DisableReason = remote.DisableReason
	account.HasFunding = remote.HasFunding
	account.AmountSpent = remote.AmountSpent
	account.SpendCap = remote.SpendCap
	account.Tasks = remote.Tasks
}
