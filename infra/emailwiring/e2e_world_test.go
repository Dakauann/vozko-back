package emailwiring_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"vozko/delivery/http/emailwebhooks"
	"vozko/delivery/http/emailwebhooks/snstest"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/webhook"
	"vozko/domain/workspace/workspace_pricing"
	infracache "vozko/infra/cache"
	"vozko/infra/cache/redistest"
	"vozko/infra/campaignaudiencewiring"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/emailwiring"
	balance_repository "vozko/infra/repositories/balance"
	conversation_repository "vozko/infra/repositories/conversation"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	emailsending_repository "vozko/infra/repositories/emailsending"
	lead_repository "vozko/infra/repositories/lead"
	"vozko/infra/repositories/repotest"
	webhook_repository "vozko/infra/repositories/webhook"
	workspace_pricing_repository "vozko/infra/repositories/workspace_pricing"
	"vozko/infra/ses"
	"vozko/infra/ses/sesfake"
	"vozko/infra/testenv"
	balance_usecase "vozko/usecases/balance"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	conversation_usecase "vozko/usecases/conversation"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
	"vozko/usecases/emailoutreach"
	esuc "vozko/usecases/emailsending"
	"vozko/usecases/transactionalemail"
	webhook_usecase "vozko/usecases/webhook"
	workspace_pricing_usecase "vozko/usecases/workspace_pricing"
)

const (
	probationDomain        = "envios.vozko-e2e.com.br"
	trustedDomain          = "mail.vozko-e2e.net"
	publicBaseURL          = "https://app.vozko-e2e.example"
	tokenSecret            = "e2e-email-token-secret-0123456789abcdef"
	eventsTopic            = "arn:aws:sns:sa-east-1:000000000000:email-events"
	inboundTopic           = "arn:aws:sns:sa-east-1:000000000000:email-inbound"
	certURL                = "https://sns.sa-east-1.amazonaws.com/SimpleNotificationService-e2e.pem"
	productionMaxOpenConns = 10
	snsStampLayout         = "2006-01-02T15:04:05.000Z"
	campaignWorkers        = 8
)

var e2eRedisDBs = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}

type world struct {
	t   *testing.T
	ctx context.Context
	db  *gorm.DB

	shared      cache.SharedState
	ses         *sesfake.Account
	queue       *steppedQueue
	alerts      *episodes
	ops         *opsAlerts
	zones       *route53Zones
	plans       *paidPlans
	assignments *assignments
	automation  *automation
	events      *conversationEvents
	announcer   *announcer
	history     *flakyHistory
	signer      snstest.KeyPair
	router      *mux.Router
	codec       *emailtoken.Codec
	files       *cdnFiles

	pricing  workspace_pricing.Repository
	pricer   workspace_pricing.Pricer
	balances balance.Repository
	billing  *balance_usecase.ConsumeEmailUseCase

	module        *esuc.Module
	hygiene       *emailwiring.Hygiene
	resolver      *e2eResolver
	campaigns     *emailwiring.Campaigns
	audience      *campaignaudience_usecase.Service
	inbox         *emailwiring.Inbox
	transactional *transactionalemail.Service
	relay         *transactionalemail.Relay
	outreach      *emailoutreach.StartConversation

	campaignRepo  *emailcampaign_repository.CampaignRepository
	recipientRepo *emailcampaign_repository.RecipientRepository
	billingRepo   *emailcampaign_repository.BillingRepository
	accounts      es.AccountRepository
	senders       es.SenderRepository
	domains       es.DomainRepository
	messages      es.MessageRepository
	suppressions  es.SuppressionRepository
	runs          es.RunMessages
}

type tenant struct {
	ID     string
	Name   string
	Actor  conversation.Viewer
	Start  int64
	Sender es.Sender
	Domain es.SendingDomain
}

