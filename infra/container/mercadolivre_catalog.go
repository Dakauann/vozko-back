package container

import (
	"log"
	"time"

	mercadolivrehttp "vozko/delivery/http/mercadolivre"
	conversation_domain "vozko/domain/conversation"
	entrypurge_domain "vozko/domain/entrypurge"
	"vozko/domain/shared"
	redisCache "vozko/infra/cache"
	mlinfra "vozko/infra/mercadolivre"
	"vozko/infra/netguard"
	commentautomation_repository "vozko/infra/repositories/commentautomation"
	entrypurge_repository "vozko/infra/repositories/entrypurge"
	mercadolivre_repository "vozko/infra/repositories/mercadolivre"
	entrypurge_usecase "vozko/usecases/entrypurge"
	mluc "vozko/usecases/mercadolivre"
	"vozko/usecases/shared/imagerehost"
)

const (
	mercadoLivreThumbnailTimeout = 30 * time.Second
	mercadoLivreMemoTimeout      = 30 * time.Second
)

type mercadoLivreCatalog struct {
	listings   *mluc.Listings
	questions  *mluc.Questions
	unanswered *mluc.UnansweredQuestionsReconciler
}

func (c *Container) buildMercadoLivreCatalog(b *mercadoLivreBundle, publisher *mluc.JobPublisher, history conversation_domain.MessageHistoryManager) mercadoLivreCatalog {
	if b.Threads == nil || b.Tokens == nil || publisher == nil || history == nil {
		log.Fatalf("[mercadolivre] the listings and questions need the thread sync, tokens, the job publisher and the history manager")
	}
	if c.services.fileStorage == nil || c.services.conversationAuth == nil {
		log.Fatalf("[mercadolivre] file storage and the conversation authorizer must exist before the listings and questions")
	}
	files, ok := c.services.fileStorage.(entrypurge_domain.Files)
	if !ok {
		log.Fatalf("[mercadolivre] the file storage cannot delete files, so the purge could not remove personal data")
	}

	if b.Listings == nil || b.Thumbnails == nil {
		log.Fatalf("[mercadolivre] the listing mirror and thumbnail storage must be built before the listings and questions")
	}
	questionRepo := mercadolivre_repository.NewQuestionRepository(c.db)
	listings := mluc.NewListings(mluc.ListingsDeps{
		Accounts: b.Accounts, Listings: b.Listings, Services: b.Services, Tokens: b.Tokens, Images: b.Thumbnails, Metrics: b.Metrics,
	})
	packs := mluc.NewPacks(mluc.PacksDeps{
		Accounts: b.Accounts, Conversations: b.Conversations, Listings: b.Listings, Images: b.Thumbnails,
		Refresh: publisher, Access: c.services.conversationAuth, Metrics: b.Metrics,
	})

	automation := c.commentAutomation()
	automation.Manager.Register(shared.EntryTypeMercadoLivre, mluc.NewAccountOwnership(b.Accounts))
	questions := mluc.NewQuestions(mluc.QuestionsDeps{
		Accounts:  b.Accounts,
		Contacts:  b.Contacts,
		Questions: questionRepo,
		Listings:  listings,
		Services:  b.Services,
		Tokens:    b.Tokens,
		Rules:     mluc.NewRuleEvaluator(automation.Rules, automation.Executions, b.Metrics),
		Memo:      redisCache.NewSharedStateMemo(c.redisProvider.SharedState(), mercadoLivreMemoTimeout),
		Metrics:   b.Metrics,
	})
	reasons := mluc.NewReasons(mluc.ReasonsDeps{
		Accounts:      b.Accounts,
		Conversations: b.Conversations,
		Services:      b.Services,
		Tokens:        b.Tokens,
		Threads:       b.Threads,
		History:       history,
		Access:        c.services.conversationAuth,
		Metrics:       b.Metrics,
	})

	b.Purge = mluc.NewPurgeJob(mluc.PurgeDeps{
		Accounts: b.Accounts,
		Purge:    mercadolivre_repository.NewPurgeRepository(c.db),
		Entries:  entrypurge_usecase.New(entrypurge_repository.NewStore(c.db), files),
		Rules:    commentautomation_repository.NewAccountPurger(c.db),
		Metrics:  b.Metrics,
	})
	b.Catalog = mercadolivrehttp.NewCatalogHandler(mercadolivrehttp.CatalogDeps{
		Listings: listings, Questions: questions, Reasons: reasons, Packs: packs, Rules: automation.Manager,
	})
	return mercadoLivreCatalog{
		listings:   listings,
		questions:  questions,
		unanswered: mluc.NewUnansweredQuestionsReconciler(b.Services.Questions, questionRepo, publisher),
	}
}

func (c *Container) buildMercadoLivreThumbnails(b *mercadoLivreBundle) {
	if c.services.fileStorage == nil {
		log.Fatalf("[mercadolivre] file storage must exist before the listing and pack thumbnails")
	}
	b.Listings = mercadolivre_repository.NewListingRepository(c.db)
	b.Thumbnails = imagerehost.New(
		mluc.NewThumbnailFetcher(mlinfra.NewMediaFetcher(netguard.NewHTTPClient(mercadoLivreThumbnailTimeout))),
		c.services.fileStorage,
	)
}
