package tiktok

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/agent"
	rules "vozko/domain/commentautomation"
	"vozko/domain/conversation"
	"vozko/domain/messaging"
	"vozko/domain/pipeline"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
	infracache "vozko/infra/cache"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	commentautomation_repository "vozko/infra/repositories/commentautomation"
	tiktok_repository "vozko/infra/repositories/tiktok"
	webhook_repository "vozko/infra/repositories/webhook"
	"vozko/infra/testenv"
	infratiktok "vozko/infra/tiktok"
	commentautomation "vozko/usecases/commentautomation"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/shared/automationrefs"
	"vozko/usecases/shared/oauthstate"
	ttuc "vozko/usecases/tiktok"
)

const (
	e2eAppID     = "7043848340043202562"
	e2eSecret    = "e2e-app-secret"
	e2eOpenID    = "_000e2eAccountOpenID"
	e2eWorkspace = "11111111-1111-4111-8111-111111111111"
	e2ePerson    = "+e2ePerson/AbC=="
	e2eConvID    = "S:1:0:e2e+conv/x="
	e2eVideoID   = "7203946942097902849"
)

func e2eDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
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
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "tt_e2e_" + uuid.NewString()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	models := append(schema.TikTokModels(), &schema.CommentRule{}, &schema.CommentRuleExecution{}, &schema.CommentPrivateReply{}, &schema.WebhookProcessedEvent{})
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range database.TikTokDDLStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type syncQueue struct {
	mu       sync.Mutex
	handlers map[string]func([]byte, messaging.MessageAck)
	pending  sync.WaitGroup
}

type queueAck struct {
	once sync.Once
	done func()
}

func (a *queueAck) Ack() error              { a.once.Do(a.done); return nil }
func (a *queueAck) Nack(requeue bool) error { a.once.Do(a.done); return nil }
func (a *queueAck) DeliveryCount() int      { return 1 }

func (q *syncQueue) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.handlers == nil {
		q.handlers = map[string]func([]byte, messaging.MessageAck){}
	}
	q.handlers[topic] = handler
	return nil
}

func (q *syncQueue) Publish(topic string, message []byte) error {
	q.mu.Lock()
	handler := q.handlers[topic]
	q.mu.Unlock()
	if handler == nil {
		return errors.New("no consumer for " + topic)
	}
	q.pending.Add(1)
	handler(message, &queueAck{done: q.pending.Done})
	return nil
}

func (q *syncQueue) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	return q.Publish(topic, message)
}
func (q *syncQueue) DeleteQueue(topic string) error           { return nil }
func (q *syncQueue) ValidateConnection() error                { return nil }
func (q *syncQueue) GetQueueLength(topic string) (int, error) { return 0, nil }
func (q *syncQueue) drain()                                   { q.pending.Wait() }

type fakePlatform struct {
	t       *testing.T
	mu      sync.Mutex
	server  *httptest.Server
	sends   []map[string]any
	replies []map[string]any
	lookups int
	authors map[string]map[string]any
	hooks   map[string]string
}

func newFakePlatform(t *testing.T) *fakePlatform {
	p := &fakePlatform{t: t, authors: map[string]map[string]any{}, hooks: map[string]string{}}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakePlatform) ok(w http.ResponseWriter, data any) {
	raw, _ := json.Marshal(data)
	_, _ = w.Write([]byte(`{"code":0,"message":"OK","request_id":"e2e","data":` + string(raw) + `}`))
}

