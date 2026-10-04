package container

import (
	"context"
	"log"
	"net/url"
	"time"

	httpdelivery "vozko/delivery/http"
	webchathttp "vozko/delivery/http/webchat"
	"vozko/domain/cache"
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
	webchat_repository "vozko/infra/repositories/webchat"
	wcinfra "vozko/infra/webchat"
	conversation_usecase "vozko/usecases/conversation"
	wcuc "vozko/usecases/webchat"
)

type webchatBundle struct {
	Widgets       wcdomain.WidgetRepository
	Visitors      wcdomain.VisitorRepository
	Conversations wcdomain.ConversationRepository
	Keys          wcdomain.Keys
	Broker        *wcinfra.Broker
	PublicBase    string

	Management *webchathttp.Handler
	Public     *webchathttp.PublicHandler
}

func (c *Container) initWebchat() {
	keys, err := wcdomain.DeriveKeys(c.cfg.AuthJWTSecret)
	if err != nil {
		log.Fatalf("[webchat] %v", err)
	}
	c.webchat = &webchatBundle{
		Widgets:       webchat_repository.NewWidgetRepository(c.db),
		Visitors:      webchat_repository.NewVisitorRepository(c.db),
		Conversations: webchat_repository.NewConversationRepository(c.db),
		Keys:          keys,
		Broker:        wcinfra.NewBroker(c.redisProvider.SharedState()),
		PublicBase:    webchatPublicBase(c.cfg.APIBaseURL),
	}
}

func webchatPublicBase(raw string) string {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		log.Printf("[webchat] API_BASE_URL is missing or not an absolute URL; the website chat stays off until it is set")
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (c *Container) initWebchatRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.webchat
	if bundle == nil {
		return
	}

	widgets := wcuc.NewWidgets(bundle.Widgets, wcuc.References{
		Agents:      c.repositories.agent,
		Workflows:   c.repositories.workflow,
		Pipelines:   c.repositories.pipeline,
		Departments: c.repositories.workspaceDepartment,
	})
	moderation := wcuc.NewModeration(bundle.Conversations, bundle.Visitors,
		c.services.conversationAuthImpl, bundle.Broker, c.services.conversationHub)
	bundle.Management = webchathttp.NewHandler(widgets, moderation, bundle.PublicBase)

	if bundle.PublicBase == "" {
		return
	}
	presenter, ok := c.services.conversationHistory.(wcuc.MessagePresenter)
	if !ok {
		log.Printf("[webchat] history provider cannot present messages; the website chat stays off")
		return
	}
	visitors := wcuc.NewVisitorService(wcuc.VisitorDeps{
		Widgets:       bundle.Widgets,
		Visitors:      bundle.Visitors,
		Conversations: bundle.Conversations,
		Keys:          bundle.Keys,
		OneShot:       c.redisProvider.SharedState(),
		Limits:        webchatLimits(c.services.rateLimiterFactory),
		Leads:         c.repositories.lead,
		Transcript:    history,
		Messages:      c.repositories.conversation,
		Presenter:     presenter,
		Media:         c.mediaStore(),
		Assignments:   c.services.assignmentService,
		Automation: conversation_usecase.NewInboundAutomation(
			c.useCases.triggerEvaluator, c.mustChannelAIReply(), c.services.analysisScheduler, c.repositories.conversation),
		Events:    bundle.Broker,
		Operators: c.services.conversationHub,
	})
	bundle.Public = webchathttp.NewPublicHandler(visitors, bundle.Broker, bundle.PublicBase)
	bundle.Broker.Start(context.Background())
	log.Printf("[webchat] website chat served from %s%s", bundle.PublicBase, webchathttp.PublicPrefix)
}

