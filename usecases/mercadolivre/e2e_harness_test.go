package mercadolivre_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"gorm.io/gorm"

	mercadolivrehttp "vozko/delivery/http/mercadolivre"
	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	"vozko/domain/pipeline"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
	infracache "vozko/infra/cache"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/http/middleware"
	"vozko/infra/mediaprobe"
	mlinfra "vozko/infra/mercadolivre"
	"vozko/infra/mercadolivre/mlfake"
	commentautomation_repository "vozko/infra/repositories/commentautomation"
	conversation_repository "vozko/infra/repositories/conversation"
	entrypurge_repository "vozko/infra/repositories/entrypurge"
	mercadolivre_repository "vozko/infra/repositories/mercadolivre"
	"vozko/infra/repositories/repotest"
	webhook_repository "vozko/infra/repositories/webhook"
	"vozko/infra/testenv"
	cauc "vozko/usecases/commentautomation"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/conversationmedia"
	entrypurge_usecase "vozko/usecases/entrypurge"
	mluc "vozko/usecases/mercadolivre"
	"vozko/usecases/shared/automationrefs"
	"vozko/usecases/shared/imagerehost"
	"vozko/usecases/shared/oauthstate"
)

const (
	e2eAppID       = "5503910054141466"
	e2eSecret      = "e2e-client-secret"
	e2eRedirect    = "https://api.e2e.example/oauth/mercadolivre/callback"
	e2ePathToken   = "q3Jv9xZ0b2xH5nLw1aPq8sT4uV7yB6cD-eF_gH2iJkM"
	e2eFrontend    = "https://app.e2e.example"
	e2eUserHeader  = "X-E2E-User"
	e2eSpaceHeader = "X-E2E-Workspace"
	e2eAgentReply  = "Oi! Seu pedido já foi enviado e chega em até 3 dias úteis. Qualquer dúvida, estou por aqui."
)

