package googleaccount

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/googleaccount"
	"vozko/usecases/shared/oauthstate"
)

const IntegrationsPath = "/dashboard/integrations"

type ProductHooks interface {
	AfterConnect(ctx context.Context, workspaceID string) error
	BeforeDisconnect(ctx context.Context, workspaceID string) error
}

type ConnectDeps struct {
	Accounts googleaccount.Repository
	OAuth    googleaccount.OAuthClient
	States   *oauthstate.SealedIssuer
	Alerts   Alerts
	Hooks    map[googleaccount.Product]ProductHooks
}

type Connector struct {
	accounts googleaccount.Repository
	oauth    googleaccount.OAuthClient
	states   *oauthstate.SealedIssuer
	alerts   Alerts
	hooks    map[googleaccount.Product]ProductHooks
	releaser releaser
	now      Clock
}

func NewConnector(d ConnectDeps) *Connector {
	if d.Accounts == nil || d.OAuth == nil || d.States == nil || d.Alerts == nil {
		panic("googleaccount: connect requires accounts, oauth, sealed state and alerts")
	}
	return &Connector{
		accounts: d.Accounts, oauth: d.OAuth, states: d.States, alerts: d.Alerts, hooks: d.Hooks,
		releaser: releaser{accounts: d.Accounts, oauth: d.OAuth}, now: utcNow,
	}
}

type StartInput struct {
	WorkspaceID string
	UserID      string
	Product     googleaccount.Product
	ReturnPath  string
	Popup       bool
}

func (c *Connector) Start(_ context.Context, in StartInput) (string, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return "", googleaccount.ErrWorkspaceRequired
	}
	if !in.Product.Valid() {
		return "", googleaccount.ErrUnknownProduct
	}
	state, err := c.states.Issue(oauthstate.IssueInput{
		WorkspaceID: in.WorkspaceID,
		UserID:      in.UserID,
		ReturnPath:  in.ReturnPath,
		Popup:       in.Popup,
	}, string(in.Product))
	if err != nil {
		return "", err
	}
	return c.oauth.AuthorizeURL(state, in.Product.AuthorizationScopes()), nil
}

type CompleteInput struct {
	Code  string
	State string
	Error string
}

type CompleteOutput struct {
	Product    googleaccount.Product
	Account    *googleaccount.Account
	ReturnPath string
	Popup      bool
}

func (c *Connector) Complete(ctx context.Context, in CompleteInput) (*CompleteOutput, error) {
	state, sealed, err := c.states.Redeem(in.State)
	if err != nil {
		return nil, err
	}
	out := &CompleteOutput{ReturnPath: c.states.ReturnPath(state), Popup: state.Popup}
	product, err := googleaccount.ParseProduct(sealed)
	if err != nil {
		return out, err
	}
	out.Product = product
	account, err := c.complete(ctx, state, product, in)
	if err != nil {
		log.Printf("[googleaccount] %s connect refused for workspace %s: %v", product, state.WorkspaceID, err)
		return out, err
	}
	out.Account = account
	if err := c.alerts.Reconnected(ctx, account, product); err != nil {
		log.Printf("[googleaccount] reconnect alert of %s not resolved: %v", account.ID, err)
	}
	if hooks := c.hooks[product]; hooks != nil {
		if err := hooks.AfterConnect(ctx, account.WorkspaceID); err != nil {
			log.Printf("[googleaccount] %s follow-up after connect failed for workspace %s: %v", product, account.WorkspaceID, err)
		}
	}
	return out, nil
}

func (c *Connector) complete(ctx context.Context, state *oauthstate.OAuthState, product googleaccount.Product, in CompleteInput) (*googleaccount.Account, error) {
	if failure := strings.TrimSpace(in.Error); failure != "" {
		return nil, fmt.Errorf("%w: %s", googleaccount.ErrAuthorizationError, failure)
	}
	if strings.TrimSpace(in.Code) == "" {
		return nil, googleaccount.ErrAuthorizationCode
	}
	grant, err := c.oauth.ExchangeCode(ctx, in.Code)
	if err != nil {
		return nil, fmt.Errorf("googleaccount: exchange code: %w", err)
	}
	if err := grant.Validate(); err != nil {
		return nil, err
	}
	if !googleaccount.GrantCovers(grant.Scopes, product) {
		return nil, fmt.Errorf("%w: granted %v", googleaccount.ErrScopeNotGranted, grant.Scopes)
	}
	profile, err := c.oauth.Profile(ctx, grant.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("googleaccount: read the connected profile: %w", err)
	}
	existing, err := c.accounts.FindByEmail(ctx, state.WorkspaceID, profile.Email)
	if err != nil && !errors.Is(err, googleaccount.ErrAccountNotFound) {
		return nil, err
	}
	account := &googleaccount.Account{
		WorkspaceID:     state.WorkspaceID,
		Email:           profile.Email,
		AccessToken:     grant.AccessToken,
		RefreshToken:    grant.RefreshToken,
		AccessExpiresAt: c.now().Add(grant.ExpiresIn),
		Scopes:          grant.Scopes,
		ConnectedBy:     state.UserID,
	}
	if existing != nil && existing.Status == googleaccount.StatusConnected {
		account.Scopes = googleaccount.MergeScopes(existing.Scopes, grant.Scopes)
		if strings.TrimSpace(account.RefreshToken) == "" {
			account.RefreshToken = existing.RefreshToken
		}
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		return nil, googleaccount.ErrNoRefreshToken
	}
	previous, err := c.accounts.FindBound(ctx, state.WorkspaceID, product)
	if err != nil && !errors.Is(err, googleaccount.ErrNotConnected) {
		return nil, err
	}
	if err := c.accounts.SaveConnected(ctx, account); err != nil {
		return nil, err
	}
	if err := c.accounts.Bind(ctx, googleaccount.Binding{WorkspaceID: state.WorkspaceID, Product: product, AccountID: account.ID}); err != nil {
		return nil, err
	}
	if previous != nil && previous.ID != account.ID {
		if err := c.releaser.release(ctx, previous); err != nil {
			log.Printf("[googleaccount] previous account %s of %s not released: %v", previous.ID, product, err)
		}
	}
	return account, nil
}

