package container

import (
	"context"
	"log"

	facebookhttp "vozko/delivery/http/facebook"
	conversation_domain "vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
	fbuc "vozko/usecases/facebook"
	"vozko/usecases/metachannel"
)

func (c *Container) initFacebookRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.facebook
	if bundle == nil || !bundle.Enabled {
		return
	}
	if c.useCases == nil {
		log.Fatalf("[facebook] runtime wiring ran before useCases were built")
	}
	watermarks, ok := c.repositories.conversation.(conversation_domain.DeliveryWatermarkRepository)
	if !ok {
		log.Fatalf("[facebook] the message repository does not support delivery watermarks")
	}

	bundle.WebhookHandler = facebookhttp.NewWebhookHandler(
		c.useCases.publishWebhook,
		append([]string{c.cfg.MetaAppSecret}, c.cfg.MetaAppSecretsExtra...),
		c.cfg.FacebookWebhookVerifyToken,
	)

	transcript := &metachannel.Transcript{
		EntryType:   shared.EntryTypeFacebook,
		Channel:     conversation_domain.MessageChannelFacebook,
		Prefix:      fbuc.MetadataPrefix,
		History:     history,
		Messages:    c.repositories.conversation,
		Media:       c.mediaStore(),
		Ads:         c.adOriginRecorder(),
		Fetch:       bundle.Messaging.FetchBytes,
		Broadcaster: c.services.conversationHub,
	}

	messages := fbuc.NewHandleMessagingUseCase(fbuc.HandleMessagingDeps{
		Pages:         bundle.Pages,
		Contacts:      bundle.Contacts,
		Conversations: bundle.Conversations,
		Profiles:      bundle.Messaging,
		Avatars:       bundle.Pictures,
		Transcript:    transcript,
		Watermarks:    watermarks,
		Assignments:   c.services.assignmentService,
		Automation: conversation_usecase.NewInboundAutomation(
			c.useCases.triggerEvaluator,
			c.mustChannelAIReply(),
			c.services.analysisScheduler,
			c.repositories.conversation,
		).WithDelegations(c.services.delegations),
		OurAppID: c.cfg.MetaAppID,
	})

	automation := c.commentAutomation()
	audience := c.facebookCommentAudience()
	bundle.CommentsUC = fbuc.NewCommentUseCases(fbuc.CommentDeps{
		Pages: bundle.Pages, Comments: bundle.Comments, Service: bundle.CommentSvc, Messaging: bundle.Messaging,
		Contacts: bundle.Contacts, Conversations: bundle.Conversations, Transcript: transcript,
		PrivateReplies: automation.Sender, ReplyRecords: automation.Replies, Audience: audience,
	})
	feed := fbuc.NewHandleFeedUseCase(fbuc.HandleFeedDeps{
		Pages: bundle.Pages, Posts: bundle.Posts, Comments: bundle.Comments, Contacts: bundle.Contacts,
		Service: bundle.CommentSvc, Audience: audience, Videos: bundle.Publisher,
		Rules: fbuc.NewCommentRules(automation.Evaluator, fbuc.NewCommentActions(bundle.CommentsUC)),
	})
	bundle.Handler = facebookhttp.NewHandler(facebookhttp.HandlerDeps{
		Connect:             bundle.Connect,
		Pages:               bundle.PagesUC,
		Health:              bundle.Health,
		ThreadControl:       bundle.ThreadControl,
		Posts:               bundle.PostsUC,
		Comments:            bundle.CommentsUC,
		Profile:             bundle.ProfileUC,
		Rules:               automation.Manager,
		PictureURL:          bundle.Pictures.URL,
		HumanAgentAvailable: c.cfg.FacebookHumanAgentApproved,
		FrontendBaseURL:     c.cfg.FrontendBaseURL,
	})

	bundle.Consume = fbuc.NewConsumeWebhookUseCase(fbuc.ConsumeWebhookDeps{
		QueueSub:    c.services.webhookQueueSub,
		QueuePub:    c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(),
		Durable:     c.processedWebhookEvents(),
		Messages:    messages,
		Feed:        feed,
		PageEvents:  fbuc.PageEventLogger{},
	})
	bundle.PublishRunner = fbuc.NewPublishConsumer(
		c.services.facebookPublishSub, c.services.facebookPublishPub, c.redisProvider.SharedState(), bundle.Publisher)
}