func e2eDB(t testing.TB) *gorm.DB {
	t.Helper()
	testenv.Postgres(t)
	testenv.Redis(t)
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	piigorm.SetService(svc)
	t.Cleanup(func() { piigorm.SetService(nil) })
	db := repotest.IsolatedDB(t, "ml_e2e")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(
		&schema.MercadoLivreAccount{}, &schema.MercadoLivreContact{}, &schema.MercadoLivreConversation{},
		&schema.MercadoLivreListing{}, &schema.MercadoLivreListingSync{}, &schema.MercadoLivreQuestion{},
		&schema.CommentRule{}, &schema.CommentRuleExecution{}, &schema.CommentPrivateReply{}, &schema.WebhookProcessedEvent{},
		&schema.ConversationMessage{}, &schema.ConversationMedia{}, &schema.ConversationEvent{}, &schema.ConversationNote{},
		&schema.ConversationPersonalMark{}, &schema.ConversationDelegation{}, &schema.ConversationAdOrigin{},
		&schema.ConversationLiveRead{}, &schema.LiveDecisionRecord{}, &schema.AIAttendanceSession{}, &schema.AssignmentHistory{},
		&schema.InboxAssignment{}, &schema.EntryLabel{}, &schema.EntryStage{}, &schema.OpportunityConversation{},
		&schema.ScheduledMessage{}, &schema.LeadExitRun{}, &schema.WhatsAppFormSend{}, &schema.WhatsAppFormResponse{},
		&schema.LeadMemory{}, &schema.WorkflowRunSchema{}, &schema.WorkflowRunLogSchema{}, &schema.CustomerPortfolioEntry{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{
		"ux_ml_account_live_user", "ux_ml_contact_account_user", "ux_ml_conversation_account_pack",
		"idx_ml_acct_refresh_due", "idx_ml_acct_reconcile_due", "idx_ml_acct_health_due", "idx_ml_acct_purge_due",
		"idx_ml_conv_workspace_lastmsg", "idx_ml_conv_account_lastmsg", "idx_ml_acct_workspace_created",
		"idx_ml_conv_account_created", "idx_ml_conv_account_shipment", "idx_ml_contact_questions_blocked",
		"ux_cm_entry_external_msgid",
	} {
		stmt, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return db
}

type memDelivery struct {
	body  []byte
	count int
}

type memQueue struct {
	mu         sync.Mutex
	inboxes    map[string]chan memDelivery
	pending    atomic.Int64
	delayed    atomic.Int64
	dead       map[string]int
	published  map[string]int
	delayScale float64
}

func newMemQueue(delayScale float64) *memQueue {
	return &memQueue{inboxes: map[string]chan memDelivery{}, dead: map[string]int{}, published: map[string]int{}, delayScale: delayScale}
}

type memAck struct {
	q     *memQueue
	topic string
	d     memDelivery
	once  sync.Once
}

func (a *memAck) Ack() error {
	a.once.Do(func() { a.q.pending.Add(-1) })
	return nil
}

func (a *memAck) Nack(requeue bool) error {
	a.once.Do(func() {
		if requeue {
			a.q.inbox(a.topic) <- a.d
			return
		}
		a.q.pending.Add(-1)
	})
	return nil
}

func (a *memAck) DeliveryCount() int { return a.d.count }

func (q *memQueue) inbox(topic string) chan memDelivery {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch, ok := q.inboxes[topic]
	if !ok {
		ch = make(chan memDelivery, 1<<16)
		q.inboxes[topic] = ch
	}
	return ch
}

func (q *memQueue) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	ch := q.inbox(topic)
	go func() {
		for d := range ch {
			handler(d.body, &memAck{q: q, topic: topic, d: d})
		}
	}()
	return nil
}

func (q *memQueue) Publish(topic string, body []byte) error {
	q.mu.Lock()
	if strings.HasSuffix(topic, messaging.DLQSuffix) {
		q.dead[strings.TrimSuffix(topic, messaging.DLQSuffix)]++
		q.mu.Unlock()
		return nil
	}
	q.published[topic]++
	q.mu.Unlock()
	q.pending.Add(1)
	q.inbox(topic) <- memDelivery{body: append([]byte(nil), body...), count: 1}
	return nil
}

func (q *memQueue) PublishWithDelay(topic string, body []byte, delay time.Duration) error {
	q.pending.Add(1)
	q.delayed.Add(1)
	copied := append([]byte(nil), body...)
	time.AfterFunc(time.Duration(float64(delay)*q.delayScale), func() {
		q.inbox(topic) <- memDelivery{body: copied, count: 2}
	})
	return nil
}

func (q *memQueue) DeleteQueue(string) error                 { return nil }
func (q *memQueue) ValidateConnection() error                { return nil }
func (q *memQueue) GetQueueLength(topic string) (int, error) { return len(q.inbox(topic)), nil }

func (q *memQueue) deadLetters(topic string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dead[topic]
}

func (q *memQueue) waitIdle(t testing.TB, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	quiet := 0
	for time.Now().Before(deadline) {
		if q.pending.Load() == 0 {
			quiet++
			if quiet >= 3 {
				return
			}
		} else {
			quiet = 0
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the queue did not drain in %s (pending %d)", timeout, q.pending.Load())
}

type countingLimiter struct{ calls *atomic.Int64 }

func (l countingLimiter) Allow(string) (bool, time.Duration, error) {
	l.calls.Add(1)
	return true, 0, nil
}

type e2eMetrics struct {
	mu     sync.Mutex
	events map[string]int
}

func (m *e2eMetrics) AddMercadoLivreEvent(stage, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = map[string]int{}
	}
	m.events[stage+"/"+outcome]++
}

func (m *e2eMetrics) count(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events[key]
}

type episodes struct {
	mu        sync.Mutex
	published []notification.Draft
	resolved  []string
}

func (e *episodes) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.published = append(e.published, d)
	return notification.Record{}, nil
}

func (e *episodes) Resolve(_ context.Context, _ string, keys ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resolved = append(e.resolved, keys...)
	return nil
}

func (e *episodes) drafts() []notification.Draft {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]notification.Draft(nil), e.published...)
}

type assignments struct {
	mu    sync.Mutex
	calls map[string]int
}

func (a *assignments) EnsureAssignment(entryID, entryType, accountID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.calls == nil {
		a.calls = map[string]int{}
	}
	a.calls[entryType+":"+entryID+":"+accountID]++
	return ""
}

func (a *assignments) count(entryID, accountID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[string(shared.EntryTypeMercadoLivre)+":"+entryID+":"+accountID]
}

