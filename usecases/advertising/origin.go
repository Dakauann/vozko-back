package advertising

import (
	"context"
	"errors"
	"strings"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

var ErrConversationNotVisible = errors.New("ads: conversation is not visible to this person")

type ConversationOrigin struct {
	Ad       *ads.Object
	AdSet    *ads.Object
	Campaign *ads.Object
	Account  *ads.AdAccount
	Cost     ads.LeadCost
}

type OriginUseCase struct {
	access      shared.EntryAccessChecker
	origins     conversation.AdOriginReader
	accounts    ads.AccountRepository
	objects     ads.ObjectRepository
	insights    ads.InsightRepository
	attribution ads.AttributionRepository
}

func NewOriginUseCase(access shared.EntryAccessChecker, origins conversation.AdOriginReader, accounts ads.AccountRepository, objects ads.ObjectRepository, insights ads.InsightRepository, attribution ads.AttributionRepository) *OriginUseCase {
	return &OriginUseCase{access: access, origins: origins, accounts: accounts, objects: objects, insights: insights, attribution: attribution}
}

func (uc *OriginUseCase) Origin(ctx context.Context, person shared.Person, workspaceID, entryType, entryID string) (*ConversationOrigin, error) {
	if !person.MayActOn(uc.access, workspaceID, entryID, entryType) {
		return nil, ErrConversationNotVisible
	}
	origin, err := uc.origins.AdOrigin(entryID, shared.EntryType(entryType))
	if err != nil {
		return nil, err
	}
	if origin == nil || strings.TrimSpace(origin.AdID) == "" {
		return nil, ads.ErrObjectNotFound
	}
	ad, err := uc.objects.Find(ctx, workspaceID, origin.AdID)
	if err != nil {
		return nil, err
	}
	account, err := uc.accounts.FindByID(ctx, workspaceID, ad.AdAccountID)
	if err != nil {
		return nil, err
	}
	loc, err := account.Location()
	if err != nil {
		return nil, err
	}
	out := &ConversationOrigin{Ad: ad, Account: account}
	out.AdSet = uc.optional(ctx, workspaceID, ad.AdSetMetaID)
	out.Campaign = uc.optional(ctx, workspaceID, ad.CampaignMetaID)
	cost, err := uc.leadCost(ctx, workspaceID, account, ad.MetaID, origin.ArrivedAt, loc)
	if err != nil {
		return nil, err
	}
	out.Cost = cost
	return out, nil
}

func (uc *OriginUseCase) optional(ctx context.Context, workspaceID, metaID string) *ads.Object {
	if metaID == "" {
		return nil
	}
	object, err := uc.objects.Find(ctx, workspaceID, metaID)
	if err != nil {
		return nil
	}
	return object
}

func (uc *OriginUseCase) leadCost(ctx context.Context, workspaceID string, account *ads.AdAccount, adID string, arrived time.Time, loc *time.Location) (ads.LeadCost, error) {
	day := ads.CivilDay(arrived, loc)
	cost := ads.LeadCost{AdMetaID: adID, Day: day, Currency: account.Currency}
	insight, err := uc.insights.AdDay(ctx, adID, day)
	switch {
	case errors.Is(err, ads.ErrObjectNotFound):
		return cost, nil
	case err != nil:
		return ads.LeadCost{}, err
	}
	from, to := ads.DateRange{Since: day, Until: day}.Bounds(loc)
	conversations, err := uc.attribution.Conversations(ctx, workspaceID, adID, from, to)
	if err != nil {
		return ads.LeadCost{}, err
	}
	cost.SpendMicros = insight.SpendMicros
	cost.Conversations = conversations
	return cost, nil
}