func e2eDB(t *testing.T) *gorm.DB {
	t.Helper()
	testenv.Postgres(t)
	testenv.Redis(t)
	repotest.WithPII(t)
	db := repotest.IsolatedDB(t, "email_e2e")
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	pool.SetMaxOpenConns(productionMaxOpenConns)
	pool.SetMaxIdleConns(productionMaxOpenConns)
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	models := append(append(append([]any{}, schema.EmailSendingModels()...), schema.EmailCampaignModels()...), schema.EmailInboxModels()...)
	models = append(models,
		&schema.Workspace{}, &schema.Balance{}, &schema.BalanceTransaction{}, &schema.PricingItem{}, &schema.EmailAPISend{},
		&schema.Lead{}, &schema.LeadCompany{}, &schema.ConversationMessage{}, &schema.WebhookProcessedEvent{},
		&schema.ConversationMedia{},
	)
	models = append(models, schema.CampaignAudienceModels()...)
	models = append(models, repotest.LeadStoreModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{"ux_email_contact_workspace_address", "ux_email_conversation_workspace_contact", "ux_cm_entry_external_msgid", "idx_leads_workspace_email_lower"} {
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

func e2eRedis(t *testing.T) cache.SharedState {
	t.Helper()
	target := testenv.Redis(t)
	db := redistest.ExclusiveDB(t, target.Addr, target.Password, e2eRedisDBs)
	return infracache.NewRedisProviderOnDB(target.Addr, target.Password, db).SharedState()
}

func newWorld(t *testing.T) *world {
	t.Helper()
	return buildWorld(t, true)
}

func newWorldCheckingDomainsInTheBackground(t *testing.T) *world {
	t.Helper()
	return buildWorld(t, false)
}

func buildWorld(t *testing.T, resolveBeforeSend bool) *world {
	t.Helper()
	db := e2eDB(t)
	w := &world{
		t: t, ctx: context.Background(), db: db, shared: e2eRedis(t), ses: sesfake.New(), queue: newSteppedQueue(),
		alerts: &episodes{}, ops: &opsAlerts{}, assignments: &assignments{}, automation: &automation{},
		events: &conversationEvents{}, announcer: &announcer{}, signer: snstest.NewKeyPair(t, time.Now().Add(24*time.Hour)),
		files: newCDNFiles(), zones: newRoute53Zones(t), plans: &paidPlans{},
	}
	w.pricing = workspace_pricing_repository.NewRepository(db)
	w.must(w.pricing.SeedDefaults(workspace_pricing.DefaultPricingCatalog))
	w.pricer = workspace_pricing.NewPricer(w.pricing)
	w.balances = balance_repository.NewRepository(db)
	w.billing = balance_usecase.NewConsumeEmailUseCase(w.balances, w.pricer, activeSubscription{})
	codec, err := emailtoken.NewCodec(tokenSecret)
	w.must(err)
	w.codec = codec
	leads := lead_repository.NewRepository(db)

	w.resolver = newE2EResolver()
	w.hygiene, err = emailwiring.NewHygiene(w.ctx, db, emailwiring.HygieneAdapters{Resolver: w.resolver, Source: snapshotSource{}, QueuePub: w.queue})
	w.must(err)
	var sendHygiene esuc.RecipientHygiene = w.hygiene.Service
	if resolveBeforeSend {
		sendHygiene = checkedBeforeSend{Service: w.hygiene.Service}
	}
	w.module, err = emailwiring.NewSendingModule(db, emailwiring.SendingAdapters{
		Provider: w.ses, DNS: w.zones, DMARC: quietDNS{}, Records: quietDNS{}, Monitor: quietDNS{}, Decoder: ses.Decoder{},
		Alerts: w.alerts, Ops: w.ops, Tenants: w.ses, Routes: w.ses, Quota: w.ses, Shared: w.shared, QueuePub: w.queue, Codec: codec,
		Hygiene: sendHygiene, Plans: w.plans, Config: esuc.Config{ProbationDomain: probationDomain, TrustedDomain: trustedDomain, PublicBaseURL: publicBaseURL},
	})
	w.must(err)
	audienceModule, err := campaignaudiencewiring.New(db, campaignaudiencewiring.Adapters{Selections: everyLead{db: db}, Leads: leads, Definitions: noLeadFields{}, Gate: openAudienceGate{}, Shared: w.shared, EmailsSeen: w.hygiene.Service})
	w.must(err)
	w.audience = audienceModule.Service
	w.campaigns, err = emailwiring.NewCampaigns(db, emailwiring.CampaignAdapters{
		Audience: w.audience,
		Sending:  w.module, Codec: codec, Billing: w.billing, Selection: everyLead{db: db}, Leads: leads, Wallet: w.balances,
		Rates: workspace_pricing_usecase.NewGetExchangeRateUseCase(w.pricing), QueuePub: w.queue, QueueSub: w.queue,
		Shared: w.shared, Memo: infracache.NewSharedStateMemo(w.shared, 5*time.Second), Notifications: w.alerts, PublicBaseURL: publicBaseURL,
		Hygiene: checkedBeforeReview{Service: w.hygiene.Service},
	})
	w.must(err)
	messages := conversation_repository.NewRepository(db)
	w.history = &flakyHistory{MessageHistoryManager: conversation_usecase.NewMessageHistoryManager(messages)}
	w.inbox, err = emailwiring.NewInbox(db, emailwiring.InboxAdapters{
		Sending: w.module, Raw: w.ses, Codec: codec, Consent: w.campaigns.Consent, Billing: w.billing, Leads: leads,
		History: w.history, Messages: messages, Media: refusingMedia{}, Assignments: w.assignments, Automation: w.automation,
		Claims: webhook_repository.NewProcessedEventRepository(db), Events: w.events, Announcer: w.announcer, Originals: w.campaigns.Service,
		Files: w.files, EmailsSeen: w.hygiene.Service,
	})
	w.must(err)
	w.transactional, err = emailwiring.NewTransactional(db, emailwiring.TransactionalAdapters{Gateway: w.module.Gateway, Billing: w.billing, Leads: leads, Inbox: w.inbox})
	w.must(err)
	w.relay = transactionalemail.NewRelay()
	w.relay.Bind(w.transactional)
	w.outreach, err = emailwiring.NewOutreach(db, emailwiring.OutreachAdapters{Sender: w.relay, Inbox: w.inbox, Assignments: w.assignments})
	w.must(err)

	setup, err := w.module.NewSetupConsumer(w.queue)
	w.must(err)
	w.must(setup.Start())
	consumer, err := w.module.NewConsumer(esuc.ConsumerDeps{QueueSub: w.queue, QueuePub: w.queue, SharedState: w.shared, Durable: webhook_repository.NewProcessedEventRepository(db)})
	w.must(err)
	w.must(consumer.Start())
	intake, err := esuc.NewNotificationIntake(webhook_usecase.NewPublishWebhookUseCase(w.queue))
	w.must(err)
	verifier, err := emailwebhooks.NewVerifier(sesfake.Region, snstest.NewFetcher(certURL, w.signer.Cert))
	w.must(err)
	w.router = mux.NewRouter()
	emailwebhooks.RegisterPublicRoutes(w.router, emailwebhooks.NewHandler(emailwebhooks.Deps{
		Verifier: verifier, AllowedTopics: []string{eventsTopic, inboundTopic}, Intake: intake, Confirmer: emailwebhooks.NewHTTPConfirmer(nil),
	}))
	w.must(w.campaigns.Service.StartConsumer())
	w.must(w.module.SendQuota.Execute(w.ctx))

	w.campaignRepo = emailcampaign_repository.NewCampaignRepository(db)
	w.recipientRepo = emailcampaign_repository.NewRecipientRepository(db)
	w.billingRepo = emailcampaign_repository.NewBillingRepository(db)
	w.accounts = emailsending_repository.NewAccountRepository(db)
	w.senders = emailsending_repository.NewSenderRepository(db)
	w.domains = emailsending_repository.NewDomainRepository(db)
	w.messages = emailsending_repository.NewMessageRepository(db)
	w.suppressions = emailsending_repository.NewSuppressionRepository(db)
	w.runs = emailsending_repository.NewRunMessages(db)
	return w
}

func (w *world) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) newTenant(name string, wallet int64) *tenant {
	w.t.Helper()
	ws := &tenant{ID: uuid.NewString(), Name: name, Start: wallet}
	ws.Actor = conversation.Viewer{UserID: uuid.NewString(), WorkspaceID: ws.ID, IsAdmin: true}
	w.must(w.db.Exec("INSERT INTO workspaces (id, name, created_at, updated_at) VALUES (?, ?, now(), now())", ws.ID, name).Error)
	w.must(w.db.Create(&schema.Balance{ID: uuid.NewString(), WorkspaceID: ws.ID, Amount: wallet, Currency: "USD"}).Error)
	return ws
}

func (w *world) promoteBeforeSetup(ws *tenant) {
	w.t.Helper()
	tenantName, err := es.TenantNameFor(ws.ID)
	w.must(err)
	promoted := time.Now().UTC().Add(-30 * 24 * time.Hour)
	w.must(w.accounts.Create(w.ctx, &es.SenderAccount{
		WorkspaceID: ws.ID, Provider: sesfake.ProviderName, TenantName: tenantName, Mode: es.ModeTrusted, Status: es.AccountActive,
		Slug: es.SlugFor(ws.Name, ws.ID, func(string) bool { return false }), ProbationStartedAt: promoted.Add(-15 * 24 * time.Hour), PromotedAt: &promoted,
	}))
}

func (w *world) verifyDomains(ws *tenant) {
	w.t.Helper()
	w.ses.PublishDNS()
	w.must(w.module.Verification.Sweep(w.ctx))
	sender, err := w.senders.GetDefault(w.ctx, ws.ID)
	w.must(err)
	ws.Sender = *sender
	domain, err := w.domains.Get(w.ctx, ws.ID, sender.DomainID)
	w.must(err)
	ws.Domain = *domain
}

func (w *world) runSetup() int {
	w.t.Helper()
	return w.queue.drain(w.t, es.SetupFireTopic, 1, 0)
}

func (w *world) sendingTenant(name string, wallet int64) *tenant {
	w.t.Helper()
	ws := w.newTenant(name, wallet)
	w.promoteBeforeSetup(ws)
	_, err := w.module.Setup.Provision(w.ctx, ws.ID, "")
	w.must(err)
	w.runSetup()
	w.verifyDomains(ws)
	return ws
}

func (w *world) unit(ws *tenant) int64 {
	w.t.Helper()
	q, err := w.billing.QuoteRun(w.ctx, ws.ID, 1)
	w.must(err)
	return q.UnitMicros
}

func (w *world) setSendPrice(price int64) {
	w.t.Helper()
	err := w.db.Model(&schema.PricingItem{}).
		Where("workspace_id IS NULL AND category = ? AND service = ? AND metric = ?", string(workspace_pricing.CategoryEmail), workspace_pricing.EmailServiceSend, workspace_pricing.EmailMetricPerMessage).
		Update("price_micros", price).Error
	w.must(err)
	invalidator, ok := w.pricer.(interface{ InvalidateResolvedCache(string) })
	if !ok {
		w.t.Fatal("the pricer cannot drop its cache after a Tarifas edit")
	}
	invalidator.InvalidateResolvedCache("")
}

func (w *world) wallet(ws *tenant) int64 {
	w.t.Helper()
	b, err := w.balances.GetByWorkspaceID(ws.ID)
	w.must(err)
	return b.Amount
}

func (w *world) topUp(ws *tenant, amount int64) {
	w.t.Helper()
	ref := "topup:" + uuid.NewString()
	_, err := w.balances.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID: ws.ID, Amount: amount, ServiceType: balance.ServiceTopUp, ReferenceID: &ref, Description: "Recarga e2e", OncePerReference: true,
	})
	w.must(err)
}