type Disconnector struct {
	accounts googleaccount.Repository
	alerts   Alerts
	hooks    map[googleaccount.Product]ProductHooks
	releaser releaser
}

func NewDisconnector(accounts googleaccount.Repository, oauth googleaccount.OAuthClient, alerts Alerts, hooks map[googleaccount.Product]ProductHooks) *Disconnector {
	if accounts == nil || oauth == nil || alerts == nil {
		panic("googleaccount: disconnect requires accounts, oauth and alerts")
	}
	return &Disconnector{accounts: accounts, alerts: alerts, hooks: hooks, releaser: releaser{accounts: accounts, oauth: oauth}}
}

func (d *Disconnector) Disconnect(ctx context.Context, workspaceID string, product googleaccount.Product) error {
	if strings.TrimSpace(workspaceID) == "" {
		return googleaccount.ErrWorkspaceRequired
	}
	if !product.Valid() {
		return googleaccount.ErrUnknownProduct
	}
	if hooks := d.hooks[product]; hooks != nil {
		if err := hooks.BeforeDisconnect(ctx, workspaceID); err != nil {
			log.Printf("[googleaccount] %s clean up before disconnect failed for workspace %s: %v", product, workspaceID, err)
		}
	}
	accountID, err := d.accounts.Unbind(ctx, workspaceID, product)
	if err != nil {
		return err
	}
	account, err := d.accounts.FindByID(ctx, accountID)
	if err != nil {
		return err
	}
	if err := d.alerts.Reconnected(ctx, account, product); err != nil {
		log.Printf("[googleaccount] reconnect alert of %s not resolved: %v", account.ID, err)
	}
	return d.releaser.release(ctx, account)
}

type releaser struct {
	accounts googleaccount.Repository
	oauth    googleaccount.OAuthClient
}

func (r releaser) release(ctx context.Context, account *googleaccount.Account) error {
	bindings, err := r.accounts.CountBindings(ctx, account.ID)
	if err != nil {
		return err
	}
	if bindings > 0 {
		return nil
	}
	elsewhere, err := r.accounts.ConnectedElsewhere(ctx, account.Email, account.ID)
	if err != nil {
		return err
	}
	if !elsewhere && strings.TrimSpace(account.RefreshToken) != "" {
		if err := r.oauth.Revoke(ctx, account.RefreshToken); err != nil {
			log.Printf("[googleaccount] revoke of %s failed, the grant stays at Google until the user removes it: %v", account.ID, err)
		}
	}
	return r.accounts.Disconnect(ctx, account.ID)
}

type ProductStatus struct {
	Product      googleaccount.Product
	Connected    bool
	Status       googleaccount.Status
	Email        string
	ConnectedAt  *time.Time
	StatusReason string
}

type StatusReader struct {
	accounts googleaccount.Repository
}

func NewStatusReader(accounts googleaccount.Repository) *StatusReader {
	if accounts == nil {
		panic("googleaccount: status requires accounts")
	}
	return &StatusReader{accounts: accounts}
}

func (s *StatusReader) Status(ctx context.Context, workspaceID string) ([]ProductStatus, error) {
	bound, err := s.accounts.ListBound(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	byProduct := map[googleaccount.Product]*googleaccount.Account{}
	for _, b := range bound {
		byProduct[b.Product] = b.Account
	}
	out := make([]ProductStatus, 0, len(googleaccount.Products()))
	for _, p := range googleaccount.Products() {
		status := ProductStatus{Product: p, Status: googleaccount.StatusDisconnected}
		if a := byProduct[p]; a != nil {
			status.Status = a.Status
			status.Connected = a.Usable() == nil
			status.Email = a.Email
			status.StatusReason = a.StatusReason
			connectedAt := a.UpdatedAt.UTC()
			status.ConnectedAt = &connectedAt
		}
		out = append(out, status)
	}
	return out, nil
}

func (s *StatusReader) ProductStatus(ctx context.Context, workspaceID string, product googleaccount.Product) (ProductStatus, error) {
	all, err := s.Status(ctx, workspaceID)
	if err != nil {
		return ProductStatus{}, err
	}
	for _, st := range all {
		if st.Product == product {
			return st, nil
		}
	}
	return ProductStatus{}, googleaccount.ErrUnknownProduct
}
