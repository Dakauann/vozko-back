package facebook

import (
	"context"
	"log"
	"time"

	fbdomain "vozko/domain/facebook"
)

const healthBatchSize = 200

type RoutingProbe interface {
	Probe(ctx context.Context, page *fbdomain.Page)
}

type HealthCheckUseCase struct {
	oauth        fbdomain.OAuthService
	subscription fbdomain.SubscriptionService
	grants       fbdomain.GrantRepository
	pages        fbdomain.PageRepository
	routing      RoutingProbe
	now          func() time.Time
}

func NewHealthCheckUseCase(
	oauth fbdomain.OAuthService,
	subscription fbdomain.SubscriptionService,
	grants fbdomain.GrantRepository,
	pages fbdomain.PageRepository,
	routing RoutingProbe,
) *HealthCheckUseCase {
	return &HealthCheckUseCase{
		oauth: oauth, subscription: subscription, grants: grants, pages: pages, routing: routing,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (uc *HealthCheckUseCase) Execute(ctx context.Context) error {
	grants, err := uc.grants.ListActive(ctx, healthBatchSize)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		uc.checkGrant(ctx, grant)
	}
	return nil
}

func (uc *HealthCheckUseCase) checkGrant(ctx context.Context, grant *fbdomain.Grant) {
	debug, err := uc.oauth.DebugToken(ctx, grant.AccessToken)
	if err != nil {
		if failure := fbdomain.Classify(err); failure == fbdomain.FailureReauth || failure == fbdomain.FailureRoleLost {
			uc.revokeGrant(ctx, grant, "facebook rejected the integration token")
			return
		}
		log.Printf("[facebook-health] grant %s check skipped: %v", grant.ID, err)
		return
	}
	if !debug.Valid {
		uc.revokeGrant(ctx, grant, "the integration was removed from Facebook")
		return
	}
	remote, err := uc.oauth.ListPages(ctx, grant.AccessToken)
	if err != nil {
		log.Printf("[facebook-health] grant %s pages could not be listed: %v", grant.ID, err)
		return
	}
	if err := debug.AdoptListedPages(pageIDsOf(remote)); err != nil {
		uc.revokeGrant(ctx, grant, "the integration no longer grants access to Pages")
		return
	}
	grant.Scopes, grant.GranularScopes = debug.Scopes, debug.GranularScopes
	if err := uc.grants.MarkChecked(ctx, grant.ID, debug.Scopes, debug.GranularScopes, uc.now()); err != nil {
		log.Printf("[facebook-health] grant %s could not be marked checked: %v", grant.ID, err)
	}
	byID := make(map[string]*fbdomain.RemotePage, len(remote))
	for _, r := range remote {
		byID[r.FBPageID] = r
	}

	pages, err := uc.pages.ListByGrant(ctx, grant.ID)
	if err != nil {
		log.Printf("[facebook-health] grant %s pages could not be loaded: %v", grant.ID, err)
		return
	}
	for _, page := range pages {
		uc.checkPage(ctx, grant, page, byID[page.FBPageID])
	}
}

func (uc *HealthCheckUseCase) checkPage(ctx context.Context, grant *fbdomain.Grant, page *fbdomain.Page, remote *fbdomain.RemotePage) {
	if remote == nil || remote.AccessToken == "" || !grant.Lists(page.FBPageID) {
		uc.setStatus(ctx, page, fbdomain.StatusTokenRevoked, "the page is no longer shared with Vozko")
		return
	}
	page.PageToken, page.Tasks, page.GrantedScopes = remote.AccessToken, remote.Tasks, grant.ScopesForPage(page.FBPageID)
	if err := uc.pages.UpdateToken(ctx, page.ID, grant.ID, page.PageToken, page.GrantedScopes, page.Tasks); err != nil {
		log.Printf("[facebook-health] page %s token could not be refreshed: %v", page.FBPageID, err)
		return
	}
	if page.Status != fbdomain.StatusConnected && page.Status != fbdomain.StatusRestricted {
		uc.setStatus(ctx, page, fbdomain.StatusConnected, "")
		page.Status = fbdomain.StatusConnected
	}
	if err := uc.pages.UpdateProfile(ctx, page.ID, remote, "", uc.now()); err != nil {
		log.Printf("[facebook-health] page %s profile could not be refreshed: %v", page.FBPageID, err)
	}
	uc.ensureSubscription(ctx, page)
	if uc.routing != nil && page.Can(fbdomain.CapMessaging) {
		uc.routing.Probe(ctx, page)
	}
}

func (uc *HealthCheckUseCase) ensureSubscription(ctx context.Context, page *fbdomain.Page) {
	if !page.Can(fbdomain.CapSubscribe) {
		return
	}
	active, err := uc.subscription.ActiveFields(ctx, page.FBPageID, page.PageToken)
	if err != nil {
		log.Printf("[facebook-health] page %s subscription could not be read: %v", page.FBPageID, err)
		return
	}
	if len(missingFields(fbdomain.SubscribedFields(), active)) == 0 {
		return
	}
	confirmed, err := uc.subscription.Subscribe(ctx, page.FBPageID, page.PageToken, fbdomain.SubscribedFields())
	if err != nil {
		log.Printf("[facebook-health] page %s could not be re-subscribed: %v", page.FBPageID, err)
		uc.setStatus(ctx, page, page.Status, ReasonWebhookSubscriptionFailed)
		return
	}
	if err := uc.pages.UpdateSubscription(ctx, page.ID, confirmed, uc.now()); err != nil {
		log.Printf("[facebook-health] page %s subscription could not be recorded: %v", page.FBPageID, err)
	}
}

func (uc *HealthCheckUseCase) revokeGrant(ctx context.Context, grant *fbdomain.Grant, reason string) {
	if err := uc.grants.Revoke(ctx, grant.ID, uc.now()); err != nil {
		log.Printf("[facebook-health] grant %s could not be revoked: %v", grant.ID, err)
	}
	pages, err := uc.pages.ListByGrant(ctx, grant.ID)
	if err != nil {
		log.Printf("[facebook-health] pages of grant %s could not be loaded: %v", grant.ID, err)
		return
	}
	for _, page := range pages {
		uc.setStatus(ctx, page, fbdomain.StatusTokenRevoked, reason)
	}
}

func (uc *HealthCheckUseCase) setStatus(ctx context.Context, page *fbdomain.Page, status fbdomain.Status, reason string) {
	if !page.Status.CanTransitionTo(status) {
		return
	}
	if err := uc.pages.UpdateStatus(ctx, page.ID, status, reason); err != nil {
		log.Printf("[facebook-health] page %s status could not be set to %s: %v", page.FBPageID, status, err)
	}
}

func missingFields(want, active []string) []string {
	have := make(map[string]struct{}, len(active))
	for _, f := range active {
		have[f] = struct{}{}
	}
	var missing []string
	for _, f := range want {
		if _, ok := have[f]; !ok {
			missing = append(missing, f)
		}
	}
	return missing
}

func (uc *HealthCheckUseCase) CheckPage(ctx context.Context, workspaceID, pageID string) (*fbdomain.Page, error) {
	page, err := uc.pages.FindByID(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if page.WorkspaceID != workspaceID {
		return nil, fbdomain.ErrPageNotFound
	}
	grant, err := uc.grants.FindByID(ctx, page.GrantID)
	if err != nil {
		return nil, err
	}
	uc.checkGrant(ctx, grant)
	return uc.pages.FindByID(ctx, pageID)
}