type memFiles struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (f *memFiles) UploadFile(key string, data []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[key] = data
	return nil
}

func (f *memFiles) GetFileURL(key string) string { return "https://files.e2e.example/" + key }

func (f *memFiles) KeyFromURL(raw string) (string, bool) {
	key, ok := strings.CutPrefix(raw, "https://files.e2e.example/")
	return key, ok && key != ""
}

func (f *memFiles) DeleteFile(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, key)
	return nil
}

func (f *memFiles) withPrefix(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k := range f.files {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n
}

type scriptedAI struct {
	mu      sync.Mutex
	replies []string
	inputs  []ai.GenerateInput
}

func (s *scriptedAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, in)
	reply := e2eAgentReply
	if len(s.replies) > 0 {
		reply, s.replies = s.replies[0], s.replies[1:]
	}
	return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: reply}}, nil
}

func (s *scriptedAI) GenerateStream(context.Context, ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not streamed in the end-to-end test")
}
func (s *scriptedAI) GetAvaibleModels(context.Context) ([]string, error) { return nil, nil }
func (s *scriptedAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return nil, nil
}

func (s *scriptedAI) script(replies ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies = append(s.replies, replies...)
}

func (s *scriptedAI) generated() []ai.GenerateInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ai.GenerateInput(nil), s.inputs...)
}

type agents struct{ byID map[string]*agent.Agent }

func (a agents) FindByID(id string) (*agent.Agent, error) {
	if found, ok := a.byID[id]; ok {
		return found, nil
	}
	return nil, errors.New("agent not found")
}
func (a agents) Create(*agent.Agent) error                  { return errors.New("read only") }
func (a agents) Update(string, *agent.Agent) error          { return errors.New("read only") }
func (a agents) Delete(string) error                        { return errors.New("read only") }
func (a agents) FindByIDs([]string) ([]*agent.Agent, error) { return nil, errors.New("not used") }
func (a agents) List(agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	return nil, errors.New("not used")
}

type noWorkflows struct{}

func (noWorkflows) FindByID(string) (*workflow.Workflow, error) { return nil, errors.New("none") }

type noPipelines struct{}

func (noPipelines) GetByID(string, string) (*pipeline.Pipeline, error) {
	return nil, errors.New("none")
}

type noDepartments struct{}

func (noDepartments) GetDepartmentByID(string) (*wd.Department, error) {
	return nil, errors.New("none")
}

type handOffs struct {
	mu    sync.Mutex
	calls []ia.RouletteHandOff
}

func (h *handOffs) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, in)
	return "", nil
}

type events struct{}

func (events) Log(*ce.ConversationEvent) {}

type announcer struct{}

func (announcer) BroadcastMessageUpdated(string, string, *conversation.Message) {}
func (announcer) BroadcastEntryUpdate(string, string, *conversation.Message)    {}

type everyoneMayAccess struct{}

func (everyoneMayAccess) CanAccessEntry(string, string, string, string, bool) bool { return true }

type harness struct {
	t           testing.TB
	ctx         context.Context
	db          *gorm.DB
	fake        *mlfake.Server
	queue       *memQueue
	limiter     *atomic.Int64
	metrics     *e2eMetrics
	alerts      *episodes
	assignments *assignments
	files       *memFiles
	ai          *scriptedAI
	handOffs    *handOffs
	agentID     string
	workspaceID string
	userID      string

	accounts      mldomain.AccountRepository
	contacts      mldomain.ContactRepository
	conversations mldomain.ConversationRepository
	questionsRepo mldomain.QuestionRepository
	messages      conversation.MessageRepository

	tokens     *mluc.TokenSource
	adapter    *mluc.ChannelAdapter
	sender     *conversation_usecase.MessageSenderService
	reconciler *mluc.Reconciler
	missed     *mluc.MissedFeedsJob
	purge      *mluc.PurgeJob

	server  *httptest.Server
	poster  *http.Client
	handled *handlerTimes
}

type handlerTimes struct {
	mu        sync.Mutex
	durations []time.Duration
}

func (h *handlerTimes) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		at := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/webhooks/") {
			h.mu.Lock()
			h.durations = append(h.durations, time.Since(at))
			h.mu.Unlock()
		}
	})
}