func (p *fakePlatform) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	body := map[string]any{}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	switch r.URL.Path {
	case "/tt_user/oauth2/token/":
		p.ok(w, map[string]any{"access_token": "act.e2e", "expires_in": 86400, "open_id": e2eOpenID, "refresh_token": "rft.e2e", "refresh_token_expires_in": 31536000, "scope": "user.info.basic,video.list,comment.list,comment.list.manage,message.list.read,message.list.send"})
	case "/tt_user/token_info/get/":
		p.ok(w, map[string]any{"app_id": e2eAppID, "creator_id": e2eOpenID, "scope": "user.info.basic,video.list,comment.list,comment.list.manage,message.list.read,message.list.send"})
	case "/tt_user/oauth2/revoke/":
		p.ok(w, map[string]any{})
	case "/business/get/":
		p.ok(w, map[string]any{"username": "loja.e2e", "display_name": "Loja E2E", "has_advanced_access": true})
	case "/business/message/content/list/":
		p.ok(w, map[string]any{"messages": []any{}, "participants": []map[string]string{{"role": "PERSONAL_ACCOUNT", "id": e2ePerson, "display_name": "Ana Cliente"}}})
	case "/business/message/conversation/list/":
		p.ok(w, map[string]any{"conversations": []any{}, "has_more": false})
	case "/business/webhook/list/":
		p.ok(w, map[string]any{"app_id": e2eAppID, "event_type": r.URL.Query().Get("event_type"), "callback_url": p.hooks[r.URL.Query().Get("event_type")]})
	case "/business/webhook/update/":
		p.hooks[body["event_type"].(string)] = body["callback_url"].(string)
		p.ok(w, map[string]any{})
	case "/business/message/send/":
		p.sends = append(p.sends, body)
		p.ok(w, map[string]any{"message": map[string]string{"message_id": "out-" + strconv.Itoa(len(p.sends))}})
	case "/business/comment/list/":
		p.lookups++
		var ids []string
		_ = json.Unmarshal([]byte(r.URL.Query().Get("comment_ids")), &ids)
		comments := []any{}
		for _, id := range ids {
			if a, ok := p.authors[id]; ok {
				comments = append(comments, a)
			}
		}
		p.ok(w, map[string]any{"comments": comments, "has_more": false, "cursor": 0})
	case "/business/comment/reply/create/":
		p.replies = append(p.replies, body)
		p.ok(w, map[string]any{"comment_id": "79" + strconv.Itoa(len(p.replies)), "video_id": body["video_id"]})
	default:
		p.t.Errorf("unexpected TikTok call %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (p *fakePlatform) sendCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sends)
}

func (p *fakePlatform) replyCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.replies)
}

type e2eMetrics struct {
	mu     sync.Mutex
	events map[string]int
}

func (m *e2eMetrics) AddTikTokEvent(stage, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = map[string]int{}
	}
	m.events[stage+":"+outcome]++
}

type e2eAlerts struct{}

func (e2eAlerts) NeedsReconnect(ctx context.Context, a *ttdomain.Account) error    { return nil }
func (e2eAlerts) ReconnectSoon(ctx context.Context, a *ttdomain.Account) error     { return nil }
func (e2eAlerts) WebhooksUnhealthy(ctx context.Context, a *ttdomain.Account) error { return nil }
func (e2eAlerts) Healthy(ctx context.Context, a *ttdomain.Account) error           { return nil }

type e2eHistory struct {
	mu      sync.Mutex
	records []conversation.MessageHistoryRecord
}

func (h *e2eHistory) Record(ctx context.Context, r conversation.MessageHistoryRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

type e2eMessages struct{}

func (e2eMessages) GetByEntryAndExternalMessageID(t shared.EntryType, entryID, externalID string) (*conversation.Message, error) {
	return nil, errors.New("not stored")
}
func (e2eMessages) Update(id string, m *conversation.Message) error { return nil }

type e2eAnnouncer struct{}

func (e2eAnnouncer) BroadcastMessageUpdated(entryID, entryType string, m *conversation.Message) {}
func (e2eAnnouncer) BroadcastEntryUpdate(entryID, entryType string, m *conversation.Message)    {}

type e2eWatermarks struct{}

func (e2eWatermarks) MarkOutboundStatusUpTo(entryID string, t shared.EntryType, s conversation.DeliveryStatus, upTo time.Time) (int64, error) {
	return 0, nil
}

type e2eMedia struct{}

func (e2eMedia) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	return &conversation.ConversationMedia{ID: in.ID, URL: "https://media.example.com/" + in.Key, Type: in.Type}, nil
}

type e2eObjects struct{}

func (e2eObjects) UploadFile(key string, data []byte, contentType string) error { return nil }
func (e2eObjects) DeleteFile(ctx context.Context, key string) error             { return nil }
func (e2eObjects) GetFileURL(key string) string                                 { return "https://media.example.com/" + key }

type e2eFetcher struct{}

func (e2eFetcher) Fetch(ctx context.Context, u string) ([]byte, error) {
	return nil, errors.New("no remote files in the end-to-end test")
}

type e2eAssignments struct{}