func webchatLimits(factory cache.RateLimiterFactory) wcuc.Limits {
	if factory == nil {
		return wcuc.Limits{}
	}
	return wcuc.Limits{
		SessionsPerIP:         factory("wc_sessions_ip", 20, time.Hour),
		SessionsPerWidget:     factory("wc_sessions_widget", 600, time.Hour),
		MessagesPerVisitor:    factory("wc_msgs_visitor", 20, time.Minute),
		MessagesPerVisitorDay: factory("wc_msgs_visitor_day", 300, 24*time.Hour),
		MessagesPerIP:         factory("wc_msgs_ip", 60, time.Minute),
		UploadsPerVisitor:     factory("wc_uploads_visitor", 10, time.Hour),
		HandOffsPerVisitor:    factory("wc_handoffs_visitor", 3, time.Hour),
		TypingPerVisitor:      factory("wc_typing_visitor", 60, time.Minute),
	}
}

func (c *Container) wireWebchatConversationStack() {
	bundle := c.webchat
	if bundle == nil {
		return
	}
	conversations := bundle.Conversations

	c.registerChannelAdapter(wcuc.NewChannelAdapter(bundle.Widgets, bundle.Visitors, conversations, bundle.Broker))

	if setter, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	}); ok {
		setter.SetAutomationReader(shared.EntryTypeWebchat, func(ctx context.Context, entryID string) (*bool, error) {
			conv, err := conversations.FindByID(ctx, entryID)
			if err != nil {
				return nil, err
			}
			return conv.AutomationEnabled, nil
		})
	}
	if c.services.conversationAutomation != nil {
		c.services.conversationAutomation.Register(shared.EntryTypeWebchat, conversations.SetAutomationEnabled)
	}
	if c.services.conversationAuthImpl != nil {
		c.services.conversationAuthImpl.SetEntryAccessRepo(shared.EntryTypeWebchat, conversations)
	}
	if c.services.conversationStatusService != nil {
		c.services.conversationStatusService.SetConversationStatusStore(shared.EntryTypeWebchat, conversationStatusFuncs{
			status: conversations.StatusForEntry,
			set:    conversations.SetStatus,
		})
		c.services.conversationStatusService.SetConversationCounter(shared.EntryTypeWebchat, conversations.CountByStatus)
	}
	if setter, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	}); ok {
		setter.SetEntryOwnerResolver(shared.EntryTypeWebchat, conversations)
	}
	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(shared.EntryTypeWebchat, webchatContactIdentity(bundle))
	}
}

func webchatContactIdentity(bundle *webchatBundle) conversation_usecase.ContactIdentityLookup {
	visitors, conversations := bundle.Visitors, bundle.Conversations

	display := func(v *wcdomain.Visitor) conversation_usecase.ContactDisplay {
		out := conversation_usecase.ContactDisplay{
			ContactID: v.ID,
			Ref:       v.ID,
			Handle:    v.Handle(),
			Name:      v.DisplayName(),
		}
		if v.LeadID != nil {
			out.LeadID = *v.LeadID
		}
		return out
	}

	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := visitors.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, v := range found {
				if v != nil {
					out[v.ID] = display(v)
				}
			}
			return out, nil
		},
		forConversation: func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error) {
			conv, err := conversations.FindByID(ctx, conversationID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, "", err
			}
			v, err := visitors.FindByID(ctx, conv.VisitorID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, conv.WorkspaceID, err
			}
			return display(v), conv.WorkspaceID, nil
		},
	}
}

func webchatAnalysisResolver(bundle *webchatBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeWebchat,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := bundle.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			widget, err := bundle.Widgets.FindByIDUnscoped(ctx, conv.WidgetID)
			if err != nil || widget == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.VisitorID,
				Container: analysisContainer{ID: widget.ID, Name: widget.Name, Automation: widget.Automation()},
			}, nil
		},
		func(ctx context.Context, visitorID string) (*analysisContact, error) {
			v, err := bundle.Visitors.FindByID(ctx, visitorID)
			if err != nil || v == nil {
				return nil, err
			}
			return &analysisContact{Label: v.DisplayName(), LeadID: v.LeadID}, nil
		})
}

func webchatRoutes(c *Container) httpdelivery.WebchatRoutes {
	if c.webchat == nil {
		return httpdelivery.WebchatRoutes{}
	}
	return httpdelivery.WebchatRoutes{Management: c.webchat.Management, Public: c.webchat.Public}
}