func (h *handlerTimes) snapshot() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]time.Duration(nil), h.durations...)
}

func newHarness(t testing.TB) *harness {
	t.Helper()
	db := e2eDB(t)
	target := testenv.Redis(t)
	redis := infracache.NewRedisProvider(target.Addr, target.Password)
	if _, err := redis.SharedState().Exists("ml:e2e:ping"); err != nil {
		t.Fatalf("redis at %s is not reachable: %v", target.Addr, err)
	}
	fake := mlfake.New(mlfake.Config{AppID: e2eAppID, ClientSecret: e2eSecret, RedirectURI: e2eRedirect, IDBase: 13000000000 + rand.Int64N(800000000)*10}).Start()
	t.Cleanup(fake.Close)

	h := &harness{
		t: t, ctx: context.Background(), db: db, fake: fake, queue: newMemQueue(0.01), limiter: &atomic.Int64{},
		metrics: &e2eMetrics{}, alerts: &episodes{}, assignments: &assignments{}, files: &memFiles{}, ai: &scriptedAI{},
		handOffs: &handOffs{}, agentID: uuid.NewString(), workspaceID: uuid.NewString(), userID: uuid.NewString(),
	}
	limiterFactory := func(string, int, time.Duration) cache.RateLimiter { return countingLimiter{calls: h.limiter} }
	client, err := mlinfra.NewClient(mlinfra.Config{BaseURL: fake.URL(), RateLimiterFactory: limiterFactory, HTTPClient: &http.Client{Timeout: 5 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	oauth, err := mlinfra.NewOAuthService(client, mlinfra.OAuthConfig{AppID: e2eAppID, ClientSecret: e2eSecret, RedirectURI: e2eRedirect, AuthorizeURL: fake.AuthorizeURL()})
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	users, err := mlinfra.NewUserService(client)
	must(err)
	questionsAPI, err := mlinfra.NewQuestionService(client)
	must(err)
	messagesAPI, err := mlinfra.NewMessageService(client)
	must(err)
	attachments, err := mlinfra.NewAttachmentService(client)
	must(err)
	sales, err := mlinfra.NewSalesService(client)
	must(err)
	missedAPI, err := mlinfra.NewMissedFeedService(client)
	must(err)
	items, err := mlinfra.NewItemService(client)
	must(err)
	guide, err := mlinfra.NewActionGuideService(client)
	must(err)
	services := mluc.Services{
		OAuth: oauth, Users: users, Questions: questionsAPI, Messages: messagesAPI, Attachments: attachments,
		Packs: sales, Orders: sales, Shipments: sales, MissedFeeds: missedAPI, Items: items, ActionGuide: guide, Errors: mlinfra.Classifier{},
	}

	h.accounts = mercadolivre_repository.NewAccountRepository(db)
	h.contacts = mercadolivre_repository.NewContactRepository(db)
	h.conversations = mercadolivre_repository.NewConversationRepository(db)
	listingRepo := mercadolivre_repository.NewListingRepository(db)
	h.questionsRepo = mercadolivre_repository.NewQuestionRepository(db)
	h.messages = conversation_repository.NewRepository(db)
	watermarks, ok := h.messages.(conversation.DeliveryWatermarkRepository)
	if !ok {
		t.Fatal("the message repository does not keep delivery watermarks")
	}
	history := conversation_usecase.NewMessageHistoryManager(h.messages)

	prefix := "ml:e2e:" + uuid.NewString()[:8]
	nonces, err := oauthstate.NewNonceStore(redis.SharedState(), prefix)
	must(err)
	issuer, err := oauthstate.NewIssuer(e2eSecret, nonces, mluc.AccountsPath)
	must(err)
	sealed, err := oauthstate.NewSealedIssuer(issuer, redis.SharedState(), prefix)
	must(err)

	alerts := mluc.NewAccountAlerts(h.alerts)
	h.tokens = mluc.NewTokenSource(h.accounts, oauth, mlinfra.Classifier{}, alerts, h.metrics)
	publisher := mluc.NewJobPublisher(h.queue)
	intake := mluc.NewAcceptNotification(e2eAppID, e2ePathToken, publisher, h.metrics)
	thumbnails := imagerehost.New(mluc.NewThumbnailFetcher(mlinfra.NewMediaFetcher(&http.Client{Timeout: 5 * time.Second})), h.files)
	summaries := mluc.NewPackSummaries(mluc.PackSummariesDeps{Conversations: h.conversations, Listings: listingRepo, Services: services, Images: thumbnails, Metrics: h.metrics})

	agentRecord := &agent.Agent{ID: h.agentID, WorkspaceID: h.workspaceID, Name: "Atendente", MessagingModel: "e2e-model", MessagingPrompt: "Você atende compradores do Mercado Livre.", IsActive: true}
	agentRepo := agents{byID: map[string]*agent.Agent{h.agentID: agentRecord}}
	h.sender = conversation_usecase.NewMessageSenderService(h.messages, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	replies := conversation_usecase.NewChannelAIReplyService(agentRepo, h.ai, h.messages, h.sender, adapterRegistry{h: h}, h.handOffs, events{})
	automation := conversation_usecase.NewInboundAutomation(nil, replies, nil, h.messages).WithDelegations(conversation_repository.NewDelegationRepository(db))
	mediaStore := conversationmedia.NewStore(h.files, conversation_repository.NewMediaRepository(db), mediaprobe.New())
	durable := webhook_repository.NewProcessedEventRepository(db)

	threads := mluc.NewThreadSync(mluc.ThreadSyncDeps{
		Contacts: h.contacts, Conversations: h.conversations, Services: services, Tokens: h.tokens, History: history,
		Messages: h.messages, Media: mediaStore, Watermarks: watermarks, Assignments: h.assignments, Automation: automation,
		Dispatches: durable, Announcer: announcer{}, PackSummaries: summaries, Metrics: h.metrics,
	})
	h.adapter = mluc.NewChannelAdapter(mluc.ChannelAdapterDeps{
		Accounts: h.accounts, Contacts: h.contacts, Conversations: h.conversations, Services: services, Tokens: h.tokens,
		Threads: threads, Refresh: publisher, Media: mlinfra.NewMediaFetcher(&http.Client{Timeout: 5 * time.Second}), Metrics: h.metrics,
	})
	h.sender.SetChannelAdapters(conversation.NewAdapterRegistry(h.adapter))

	salesSync := mluc.NewSalesSync(h.conversations, services, h.tokens, summaries, threads)
	listings := mluc.NewListings(mluc.ListingsDeps{Accounts: h.accounts, Listings: listingRepo, Services: services, Tokens: h.tokens, Images: thumbnails, Metrics: h.metrics})
	ruleRepo := commentautomation_repository.NewRuleRepository(db)
	executions := commentautomation_repository.NewExecutionClaimRepository(db)
	manager := cauc.NewManager(ruleRepo, nil)
	manager.Register(shared.EntryTypeMercadoLivre, mluc.NewAccountOwnership(h.accounts))
	questions := mluc.NewQuestions(mluc.QuestionsDeps{
		Accounts: h.accounts, Contacts: h.contacts, Questions: h.questionsRepo, Listings: listings, Services: services, Tokens: h.tokens,
		Rules: mluc.NewRuleEvaluator(ruleRepo, executions, h.metrics), Memo: infracache.NewSharedStateMemo(redis.SharedState(), 5*time.Second), Metrics: h.metrics,
	})
	dispatcher := mluc.NewNotificationDispatcher(h.accounts, h.metrics, map[mldomain.JobKind]mluc.JobHandler{
		mldomain.JobFetchMessage: threads, mldomain.JobFetchThread: threads, mldomain.JobFetchOrder: salesSync,
		mldomain.JobFetchShipment: salesSync, mldomain.JobRefreshPack: salesSync, mldomain.JobFetchQuestion: questions, mldomain.JobFetchItem: listings,
	})
	consumers := mluc.NewConsumeNotifications(mluc.ConsumeNotificationsDeps{
		QueueSub: h.queue, QueuePub: h.queue, SharedState: redis.SharedState(), Durable: durable,
		Dispatcher: dispatcher, Errors: mlinfra.Classifier{}, Metrics: h.metrics,
	})
	must(consumers.Start())

	h.reconciler = mluc.NewReconciler(h.accounts, h.tokens, h.metrics,
		mluc.NewUnreadThreadsReconciler(messagesAPI, publisher),
		mluc.NewUnansweredQuestionsReconciler(questionsAPI, h.questionsRepo, publisher),
		listings,
	)
	h.missed = mluc.NewMissedFeedsJob(h.accounts, h.tokens, missedAPI, intake)
	h.purge = mluc.NewPurgeJob(mluc.PurgeDeps{
		Accounts: h.accounts, Purge: mercadolivre_repository.NewPurgeRepository(db),
		Entries: entrypurge_usecase.New(entrypurge_repository.NewStore(db), h.files),
		Rules:   commentautomation_repository.NewAccountPurger(db), Metrics: h.metrics,
	})

	connect := mluc.NewConnectUseCase(mluc.ConnectDeps{Accounts: h.accounts, Services: services, States: sealed, PKCE: mlinfra.PKCE{}, Alerts: alerts, Metrics: h.metrics})
	accountsUC := mluc.NewAccounts(mluc.AccountsDeps{
		Accounts: h.accounts, OAuth: oauth,
		References: automationrefs.Finders{Agents: agentRepo, Workflows: noWorkflows{}, Pipelines: noPipelines{}, Departments: noDepartments{}},
		Alerts:     alerts, Metrics: h.metrics,
	})
	health := mluc.NewHealthCheck(mluc.HealthCheckDeps{Accounts: h.accounts, Contacts: h.contacts, Services: services, Tokens: h.tokens, Alerts: alerts, Metrics: h.metrics})
	packs := mluc.NewPacks(mluc.PacksDeps{Accounts: h.accounts, Conversations: h.conversations, Listings: listingRepo, Images: thumbnails, Refresh: publisher, Access: everyoneMayAccess{}, Metrics: h.metrics})
	reasons := mluc.NewReasons(mluc.ReasonsDeps{Accounts: h.accounts, Conversations: h.conversations, Services: services, Tokens: h.tokens, Threads: threads, History: history, Access: everyoneMayAccess{}, Metrics: h.metrics})

	router := mux.NewRouter()
	ac := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, space := r.Header.Get(e2eUserHeader), r.Header.Get(e2eSpaceHeader)
			if user == "" || space == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: user, Role: "admin"})
			ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, space)
			next(w, r.WithContext(ctx))
		}
	}
	handler := mercadolivrehttp.NewHandler(mercadolivrehttp.HandlerDeps{Connect: connect, Accounts: accountsUC, Health: health, FrontendBaseURL: e2eFrontend})
	mercadolivrehttp.RegisterProtectedRoutes(router, handler, ac)
	mercadolivrehttp.RegisterCatalogRoutes(router, mercadolivrehttp.NewCatalogHandler(mercadolivrehttp.CatalogDeps{
		Listings: listings, Questions: questions, Reasons: reasons, Packs: packs, Rules: manager,
	}), ac)
	mercadolivrehttp.RegisterPublicRoutes(router, handler, mercadolivrehttp.NewNotificationHandler(intake))
	h.handled = &handlerTimes{}
	h.server = httptest.NewServer(h.handled.wrap(router))
	t.Cleanup(h.server.Close)
	h.poster = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 128}}
	return h
}