func (e2eAssignments) EnsureAssignment(entryID, entryType, accountID string) string { return "" }

type e2eAudience struct {
	mu       sync.Mutex
	enqueued []string
}

func (a *e2eAudience) Enqueue(ctx context.Context, c *ttdomain.Comment) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enqueued = append(a.enqueued, c.CommentID)
}

func (a *e2eAudience) Forget(ctx context.Context, account *ttdomain.Account, commentID string) {}

type e2eDispatch struct {
	mu    sync.Mutex
	texts []string
}

func (d *e2eDispatch) Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.texts = append(d.texts, in.Text)
	return nil
}

func signDelivery(body []byte, at time.Time) string {
	var compact bytes.Buffer
	_ = json.Compact(&compact, body)
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(e2eSecret))
	mac.Write([]byte(ts + "." + compact.String()))
	return "t=" + ts + ",s=" + hex.EncodeToString(mac.Sum(nil))
}

func deliver(t *testing.T, h *WebhookHandler, queue *syncQueue, event string, content map[string]any) {
	t.Helper()
	inner, _ := json.Marshal(content)
	body, _ := json.Marshal(map[string]any{"client_key": e2eAppID, "event": event, "create_time": time.Now().Unix(), "user_openid": e2eOpenID, "content": string(inner)})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok", bytes.NewReader(body))
	req.Header.Set(signatureHeader, signDelivery(body, time.Now()))
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook %s answered %d: %s", event, rec.Code, rec.Body.String())
	}
	queue.drain()
}

