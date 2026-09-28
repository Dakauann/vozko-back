package facebook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/usecases/shared/oauthstate"
)

const ReasonWebhookSubscriptionFailed = "webhook_subscription_failed"

type Outcome string

const (
	OutcomeConnected       Outcome = "connected"
	OutcomeReconnected     Outcome = "reconnected"
	OutcomeLinkedElsewhere Outcome = "already_linked_elsewhere"
	OutcomeNoAccess        Outcome = "no_access"
)

type PicturePort interface {
	StorePagePicture(ctx context.Context, pageID, url string) (storageKey string, err error)
}

type StartConnectInput struct {
	WorkspaceID string
	UserID      string
	ReturnPath  string
	Popup       bool
}

type StartConnectOutput struct {
	AuthorizeURL string
	State        string
}

type CompleteConnectInput struct {
	Code        string
	State       string
	Error       string
	ErrorReason string
}

type PageOutcome struct {
	PageID   string
	FBPageID string
	Name     string
	Outcome  Outcome
	Missing  []fbdomain.Capability
	Warning  string
}

type CompleteConnectOutput struct {
	Pages      []PageOutcome
	ReturnPath string
	Popup      bool
}

func (o *CompleteConnectOutput) ConnectedCount() int {
	n := 0
	for _, p := range o.Pages {
		if p.Outcome == OutcomeConnected || p.Outcome == OutcomeReconnected {
			n++
		}
	}
	return n
}

type ConnectPagesUseCase struct {
	oauth        fbdomain.OAuthService
	subscription fbdomain.SubscriptionService
	grants       fbdomain.GrantRepository
	pages        fbdomain.PageRepository
	pictures     PicturePort
	states       *oauthstate.Issuer
	now          func() time.Time
}