func (w *world) ledger(ws *tenant, kind, pattern string) []schema.BalanceTransaction {
	w.t.Helper()
	var rows []schema.BalanceTransaction
	w.must(w.db.Where("workspace_id = ? AND type = ? AND reference_id LIKE ?", ws.ID, kind, pattern).Order("created_at").Find(&rows).Error)
	return rows
}

func sumOf(rows []schema.BalanceTransaction) int64 {
	var total int64
	for _, r := range rows {
		total += r.Amount
	}
	return total
}

func (w *world) assertLedger(ws *tenant) {
	w.t.Helper()
	repotest.AssertLedgerChains(w.t, w.db, ws.ID, ws.Start)
}

func (w *world) assertLedgerOwingOnlyForReplies(ws *tenant) {
	w.t.Helper()
	repotest.AssertLedgerChainsAllowingDebt(w.t, w.db, ws.ID, ws.Start, func(row schema.BalanceTransaction) bool {
		return row.Type == string(balance.TransactionTypeCredit) || (row.ReferenceID != nil && strings.HasPrefix(*row.ReferenceID, "email_receive:"))
	})
}

func (w *world) seedLeads(ws *tenant, n int, label string) []schema.Lead {
	w.t.Helper()
	leads := make([]schema.Lead, n)
	for i := range leads {
		leads[i] = schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: fmt.Sprintf("Cliente %s %d", label, i),
			Email: schema.OptionalText(fmt.Sprintf("%s%05d@%s.example", label, i, ws.ID[:8]))}
	}
	w.must(w.db.CreateInBatches(leads, 500).Error)
	w.mailedBefore(ws, leads)
	return leads
}