func TestTikTokEndToEnd(t *testing.T) {
	db := e2eDB(t)
	ctx := context.Background()
	redis := infracache.NewRedisProvider(testenv.Redis(t).Addr, "")
	platform := newFakePlatform(t)
	metrics := &e2eMetrics{}

	client, err := infratiktok.NewClient(infratiktok.Config{
		BaseURL: platform.server.URL, AppID: e2eAppID, AppSecret: e2eSecret,
		RedirectURI:        "https://api.example.com/oauth/tiktok/callback/",
		AuthorizeURL:       "https://www.tiktok.com/v2/auth/authorize?client_key=" + e2eAppID,
		RateLimiterFactory: redis.RateLimiterFactory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	errs := infratiktok.Classifier{}
	services := ttuc.Services{OAuth: client, Profiles: client, Messaging: client, Videos: client, Comments: client, Webhooks: client, Errors: errs}
	repos := ttuc.Repositories{
		Accounts: tiktok_repository.NewAccountRepository(db), Contacts: tiktok_repository.NewContactRepository(db),
		Conversations: tiktok_repository.NewConversationRepository(db), Videos: tiktok_repository.NewVideoRepository(db),
		Comments: tiktok_repository.NewCommentRepository(db), Purge: tiktok_repository.NewPurgeRepository(db),
	}
	tokens := ttuc.NewTokenSource(repos.Accounts, client, errs, e2eAlerts{}, metrics)
	nonces, err := oauthstate.NewNonceStore(redis.SharedState(), "tt:e2e:oauth:"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := oauthstate.NewIssuer("e2e-state-secret", nonces, ttuc.AccountsPath)
	if err != nil {
		t.Fatal(err)
	}
	images := ttuc.NewImageStore(e2eFetcher{}, e2eObjects{})
	webhooks := ttuc.NewWebhookSubscriptions(client, "https://api.example.com/webhooks/tiktok", metrics)
	connect := ttuc.NewConnectUseCase(ttuc.ConnectDeps{Accounts: repos.Accounts, Services: services, States: issuer, Webhooks: webhooks, Images: images, Alerts: e2eAlerts{}, Metrics: metrics})

	start, err := connect.Start(ctx, ttuc.StartConnectInput{WorkspaceID: e2eWorkspace, UserID: "user-1", ReturnPath: ttuc.AccountsPath})
	if err != nil {
		t.Fatal(err)
	}
	authorize, _ := url.Parse(start.AuthorizeURL)
	connected, err := connect.Complete(ctx, ttuc.CompleteConnectInput{Code: "auth-code", State: authorize.Query().Get("state")})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	account := connected.Account
	if !account.CanSend() || !account.CanManageCommentsNow() || !account.WebhooksHealthy {
		t.Fatalf("connected account = %+v", account)
	}

	history := &e2eHistory{}
	audience := &e2eAudience{}
	dispatch := &e2eDispatch{}
	side := ttuc.NewSideMessages(redis.SharedState())
	handler := ttuc.NewHandleWebhook(ttuc.HandleWebhookDeps{
		Repos: repos, Tokens: tokens, Messaging: client, Errors: errs, History: history, Messages: e2eMessages{}, Announcer: e2eAnnouncer{},
		Watermarks: e2eWatermarks{}, Media: e2eMedia{}, Images: images, Assignments: e2eAssignments{}, Automation: dispatch,
		Comments: audience, Side: side, Metrics: metrics,
	})
	queue := &syncQueue{}
	consumers := ttuc.NewConsumers(ttuc.ConsumerDeps{
		QueueSub: queue, QueuePub: queue, SharedState: redis.SharedState(), Durable: webhook_repository.NewProcessedEventRepository(db),
		Messages: handler, Comments: handler, Errors: errs, Metrics: metrics, Concurrency: 4,
	})
	if err := consumers.Start(); err != nil {
		t.Fatal(err)
	}
	webhook := NewWebhookHandler(e2eSecret, queue, metrics)

	messageID := "in-" + uuid.NewString()
	inbound := map[string]any{
		"from": "ana", "to": "loja.e2e", "unique_identifier": e2ePerson, "from_user": map[string]string{"id": e2ePerson, "role": "personal_account"},
		"to_user": map[string]string{"id": e2eOpenID, "role": "business_account"}, "conversation_id": e2eConvID, "message_id": messageID,
		"timestamp": time.Now().UnixMilli(), "type": "text", "text": map[string]string{"body": "Oi! Vocês entregam?"}, "message_tag": map[string]string{"source": "APP"},
	}
	deliver(t, webhook, queue, "im_receive_msg", inbound)
	deliver(t, webhook, queue, "im_receive_msg", inbound)

	conv, err := repos.Conversations.FindByConversationID(ctx, account.ID, e2eConvID)
	if err != nil {
		t.Fatalf("the conversation was not opened: %v", err)
	}
	if len(history.records) != 1 || history.records[0].Text != "Oi! Vocês entregam?" || len(dispatch.texts) != 1 {
		t.Fatalf("a redelivered message was processed twice: history %d automation %d", len(history.records), len(dispatch.texts))
	}
	if contact, err := repos.Contacts.FindByUniqueIdentifier(ctx, account.ID, e2ePerson); err != nil || contact.DisplayName != "Ana Cliente" {
		t.Fatalf("the sender profile was not refreshed: %+v %v", contact, err)
	}

	adapter := ttuc.NewChannelAdapter(ttuc.ChannelAdapterDeps{
		Accounts: repos.Accounts, Contacts: repos.Contacts, Conversations: repos.Conversations, Tokens: tokens, Messaging: client,
		Errors: errs, Fetcher: e2eFetcher{}, Side: side, Metrics: metrics,
	})
	ec, err := adapter.ResolveEntry(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ttdomain.OutboundBudget; i++ {
		if _, err := adapter.SendText(ctx, ec, conversation.SendTextRequest{Body: fmt.Sprintf("Resposta %d", i+1)}); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if _, err := adapter.SendText(ctx, ec, conversation.SendTextRequest{Body: "Mais uma"}); !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("the eleventh message must be refused: %v", err)
	}
	if platform.sendCount() != ttdomain.OutboundBudget {
		t.Fatalf("TikTok received %d messages", platform.sendCount())
	}
	state, _ := adapter.WindowState(ctx, ec)
	if state.Open || state.Reason != conversation.WindowReasonBudgetExhausted {
		t.Fatalf("window = %+v", state)
	}
	inbound["message_id"] = "in-" + uuid.NewString()
	inbound["timestamp"] = time.Now().UnixMilli()
	deliver(t, webhook, queue, "im_receive_msg", inbound)
	if _, err := adapter.SendText(ctx, ec, conversation.SendTextRequest{Body: "Voltamos a falar"}); err != nil {
		t.Fatalf("a new customer message must reopen the budget: %v", err)
	}

	t.Run("comment rules answer each comment exactly once", func(t *testing.T) {
		ruleRepo := commentautomation_repository.NewRuleRepository(db)
		rule := &rules.Rule{WorkspaceID: e2eWorkspace, Source: shared.EntryTypeTikTok, AccountID: account.ID, Name: "Preço", Enabled: true, Match: rules.MatchContains, Keywords: []string{"preço"}, Actions: []rules.Action{rules.ActionPublicReply}, PublicReplyText: "Oi {{username}}! Te chamamos no direct."}
		if err := ruleRepo.Create(ctx, rule); err != nil {
			t.Fatal(err)
		}
		comments := ttuc.NewComments(ttuc.CommentsDeps{Accounts: repos.Accounts, Videos: repos.Videos, Comments: repos.Comments, Tokens: tokens, Services: services, Images: images, Metrics: metrics})
		worker := ttuc.NewCommentWorker(ttuc.CommentWorkerDeps{
			Accounts: repos.Accounts, Contacts: repos.Contacts, Comments: repos.Comments, Tokens: tokens, Service: client, Errors: errs,
			Rules: commentautomation.NewEvaluator(ruleRepo, commentautomation_repository.NewExecutionClaimRepository(db)), Actions: ttuc.NewRuleActions(repos.Accounts, comments),
			Audience: audience, Metrics: metrics,
		})
		commentID := "72473035764185" + strconv.Itoa(int(time.Now().UnixNano()%100000))
		platform.mu.Lock()
		platform.authors[commentID] = map[string]any{"comment_id": commentID, "video_id": e2eVideoID, "unique_identifier": "+commenter==", "username": "bia", "display_name": "Bia", "text": "Qual o preço?", "status": "PUBLIC", "owner": false, "create_time": strconv.FormatInt(time.Now().Unix(), 10)}
		platform.mu.Unlock()
		event := map[string]any{"comment_id": json.Number(commentID), "video_id": json.Number(e2eVideoID), "comment_type": "comment", "comment_action": "insert", "unique_identifier": "+commenter==", "timestamp": time.Now().UnixMilli(), "text": "Qual o preço?"}
		deliver(t, webhook, queue, "comment.update", event)
		deliver(t, webhook, queue, "comment.update", event)
		for i := 0; i < 3; i++ {
			if err := worker.Execute(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if platform.replyCount() != 1 {
			t.Fatalf("the rule replied %d times, want exactly once", platform.replyCount())
		}
		stored, err := repos.Comments.FindByCommentID(ctx, account.ID, commentID)
		if err != nil || stored.RulesDue || stored.AuthorUsername != "bia" || stored.ContactID == nil {
			t.Fatalf("comment = %+v err %v", stored, err)
		}
	})

	accounts := ttuc.NewAccounts(ttuc.AccountsDeps{Accounts: repos.Accounts, OAuth: client, References: refsAllowingNothing(), Alerts: e2eAlerts{}, Metrics: metrics})
	out, err := accounts.Disconnect(ctx, e2eWorkspace, account.ID)
	if err != nil || out.Warning != "" {
		t.Fatalf("disconnect %+v err %v", out, err)
	}
	if _, err := repos.Accounts.FindLiveByOpenID(ctx, e2eOpenID); !errors.Is(err, ttdomain.ErrAccountNotFound) {
		t.Fatal("a disconnected account must stop receiving events")
	}
	inbound["message_id"] = "in-" + uuid.NewString()
	deliver(t, webhook, queue, "im_receive_msg", inbound)
	if metrics.events["consume:unknown_account"] == 0 {
		t.Errorf("events for a disconnected account must be dropped and counted: %v", metrics.events)
	}
}

type noAgents struct{}

func (noAgents) FindByID(id string) (*agent.Agent, error) { return nil, errors.New("none") }

type noWorkflows struct{}

func (noWorkflows) FindByID(id string) (*workflow.Workflow, error) { return nil, errors.New("none") }

type noPipelines struct{}

func (noPipelines) GetByID(workspaceID, id string) (*pipeline.Pipeline, error) {
	return nil, errors.New("none")
}

type noDepartments struct{}

func (noDepartments) GetDepartmentByID(id string) (*wd.Department, error) {
	return nil, errors.New("none")
}

func refsAllowingNothing() automationrefs.Finders {
	return automationrefs.Finders{Agents: noAgents{}, Workflows: noWorkflows{}, Pipelines: noPipelines{}, Departments: noDepartments{}}
}