type adapterRegistry struct{ h *harness }

func (r adapterRegistry) For(t shared.EntryType) (conversation.ChannelAdapter, error) {
	if t == shared.EntryTypeMercadoLivre && r.h.adapter != nil {
		return r.h.adapter, nil
	}
	return nil, conversation.ErrNoAdapterForEntryType
}
func (r adapterRegistry) Has(t shared.EntryType) bool { return t == shared.EntryTypeMercadoLivre }
func (r adapterRegistry) EntryTypes() []shared.EntryType {
	return []shared.EntryType{shared.EntryTypeMercadoLivre}
}

type apiResult struct {
	status int
	body   []byte
	header http.Header
}

func (r apiResult) decode(t testing.TB, out any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.body, &envelope); err != nil || len(envelope.Data) == 0 {
		if err := json.Unmarshal(r.body, out); err != nil {
			t.Fatalf("decode %s: %v", r.body, err)
		}
		return
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		t.Fatalf("decode %s: %v", envelope.Data, err)
	}
}

func (h *harness) call(method, path, workspaceID string, body any) apiResult {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if workspaceID != "" {
		req.Header.Set(e2eUserHeader, h.userID)
		req.Header.Set(e2eSpaceHeader, workspaceID)
	}
	resp, err := noRedirects().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return apiResult{status: resp.StatusCode, body: raw, header: resp.Header}
}

