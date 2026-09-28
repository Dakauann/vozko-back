package facebook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
)

var ErrConversationAccessDenied = errors.New("facebook: no access to this conversation")

type EntryAccess interface {
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type ThreadActor struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool
}

type ThreadControlDeps struct {
	Access        EntryAccess
	Pages         fbdomain.PageRepository
	Contacts      fbdomain.ContactRepository
	Conversations fbdomain.ConversationRepository
	Routing       fbdomain.RoutingService
	OurAppID      string
}

type ThreadControlUseCase struct {
	d   ThreadControlDeps
	now func() time.Time
}

func NewThreadControlUseCase(d ThreadControlDeps) *ThreadControlUseCase {
	return &ThreadControlUseCase{d: d, now: func() time.Time { return time.Now().UTC() }}
}

type thread struct {
	page *fbdomain.Page
	conv *fbdomain.Conversation
	psid string
}

func (uc *ThreadControlUseCase) Take(ctx context.Context, actor ThreadActor, conversationID string) (string, error) {
	t, err := uc.thread(ctx, actor, conversationID)
	if err != nil {
		return "", err
	}
	if err := uc.d.Routing.TakeControl(ctx, t.page.FBPageID, t.page.PageToken, t.psid, fbdomain.OutboundMetadata(true)); err != nil {
		return "", err
	}
	if err := uc.d.Conversations.SetThreadOwner(ctx, t.conv.ID, "", uc.now()); err != nil {
		return "", err
	}
	return uc.d.OurAppID, nil
}

func (uc *ThreadControlUseCase) Release(ctx context.Context, actor ThreadActor, conversationID string) error {
	t, err := uc.thread(ctx, actor, conversationID)
	if err != nil {
		return err
	}
	if err := uc.d.Routing.ReleaseControl(ctx, t.page.FBPageID, t.page.PageToken, t.psid); err != nil {
		return err
	}
	return uc.d.Conversations.SetThreadOwner(ctx, t.conv.ID, fbdomain.OtherAppOwner, uc.now())
}

func (uc *ThreadControlUseCase) Probe(ctx context.Context, page *fbdomain.Page) {
	isDefault, err := uc.latestThreadIsOurs(ctx, page)
	if err != nil {
		log.Printf("[facebook-routing] page %s probe failed: %v", page.FBPageID, err)
		return
	}
	if err := uc.d.Pages.UpdateRouting(ctx, page.ID, isDefault, uc.now()); err != nil {
		log.Printf("[facebook-routing] page %s probe not recorded: %v", page.FBPageID, err)
	}
}

func (uc *ThreadControlUseCase) latestThreadIsOurs(ctx context.Context, page *fbdomain.Page) (*bool, error) {
	conv, err := uc.d.Conversations.LatestForPage(ctx, page.ID)
	if errors.Is(err, fbdomain.ErrConversationNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	contact, err := uc.d.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	owner, err := uc.d.Routing.ThreadOwner(ctx, page.FBPageID, page.PageToken, contact.PSID)
	if err != nil {
		return nil, err
	}
	if owner == "" {
		return nil, nil
	}
	ours := owner == uc.d.OurAppID
	return &ours, nil
}

func (uc *ThreadControlUseCase) thread(ctx context.Context, actor ThreadActor, conversationID string) (*thread, error) {
	conv, err := uc.d.Conversations.FindByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conv.WorkspaceID != actor.WorkspaceID {
		return nil, fbdomain.ErrConversationNotFound
	}
	if !uc.d.Access.CanAccessEntry(actor.UserID, actor.WorkspaceID, conv.ID, string(shared.EntryTypeFacebook), actor.IsAdmin) {
		return nil, ErrConversationAccessDenied
	}
	page, err := uc.d.Pages.FindByID(ctx, conv.PageID)
	if err != nil {
		return nil, err
	}
	if !page.Can(fbdomain.CapMessaging) {
		return nil, fmt.Errorf("%w: page %s cannot message", fbdomain.ErrCapabilityDenied, page.Name)
	}
	contact, err := uc.d.Contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	return &thread{page: page, conv: conv, psid: contact.PSID}, nil
}

type ThreadHolder string

const (
	HolderVozko         ThreadHolder = "vozko"
	HolderBusinessSuite ThreadHolder = "meta_business_suite"
	HolderOtherApp      ThreadHolder = "other_app"
)

type ThreadState struct {
	Holder            ThreadHolder
	OwnerAppID        string
	IsDefaultRouteApp *bool
}

func (uc *ThreadControlUseCase) State(ctx context.Context, actor ThreadActor, conversationID string) (*ThreadState, error) {
	t, err := uc.thread(ctx, actor, conversationID)
	if err != nil {
		return nil, err
	}
	state := &ThreadState{OwnerAppID: t.conv.ThreadOwnerAppID, IsDefaultRouteApp: t.page.IsDefaultRouteApp}
	switch owner := t.conv.ThreadOwnerAppID; {
	case owner == "" || owner == uc.d.OurAppID:
		state.Holder = HolderVozko
	case fbdomain.IsPageInboxApp(owner):
		state.Holder = HolderBusinessSuite
	default:
		state.Holder = HolderOtherApp
	}
	return state, nil
}