func readyDocument() emaildesign.Document {
	return emaildesign.Document{
		Version: emaildesign.CurrentVersion,
		Blocks: []emaildesign.Block{
			{ID: "h1", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Olá {{first_name|cliente}}", Level: 1}},
			{ID: "b1", Kind: emaildesign.KindButton, Button: &emaildesign.Button{Label: "Comprar", Href: "https://loja.example/oferta"}},
			{ID: "f1", Kind: emaildesign.KindFooter, Footer: &emaildesign.Footer{CompanyName: "Loja", Address: "Rua A, 1"}},
		},
	}
}

func (w *world) launch(ws *tenant, recipients int) emailcampaign.Campaign {
	w.t.Helper()
	w.seedLeads(ws, recipients, "lote")
	return w.campaignToEveryone(ws)
}

func (w *world) campaignToEveryone(ws *tenant) emailcampaign.Campaign {
	w.t.Helper()
	everyone := selection.Selection{Mode: selection.ModeEveryone}
	draft, err := w.campaigns.Service.Create(w.ctx, ws.Actor, emailcampaign.Draft{
		Name: "Black Friday", Subject: "Oferta para {{first_name|você}}", Document: readyDocument(), Selection: &everyone,
	})
	w.must(err)
	launched, err := w.campaigns.Service.Launch(w.ctx, ws.Actor, draft.ID, nil)
	w.must(err)
	return *launched
}

func campaignTopic(c emailcampaign.Campaign) string {
	return emailcampaign_usecase.QueueNamespace.DispatchTopic(c.ID)
}

func (w *world) dispatch(c emailcampaign.Campaign, workers, limit int) int {
	w.t.Helper()
	return w.queue.drain(w.t, campaignTopic(c), workers, limit)
}

func (w *world) campaign(c emailcampaign.Campaign) emailcampaign.Campaign {
	w.t.Helper()
	got, err := w.campaignRepo.GetByID(w.ctx, c.ID)
	w.must(err)
	return *got
}

func (w *world) counts(c emailcampaign.Campaign) emailcampaign.RecipientCounts {
	w.t.Helper()
	counts, err := w.recipientRepo.Counts(w.ctx, c.ID)
	w.must(err)
	return counts
}

func (w *world) billingState(c emailcampaign.Campaign) emailcampaign.BillingState {
	w.t.Helper()
	s, err := w.billingRepo.BillingState(w.ctx, c.ID)
	w.must(err)
	return s
}

func (w *world) batchDebits(ws *tenant, c emailcampaign.Campaign) []schema.BalanceTransaction {
	return w.ledger(ws, "debit", "email_campaign:"+c.ID+":batch:%")
}

func (w *world) batchRefunds(ws *tenant, c emailcampaign.Campaign) []schema.BalanceTransaction {
	return w.ledger(ws, "credit", "refund:email_campaign:"+c.ID+":batch:%")
}

func (w *world) campaignMessages(c emailcampaign.Campaign) int64 {
	w.t.Helper()
	var n int64
	w.must(w.db.Raw("SELECT count(*) FROM email_messages WHERE run_kind = ? AND run_id = ? AND status <> ?", string(es.RunCampaign), c.ID, string(es.MessageFailed)).Scan(&n).Error)
	return n
}

func (w *world) assertSlots(c emailcampaign.Campaign) {
	w.t.Helper()
	counts := w.counts(c)
	slots := w.billingState(c).Slots
	if counts.Sent > slots.Used || slots.Used > slots.Paid {
		w.t.Fatalf("sent %d, used %d, paid %d: want sent <= used <= paid", counts.Sent, slots.Used, slots.Paid)
	}
	if messages := w.campaignMessages(c); messages != counts.Sent {
		w.t.Fatalf("%d message rows for %d sent recipients", messages, counts.Sent)
	}
}

func (w *world) assertBatches(ws *tenant, c emailcampaign.Campaign, units ...int64) {
	w.t.Helper()
	debits := w.batchDebits(ws, c)
	if len(debits) != len(units) {
		w.t.Fatalf("batch debits = %d, want %d", len(debits), len(units))
	}
	state := w.billingState(c)
	for i, want := range units {
		batch := state.Batches[i]
		if batch.Units != want || !batch.Charged() || debits[i].Amount != batch.TotalMicros || batch.TotalMicros != want*batch.UnitMicros {
			w.t.Fatalf("batch %d = %+v debited %d, want %d units at its own price", batch.No, batch, debits[i].Amount, want)
		}
		if *debits[i].ReferenceID != balance_usecase.EmailCampaignBatchReference(c.ID, batch.No) {
			w.t.Fatalf("debit %d references %s", i, *debits[i].ReferenceID)
		}
	}
}

func (w *world) postSNS(topic string, message []byte) {
	w.t.Helper()
	w.postSNSAs(uuid.NewString(), topic, message)
}

func (w *world) postSNSAs(snsMessageID, topic string, message []byte) {
	w.t.Helper()
	subject := "Amazon SES Email Event Notification"
	env := emailwebhooks.Envelope{
		Type: "Notification", MessageID: snsMessageID, TopicArn: topic, Subject: &subject, Message: string(message),
		Timestamp: time.Now().UTC().Format(snsStampLayout), SignatureVersion: "2", SigningCertURL: certURL,
	}
	env.Signature = w.signer.Sign(w.t, emailwebhooks.CanonicalString(env), env.SignatureVersion)
	raw, err := json.Marshal(env)
	w.must(err)
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, emailwebhooks.SNSPath, bytes.NewReader(raw)))
	if rec.Code != http.StatusOK {
		w.t.Fatalf("SNS answered %d: %s", rec.Code, rec.Body.String())
	}
	w.queue.drain(w.t, webhook.TopicEmailNotification, 1, 0)
	if retried := w.queue.delayedCount(webhook.TopicEmailNotification); retried != 0 {
		w.t.Fatalf("the notification failed and was queued for a retry (%d waiting)", retried)
	}
	if dead := w.queue.deadLetters(webhook.TopicEmailNotification); dead != 0 {
		w.t.Fatalf("%d notifications were dead lettered", dead)
	}
}