func noRedirects() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (h *harness) notify(body []byte) int {
	h.t.Helper()
	return h.notifyWithToken(e2ePathToken, body)
}

func (h *harness) notifyWithToken(token string, body []byte) int {
	h.t.Helper()
	resp, err := h.poster.Post(h.server.URL+"/webhooks/mercadolivre/"+token, "application/json", bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

type connectResult struct {
	status    string
	reason    string
	accountID string
}

func (h *harness) connectSeller(workspaceID string) connectResult {
	h.t.Helper()
	started := h.call(http.MethodGet, "/oauth/mercadolivre/start", workspaceID, nil)
	if started.status != http.StatusOK {
		h.t.Fatalf("start answered %d: %s", started.status, started.body)
	}
	var out struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	started.decode(h.t, &out)
	authorize, err := url.Parse(out.AuthorizeURL)
	if err != nil || authorize.Query().Get("code_challenge") == "" || authorize.Query().Get("state") == "" {
		h.t.Fatalf("authorize url %q carries no PKCE challenge or state", out.AuthorizeURL)
	}
	resp, err := noRedirects().Get(out.AuthorizeURL)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(back.String(), e2eRedirect) {
		h.t.Fatalf("the authorization page sent the browser to %q", resp.Header.Get("Location"))
	}
	callback := h.call(http.MethodGet, "/oauth/mercadolivre/callback?"+back.RawQuery, "", nil)
	if callback.status != http.StatusFound && callback.status != http.StatusSeeOther {
		h.t.Fatalf("callback answered %d: %s", callback.status, callback.body)
	}
	landed, err := url.Parse(callback.header.Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	q := landed.Query()
	return connectResult{status: q.Get("mercadolivre"), reason: q.Get("reason"), accountID: q.Get("accountId")}
}

func (h *harness) connectNewSeller(spec mlfake.SellerSpec) *mldomain.Account {
	h.t.Helper()
	h.fake.AddSeller(spec)
	h.fake.SetDefaultSeller(spec.ID)
	got := h.connectSeller(h.workspaceID)
	if got.status != "connected" || got.accountID == "" {
		h.t.Fatalf("connect of %s = %+v", spec.ID, got)
	}
	account, err := h.accounts.FindByID(h.ctx, got.accountID)
	if err != nil {
		h.t.Fatal(err)
	}
	return account
}

func (h *harness) enableAgent(accountID string) {
	h.t.Helper()
	res := h.call(http.MethodPatch, "/mercadolivre/accounts/"+accountID, h.workspaceID, map[string]any{"agentId": h.agentID, "enableAgentResponses": true})
	if res.status != http.StatusOK {
		h.t.Fatalf("enable the agent answered %d: %s", res.status, res.body)
	}
}

func (h *harness) buyerMessage(sellerID, packID, text string) string {
	h.t.Helper()
	id := h.fake.BuyerWrites(packID, text)
	if code := h.notify(h.fake.Notification("messages", sellerID, id, uuid.NewString())); code != http.StatusOK {
		h.t.Fatalf("the message notification answered %d", code)
	}
	h.settle()
	return id
}

func (h *harness) settle() {
	h.t.Helper()
	h.queue.waitIdle(h.t, 30*time.Second)
}

func (h *harness) rows(table, where string, args ...any) int64 {
	h.t.Helper()
	var n int64
	if err := h.db.Table(table).Where(where, args...).Count(&n).Error; err != nil {
		h.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (h *harness) inbound(conversationID string) int64 {
	return h.rows("conversation_messages", "entry_type = ? AND entry_id = ? AND sender_kind = ?", "mercadolivre", conversationID, string(conversation.SenderContact))
}

func (h *harness) aiReplies(conversationID string) int64 {
	return h.rows("conversation_messages", "entry_type = ? AND entry_id = ? AND sender_kind = ?", "mercadolivre", conversationID, string(conversation.SenderAI))
}

func sellerID(n int) string { return fmt.Sprintf("4154583%02d", n) }

func runes(s string) int { return len([]rune(s)) }