func (c *Container) wireFacebookConversationStack() {
	bundle := c.facebook
	if bundle == nil || !bundle.Enabled {
		return
	}
	conversations := bundle.Conversations

	c.registerChannelAdapter(fbuc.NewChannelAdapter(fbuc.ChannelAdapterDeps{
		Pages:              bundle.Pages,
		Contacts:           bundle.Contacts,
		Conversations:      conversations,
		Messaging:          bundle.Messaging,
		Routing:            bundle.Routing,
		HumanAgentApproved: c.cfg.FacebookHumanAgentApproved,
	}))

	if setter, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	}); ok {
		setter.SetAutomationReader(shared.EntryTypeFacebook, func(ctx context.Context, entryID string) (*bool, error) {
			conv, err := conversations.FindByID(ctx, entryID)
			if err != nil {
				return nil, err
			}
			return conv.AutomationEnabled, nil
		})
	} else {
		log.Fatalf("[facebook] history provider exposes no SetAutomationReader")
	}
	if c.services.conversationAutomation == nil || c.services.conversationAuthImpl == nil || c.services.conversationStatusService == nil {
		log.Fatalf("[facebook] conversation services must be built before the facebook conversation stack")
	}
	c.services.conversationAutomation.Register(shared.EntryTypeFacebook, conversations.SetAutomationEnabled)
	c.services.conversationAuthImpl.SetEntryAccessRepo(shared.EntryTypeFacebook, conversations)
	c.services.conversationStatusService.SetConversationStatusStore(shared.EntryTypeFacebook,
		conversationStatusFuncs{status: conversations.StatusForEntry, set: conversations.SetStatus})
	c.services.conversationStatusService.SetConversationCounter(shared.EntryTypeFacebook, conversations.CountByStatus)
	if setter, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	}); ok {
		setter.SetEntryOwnerResolver(shared.EntryTypeFacebook, conversations)
	} else {
		log.Fatalf("[facebook] workspace resolver exposes no SetEntryOwnerResolver")
	}
	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(shared.EntryTypeFacebook, facebookContactIdentity(bundle))
	} else {
		log.Fatalf("[facebook] history provider exposes no SetContactIdentityLookup")
	}
}

func facebookContactIdentity(bundle *facebookBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations, pictures := bundle.Contacts, bundle.Conversations, bundle.Pictures
	display := func(c *fbdomain.Contact) conversation_usecase.ContactDisplay {
		out := conversation_usecase.ContactDisplay{
			ContactID:  c.ID,
			Ref:        c.PSID,
			Name:       c.DisplayName(),
			PictureURL: pictures.URL(c.AvatarStorageKey),
		}
		if c.LeadID != nil {
			out.LeadID = *c.LeadID
		}
		return out
	}
	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := contacts.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, c := range found {
				out[c.ID] = display(c)
			}
			return out, nil
		},
		forConversation: func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error) {
			conv, err := conversations.FindByID(ctx, conversationID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, "", err
			}
			contact, err := contacts.FindByID(ctx, conv.ContactID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, conv.WorkspaceID, err
			}
			return display(contact), conv.WorkspaceID, nil
		},
	}
}

type facebookAudience interface {
	Enqueue(ctx context.Context, comment *fbdomain.Comment)
	Forget(ctx context.Context, fbCommentID string)
}

func (c *Container) facebookCommentAudience() facebookAudience {
	if c.audience != nil && c.audience.Enabled && c.audience.FacebookAdapter != nil {
		return c.audience.FacebookAdapter
	}
	log.Printf("[facebook] comment analysis is not enabled; comments are moderated without audience analysis")
	return fbuc.AudienceDisabled{}
}