func NewConnectPagesUseCase(
	oauth fbdomain.OAuthService,
	subscription fbdomain.SubscriptionService,
	grants fbdomain.GrantRepository,
	pages fbdomain.PageRepository,
	pictures PicturePort,
	states *oauthstate.Issuer,
) *ConnectPagesUseCase {
	return &ConnectPagesUseCase{
		oauth:        oauth,
		subscription: subscription,
		grants:       grants,
		pages:        pages,
		pictures:     pictures,
		states:       states,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (uc *ConnectPagesUseCase) Start(_ context.Context, in StartConnectInput) (*StartConnectOutput, error) {
	state, err := uc.states.Issue(oauthstate.IssueInput{
		WorkspaceID: in.WorkspaceID,
		UserID:      in.UserID,
		ReturnPath:  in.ReturnPath,
		Popup:       in.Popup,
	})
	if err != nil {
		return nil, err
	}
	return &StartConnectOutput{AuthorizeURL: uc.oauth.BuildAuthorizeURL(state), State: state}, nil
}

func (uc *ConnectPagesUseCase) DefaultReturnPath() string { return uc.states.DefaultReturnPath() }

type ConnectError struct {
	Popup      bool
	ReturnPath string
	Err        error
}

func (e *ConnectError) Error() string { return e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

func (uc *ConnectPagesUseCase) Complete(ctx context.Context, in CompleteConnectInput) (*CompleteConnectOutput, error) {
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

func (uc *ConnectPagesUseCase) complete(ctx context.Context, state *oauthstate.OAuthState, in CompleteConnectInput) (*CompleteConnectOutput, error) {
	if in.Error != "" || in.ErrorReason != "" {

		return nil, fmt.Errorf("%w (%s/%s)", fbdomain.ErrAuthorizationDenied, in.Error, in.ErrorReason)
	}
	if strings.TrimSpace(in.Code) == "" {
		return nil, fmt.Errorf("facebook: authorization code is required")
	}

	token, err := uc.oauth.ExchangeCode(ctx, in.Code)
	if err != nil {
		return nil, fmt.Errorf("facebook: exchange code: %w", err)
	}
	debug, err := uc.oauth.DebugToken(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("facebook: inspect token: %w", err)
	}
	log.Printf("[facebook] connect token: valid=%t type=%q scopes=%v granular=%v",
		debug.Valid, debug.Type, debug.Scopes, debug.GranularScopes)
	if !debug.Valid {
		return nil, fmt.Errorf("facebook: the returned token is not valid")
	}
	remotePages, err := uc.oauth.ListPages(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("facebook: list pages: %w", err)
	}
	if err := debug.AdoptListedPages(pageIDsOf(remotePages)); err != nil {
		return nil, err
	}
	identity, err := uc.oauth.Identify(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("facebook: identify grant: %w", err)
	}

	now := uc.now()
	grant := &fbdomain.Grant{
		WorkspaceID:      state.WorkspaceID,
		ConnectedBy:      state.UserID,
		TokenKind:        tokenKind(debug.Type),
		AccessToken:      token.AccessToken,
		TokenExpiresAt:   token.ExpiresAt,
		AppScopedUserID:  identity.AppScopedUserID,
		ClientBusinessID: identity.ClientBusinessID,
		Scopes:           debug.Scopes,
		GranularScopes:   debug.GranularScopes,
		Status:           fbdomain.GrantActive,
		CheckedAt:        &now,
	}
	if err := uc.grants.Upsert(ctx, grant); err != nil {
		return nil, err
	}

	out := &CompleteConnectOutput{ReturnPath: uc.states.ReturnPath(state), Popup: state.Popup}
	for _, remote := range remotePages {
		log.Printf("[facebook] connect listed page %s %q tasks=%v granted=%t token=%t",
			remote.FBPageID, remote.Name, remote.Tasks, grant.Lists(remote.FBPageID), remote.AccessToken != "")
		if !grant.Lists(remote.FBPageID) {
			continue
		}
		outcome, err := uc.connectPage(ctx, state.WorkspaceID, grant, remote)
		if err != nil {
			return nil, err
		}
		out.Pages = append(out.Pages, outcome)
	}
	if len(out.Pages) == 0 {
		return nil, fbdomain.ErrNoPagesGranted
	}
	log.Printf("[facebook] grant %s connected %d of %d page(s) to workspace %s",
		grant.ID, out.ConnectedCount(), len(out.Pages), state.WorkspaceID)
	return out, nil
}

func (uc *ConnectPagesUseCase) connectPage(ctx context.Context, workspaceID string, grant *fbdomain.Grant, remote *fbdomain.RemotePage) (PageOutcome, error) {
	outcome := PageOutcome{FBPageID: remote.FBPageID, Name: remote.Name}
	if strings.TrimSpace(remote.AccessToken) == "" {
		outcome.Outcome = OutcomeNoAccess
		return outcome, nil
	}

	page := &fbdomain.Page{
		WorkspaceID:    workspaceID,
		GrantID:        grant.ID,
		FBPageID:       remote.FBPageID,
		Name:           remote.Name,
		Username:       remote.Username,
		Category:       remote.Category,
		Link:           remote.Link,
		FollowersCount: remote.FollowersCount,
		LinkedIGUserID: remote.LinkedIGUserID,
		PageToken:      remote.AccessToken,
		Tasks:          remote.Tasks,
		GrantedScopes:  grant.ScopesForPage(remote.FBPageID),
		Status:         fbdomain.StatusConnected,
	}
	page.Normalize()
	if err := page.Validate(); err != nil {
		return outcome, err
	}

	existing, err := uc.pages.FindByFBPageIDUnscoped(ctx, remote.FBPageID)
	switch {
	case err == nil && existing.WorkspaceID != workspaceID:
		outcome.Outcome = OutcomeLinkedElsewhere
		return outcome, nil
	case err == nil:
		page.ID = existing.ID
		page.PictureStorageKey = existing.PictureStorageKey
		if err := uc.pages.Restore(ctx, existing.ID); err != nil {
			return outcome, err
		}
		if err := uc.pages.Update(ctx, page); err != nil {
			return outcome, err
		}
		outcome.Outcome = OutcomeReconnected
	case errors.Is(err, fbdomain.ErrPageNotFound):
		if err := uc.pages.Create(ctx, page); err != nil {
			if errors.Is(err, fbdomain.ErrPageAlreadyLinked) {
				outcome.Outcome = OutcomeLinkedElsewhere
				return outcome, nil
			}
			return outcome, err
		}
		outcome.Outcome = OutcomeConnected
	default:
		return outcome, err
	}

	outcome.PageID = page.ID
	outcome.Missing = missingCapabilities(page)
	outcome.Warning = uc.subscribe(ctx, page)
	uc.storePicture(ctx, page, remote.PictureURL)
	return outcome, nil
}

func (uc *ConnectPagesUseCase) subscribe(ctx context.Context, page *fbdomain.Page) string {
	if !page.Can(fbdomain.CapSubscribe) {
		return ""
	}
	active, err := uc.subscription.Subscribe(ctx, page.FBPageID, page.PageToken, fbdomain.SubscribedFields())
	if err != nil {
		log.Printf("[facebook] webhook subscription failed page=%s: %v", page.FBPageID, err)
		if statusErr := uc.pages.UpdateStatus(ctx, page.ID, fbdomain.StatusConnected, ReasonWebhookSubscriptionFailed); statusErr != nil {
			log.Printf("[facebook] could not record the subscription failure for page %s: %v", page.FBPageID, statusErr)
		}
		return ReasonWebhookSubscriptionFailed
	}
	if err := uc.pages.UpdateSubscription(ctx, page.ID, active, uc.now()); err != nil {
		log.Printf("[facebook] could not record the subscription for page %s: %v", page.FBPageID, err)
	}
	return ""
}

func (uc *ConnectPagesUseCase) storePicture(ctx context.Context, page *fbdomain.Page, url string) {
	if url == "" || uc.pictures == nil {
		return
	}
	key, err := uc.pictures.StorePagePicture(ctx, page.ID, url)
	if err != nil {
		log.Printf("[facebook] could not store the picture for page %s: %v", page.FBPageID, err)
		return
	}
	if err := uc.pages.UpdateProfile(ctx, page.ID, &fbdomain.RemotePage{
		Name: page.Name, Username: page.Username, Category: page.Category, Link: page.Link,
		FollowersCount: page.FollowersCount, LinkedIGUserID: page.LinkedIGUserID,
	}, key, uc.now()); err != nil {
		log.Printf("[facebook] could not record the picture for page %s: %v", page.FBPageID, err)
	}
}

func missingCapabilities(page *fbdomain.Page) []fbdomain.Capability {
	var missing []fbdomain.Capability
	for _, c := range fbdomain.AllCapabilities() {
		if !page.Can(c) {
			missing = append(missing, c)
		}
	}
	return missing
}

func tokenKind(debugType string) fbdomain.TokenKind {
	if strings.EqualFold(debugType, "SYSTEM_USER") {
		return fbdomain.TokenSystemUser
	}
	return fbdomain.TokenUser
}

func pageIDsOf(pages []*fbdomain.RemotePage) []string {
	ids := make([]string, 0, len(pages))
	for _, p := range pages {
		ids = append(ids, p.FBPageID)
	}
	return ids
}