func (w *world) providerEvent(build func(sesfake.Sent, time.Time) []byte, sent sesfake.Sent) {
	w.t.Helper()
	w.postSNS(eventsTopic, build(sent, time.Now().UTC()))
}

func (w *world) onlySentTo(address emailaddress.Address) sesfake.Sent {
	w.t.Helper()
	sent := w.ses.SentTo(address)
	if len(sent) != 1 {
		w.t.Fatalf("provider sends to %s = %d, want exactly one", address, len(sent))
	}
	return sent[0]
}

type transcriptRow struct {
	ID                string
	EntryID           string
	ExternalMessageID string
	DeliveryStatus    string
	SenderKind        string
	SenderID          string
	MessageType       string
	Text              string
	Metadata          string
	CreatedAt         time.Time
}

func (w *world) transcript(conversationID string) []transcriptRow {
	w.t.Helper()
	var rows []transcriptRow
	w.must(w.db.Raw(`SELECT id, entry_id, COALESCE(external_message_id, '') AS external_message_id, delivery_status, sender_kind, sender_id, message_type, text,
		COALESCE(metadata::text, '') AS metadata, created_at
		FROM conversation_messages WHERE entry_type = ? AND entry_id = ? AND deleted_at IS NULL ORDER BY created_at`, string(shared.EntryTypeEmail), conversationID).Scan(&rows).Error)
	return rows
}

func (w *world) conversationsOf(ws *tenant) int64 {
	w.t.Helper()
	var n int64
	w.must(w.db.Raw("SELECT count(*) FROM email_conversations WHERE workspace_id = ? AND deleted_at IS NULL", ws.ID).Scan(&n).Error)
	return n
}

func (w *world) conversationOf(ws *tenant, address emailaddress.Address) string {
	w.t.Helper()
	contact, err := w.inbox.Contacts.FindByAddress(w.ctx, ws.ID, address)
	w.must(err)
	conv, err := w.inbox.Conversations.FindByContact(w.ctx, ws.ID, contact.ID)
	w.must(err)
	return conv.ID
}
