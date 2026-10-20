package conversation_repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
	conversation_repository "vozko/infra/repositories/conversation"
	facebook_repository "vozko/infra/repositories/facebook"
	instagram_repository "vozko/infra/repositories/instagram"
	mercadolivre_repository "vozko/infra/repositories/mercadolivre"
	"vozko/infra/repositories/repotest"
	telegram_repository "vozko/infra/repositories/telegram"
	unofficial_whatsapp_repository "vozko/infra/repositories/unofficial_whatsapp"
	webchat_repository "vozko/infra/repositories/webchat"
	fbuc "vozko/usecases/facebook"
	iguc "vozko/usecases/instagram"
	mluc "vozko/usecases/mercadolivre"
	tguc "vozko/usecases/telegram"
	uwuc "vozko/usecases/unofficial_whatsapp"
	wcuc "vozko/usecases/webchat"
)

type parityWorld struct {
	t          *testing.T
	db         *gorm.DB
	entryType  shared.EntryType
	workspace  string
	want       map[string]bool
	names      map[string]string
	containers map[string]bool
}

func newParityWorld(t *testing.T, entryType shared.EntryType, models ...interface{}) *parityWorld {
	t.Helper()
	repotest.WithPII(t)
	models = append(models, &schema.ConversationMessage{})
	return &parityWorld{
		t: t, db: repotest.IsolatedDB(t, "window_parity", models...), entryType: entryType, workspace: uuid.NewString(),
		want: map[string]bool{}, names: map[string]string{}, containers: map[string]bool{},
	}
}

func (w *parityWorld) create(record interface{}) {
	w.t.Helper()
	if err := w.db.Create(record).Error; err != nil {
		w.t.Fatalf("seed %T: %v", record, err)
	}
}

func (w *parityWorld) expect(name, conversationID, containerID string, open bool) {
	w.want[conversationID] = open
	w.names[conversationID] = name
	w.containers[containerID] = true
	w.create(&schema.ConversationMessage{ID: uuid.NewString(), EntryID: conversationID, EntryType: string(w.entryType), CreatedAt: time.Now().UTC()})
}

func ago(d time.Duration) *time.Time {
	at := time.Now().UTC().Add(-d)
	return &at
}

func boolRef(v bool) *bool { return &v }

func (w *parityWorld) assertParity(adapter conversation.WindowStatesAdapter) {
	w.t.Helper()
	ids := make([]string, 0, len(w.want))
	for id := range w.want {
		ids = append(ids, id)
	}
	states, err := adapter.WindowStates(context.Background(), ids)
	if err != nil {
		w.t.Fatalf("adapter: %v", err)
	}
	filters := map[string]func(open bool) ([]string, error){
		"workspace inbox": func(open bool) ([]string, error) {
			return conversation_repository.WorkspaceWindowFilterIDs(w.db, w.workspace, w.entryType, open)
		},
		"account inbox": func(open bool) ([]string, error) {
			var all []string
			for container := range w.containers {
				found, err := conversation_repository.ContainerWindowFilterIDs(w.db, w.entryType, container, open)
				if err != nil {
					return nil, err
				}
				all = append(all, found...)
			}
			return all, nil
		},
	}
	for surface, filter := range filters {
		openIDs, err := filter(true)
		if err != nil {
			w.t.Fatalf("%s open filter: %v", surface, err)
		}
		closedIDs, err := filter(false)
		if err != nil {
			w.t.Fatalf("%s closed filter: %v", surface, err)
		}
		inOpen, inClosed := set(openIDs), set(closedIDs)
		for id, want := range w.want {
			state := conversation.WithUnavailable([]string{id}, states)[id]
			if state.Open != want {
				w.t.Errorf("%s: adapter open=%v (%s), case expects %v", w.names[id], state.Open, state.Reason, want)
			}
			if inOpen[id] != state.Open || inClosed[id] == state.Open {
				w.t.Errorf("%s, %s: the window filter disagrees with the adapter: adapter open=%v (%s), open filter=%v, closed filter=%v",
					surface, w.names[id], state.Open, state.Reason, inOpen[id], inClosed[id])
			}
		}
	}
}

func set(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestWindowFilterAgreesWithTheInstagramAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeInstagram, &schema.InstagramAccount{}, &schema.InstagramContact{}, &schema.InstagramConversation{})
	scopes := "instagram_business_basic,instagram_business_manage_messages"
	accounts := map[string]*schema.InstagramAccount{
		"ok":       {Status: "CONNECTED", GrantedScopes: scopes, AccessToken: piigorm.NewEncrypted("token")},
		"expired":  {Status: "TOKEN_EXPIRED", GrantedScopes: scopes, AccessToken: piigorm.NewEncrypted("token")},
		"no scope": {Status: "CONNECTED", GrantedScopes: "instagram_business_basic", AccessToken: piigorm.NewEncrypted("token")},
		"no token": {Status: "CONNECTED", GrantedScopes: scopes},
	}
	for _, account := range accounts {
		account.WorkspaceID, account.IGUserID = w.workspace, uuid.NewString()[:16]
		w.create(account)
	}
	conv := func(name, account string, inbound *time.Time, open bool) {
		contact := &schema.InstagramContact{WorkspaceID: w.workspace, IGAccountID: accounts[account].ID, IGSID: uuid.NewString()[:16]}
		w.create(contact)
		c := &schema.InstagramConversation{WorkspaceID: w.workspace, IGAccountID: accounts[account].ID, ContactID: contact.ID,
			LastMessageAt: ago(time.Minute), LastCustomerMessageAt: inbound}
		w.create(c)
		w.expect(name, c.ID, c.IGAccountID, open)
	}
	conv("open", "ok", ago(time.Hour), true)
	conv("expired window", "ok", ago(25*time.Hour), false)
	conv("never wrote", "ok", nil, false)
	conv("account disconnected", "expired", ago(time.Hour), false)
	conv("messaging scope missing", "no scope", ago(time.Hour), false)
	conv("token missing", "no token", ago(time.Hour), false)

	adapter := iguc.NewChannelAdapter(instagram_repository.NewAccountRepository(w.db), instagram_repository.NewContactRepository(w.db),
		instagram_repository.NewConversationRepository(w.db), nil)
	w.assertParity(adapter.(conversation.WindowStatesAdapter))
}

func TestWindowFilterAgreesWithTheFacebookAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeFacebook, &schema.FacebookPage{}, &schema.FacebookContact{}, &schema.FacebookConversation{})
	scopes := "pages_messaging,pages_manage_metadata"
	pages := map[string]*schema.FacebookPage{
		"ok":          {Status: "CONNECTED", GrantedScopes: scopes, Tasks: "MESSAGING", PageToken: piigorm.NewEncrypted("token")},
		"manager":     {Status: "CONNECTED", GrantedScopes: scopes, Tasks: "MODERATE,MANAGE", PageToken: piigorm.NewEncrypted("token")},
		"revoked":     {Status: "TOKEN_REVOKED", GrantedScopes: scopes, Tasks: "MESSAGING", PageToken: piigorm.NewEncrypted("token")},
		"moderator":   {Status: "CONNECTED", GrantedScopes: scopes, Tasks: "MODERATE", PageToken: piigorm.NewEncrypted("token")},
		"no metadata": {Status: "CONNECTED", GrantedScopes: "pages_messaging", Tasks: "MESSAGING", PageToken: piigorm.NewEncrypted("token")},
		"no token":    {Status: "CONNECTED", GrantedScopes: scopes, Tasks: "MESSAGING"},
	}
	for _, page := range pages {
		page.WorkspaceID, page.GrantID, page.FBPageID = w.workspace, uuid.NewString(), uuid.NewString()[:16]
		w.create(page)
	}
	conv := func(name, page string, inbound *time.Time, open bool) {
		contact := &schema.FacebookContact{WorkspaceID: w.workspace, PageID: pages[page].ID, PSID: uuid.NewString()[:16]}
		w.create(contact)
		c := &schema.FacebookConversation{WorkspaceID: w.workspace, PageID: pages[page].ID, ContactID: contact.ID,
			LastMessageAt: ago(time.Minute), LastCustomerMessageAt: inbound}
		w.create(c)
		w.expect(name, c.ID, c.PageID, open)
	}
	conv("open", "ok", ago(time.Hour), true)
	conv("open through the manage task", "manager", ago(time.Hour), true)
	conv("expired window", "ok", ago(25*time.Hour), false)
	conv("page revoked", "revoked", ago(time.Hour), false)
	conv("no messaging task", "moderator", ago(time.Hour), false)
	conv("scope missing", "no metadata", ago(time.Hour), false)
	conv("token missing", "no token", ago(time.Hour), false)

	adapter := fbuc.NewChannelAdapter(fbuc.ChannelAdapterDeps{
		Pages: facebook_repository.NewPageRepository(w.db), Contacts: facebook_repository.NewContactRepository(w.db),
		Conversations: facebook_repository.NewConversationRepository(w.db),
	})
	w.assertParity(adapter)
}

func TestWindowFilterAgreesWithTheTelegramAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeTelegram, &schema.TelegramAccount{}, &schema.TelegramContact{}, &schema.TelegramConversation{})
	rights := func(canReply bool) datatypes.JSON {
		if canReply {
			return datatypes.JSON(`{"can_reply":true,"can_read_messages":true}`)
		}
		return datatypes.JSON(`{"can_read_messages":true}`)
	}
	accounts := map[string]*schema.TelegramAccount{
		"bot":              {Mode: "BOT", Status: "ACTIVE", BotToken: piigorm.NewEncrypted("1:token")},
		"bot failing hook": {Mode: "BOT", Status: "WEBHOOK_FAILING", BotToken: piigorm.NewEncrypted("1:token")},
		"bot revoked":      {Mode: "BOT", Status: "REVOKED", BotToken: piigorm.NewEncrypted("1:token")},
		"bot no token":     {Mode: "BOT", Status: "ACTIVE"},
		"business":         {Mode: "BUSINESS", Status: "ACTIVE", BotToken: piigorm.NewEncrypted("1:token"), BusinessEnabled: true, BusinessRights: rights(true)},
		"business mute":    {Mode: "BUSINESS", Status: "ACTIVE", BotToken: piigorm.NewEncrypted("1:token"), BusinessEnabled: true, BusinessRights: rights(false)},
		"business off":     {Mode: "BUSINESS", Status: "ACTIVE", BotToken: piigorm.NewEncrypted("1:token"), BusinessRights: rights(true)},
	}
	for _, account := range accounts {
		account.WorkspaceID, account.BotUserID = w.workspace, time.Now().UnixNano()
		w.create(account)
	}
	conv := func(name, account string, blocked bool, inbound *time.Time, open bool) {
		contact := &schema.TelegramContact{WorkspaceID: w.workspace, AccountID: accounts[account].ID, TGUserID: time.Now().UnixNano(), TGChatID: time.Now().UnixNano()}
		w.create(contact)
		if blocked {
			if err := w.db.Model(contact).Update("blocked", true).Error; err != nil {
				t.Fatal(err)
			}
		}
		c := &schema.TelegramConversation{WorkspaceID: w.workspace, AccountID: accounts[account].ID, ContactID: contact.ID, TGChatID: contact.TGChatID,
			LastMessageAt: ago(time.Minute), LastCustomerMessageAt: inbound}
		w.create(c)
		w.expect(name, c.ID, c.AccountID, open)
	}
	conv("bot without a clock", "bot", false, ago(10*24*time.Hour), true)
	conv("bot with a failing webhook", "bot failing hook", false, ago(time.Hour), true)
	conv("bot contact blocked", "bot", true, ago(time.Hour), false)
	conv("bot revoked", "bot revoked", false, ago(time.Hour), false)
	conv("bot token missing", "bot no token", false, ago(time.Hour), false)
	conv("business open", "business", false, ago(time.Hour), true)
	conv("business expired", "business", false, ago(25*time.Hour), false)
	conv("business never wrote", "business", false, nil, false)
	conv("business reply revoked", "business mute", false, ago(time.Hour), false)
	conv("business disabled", "business off", false, ago(time.Hour), false)

	adapter := tguc.NewChannelAdapter(telegram_repository.NewAccountRepository(w.db), telegram_repository.NewContactRepository(w.db),
		telegram_repository.NewConversationRepository(w.db), nil, nil)
	w.assertParity(adapter.(conversation.WindowStatesAdapter))
}

func TestWindowFilterAgreesWithTheWebchatAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeWebchat, &schema.WebchatWidget{}, &schema.WebchatVisitor{}, &schema.WebchatConversation{})
	widget := func() *schema.WebchatWidget {
		record := &schema.WebchatWidget{WorkspaceID: w.workspace, Name: "Site", PublicKey: uuid.NewString()}
		w.create(record)
		return record
	}
	live, gone := widget(), widget()
	conv := func(name string, owner *schema.WebchatWidget, blocked bool, open bool) {
		visitor := &schema.WebchatVisitor{WorkspaceID: w.workspace, WidgetID: owner.ID}
		w.create(visitor)
		if blocked {
			if err := w.db.Model(visitor).Update("blocked", true).Error; err != nil {
				t.Fatal(err)
			}
		}
		c := &schema.WebchatConversation{WorkspaceID: w.workspace, WidgetID: owner.ID, VisitorID: visitor.ID, LastMessageAt: ago(time.Minute)}
		w.create(c)
		w.expect(name, c.ID, c.WidgetID, open)
	}
	conv("open", live, false, true)
	conv("visitor blocked", live, true, false)
	conv("widget deleted", gone, false, false)
	if err := w.db.Delete(gone).Error; err != nil {
		t.Fatal(err)
	}

	adapter := wcuc.NewChannelAdapter(webchat_repository.NewWidgetRepository(w.db), webchat_repository.NewVisitorRepository(w.db),
		webchat_repository.NewConversationRepository(w.db), nil, nil)
	w.assertParity(adapter.(conversation.WindowStatesAdapter))
}

func TestWindowFilterAgreesWithTheUnofficialWhatsAppAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeUnofficialWhatsApp, &schema.UnofficialWhatsAppInstance{}, &schema.UnofficialWhatsAppContact{}, &schema.UnofficialWhatsAppConversation{}, &schema.UnofficialWhatsAppCampaign{})
	if err := w.db.Exec(`CREATE TABLE leads (id uuid PRIMARY KEY, name text, deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	instance := func(status string, until *time.Time, canSendNew *bool) *schema.UnofficialWhatsAppInstance {
		record := &schema.UnofficialWhatsAppInstance{WorkspaceID: w.workspace, ServerID: uuid.NewString(), ProviderInstanceID: uuid.NewString()[:16],
			DeliveryTokenHash: uuid.NewString(), Status: status, RestrictionUntil: until, RestrictionCanSendNew: canSendNew}
		w.create(record)
		return record
	}
	ok := instance("CONNECTED", nil, nil)
	conv := func(name string, owner *schema.UnofficialWhatsAppInstance, blocked bool, open bool) {
		contact := &schema.UnofficialWhatsAppContact{WorkspaceID: w.workspace, InstanceID: owner.ID, JID: uuid.NewString()[:20] + "@s.whatsapp.net"}
		w.create(contact)
		if blocked {
			if err := w.db.Model(contact).Update("blocked", true).Error; err != nil {
				t.Fatal(err)
			}
		}
		c := &schema.UnofficialWhatsAppConversation{WorkspaceID: w.workspace, InstanceID: owner.ID, ContactID: contact.ID, ChatID: contact.JID, LastMessageAt: ago(time.Minute)}
		w.create(c)
		w.expect(name, c.ID, c.InstanceID, open)
	}
	conv("open", ok, false, true)
	conv("contact blocked", ok, true, false)
	conv("session down", instance("DISCONNECTED", nil, nil), false, false)
	conv("restricted for an hour", instance("CONNECTED", ago(-time.Hour), nil), false, false)
	conv("restriction lapsed", instance("CONNECTED", ago(time.Hour), nil), false, true)
	conv("new chats refused", instance("CONNECTED", nil, boolRef(false)), false, false)
	conv("new chats allowed", instance("CONNECTED", nil, boolRef(true)), false, true)

	adapter := uwuc.NewChannelAdapter(unofficial_whatsapp_repository.NewInstanceRepository(w.db), nil,
		unofficial_whatsapp_repository.NewContactRepository(w.db), unofficial_whatsapp_repository.NewConversationRepository(w.db), nil)
	w.assertParity(adapter.(conversation.WindowStatesAdapter))
}

type mlMessages struct{ mldomain.MessageService }
type mlAttachments struct{ mldomain.AttachmentService }
type mlErrors struct{ mldomain.ErrorClassifier }
type mlMedia struct{ mldomain.MediaFetcher }
type mlMetrics struct{ mluc.Metrics }

func TestWindowFilterAgreesWithTheMercadoLivreAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeMercadoLivre, &schema.MercadoLivreAccount{}, &schema.MercadoLivreContact{}, &schema.MercadoLivreConversation{})
	account := func(status string, canSend bool) *schema.MercadoLivreAccount {
		record := &schema.MercadoLivreAccount{WorkspaceID: w.workspace, MLUserID: uuid.NewString()[:12], SiteID: "MLB", Status: status,
			AccessToken: piigorm.NewEncrypted("token"), CanReadMessages: true, CanSendMessages: canSend}
		w.create(record)
		return record
	}
	ok := account("CONNECTED", true)
	conv := func(name string, owner *schema.MercadoLivreAccount, windowOpen bool, budget, spent int, open bool) {
		contact := &schema.MercadoLivreContact{WorkspaceID: w.workspace, AccountID: owner.ID, MLUserID: uuid.NewString()[:12]}
		w.create(contact)
		c := &schema.MercadoLivreConversation{WorkspaceID: w.workspace, AccountID: owner.ID, ContactID: contact.ID, PackKey: uuid.NewString()[:16],
			MLStatus: "active", AgentMediated: boolRef(budget > 0), WindowOpen: windowOpen, OutboundBudget: budget, OutboundSinceInbound: spent,
			LastMessageAt: ago(time.Minute), LastCustomerMessageAt: ago(time.Hour)}
		w.create(c)
		w.expect(name, c.ID, c.AccountID, open)
	}
	conv("open", ok, true, 0, 3, true)
	conv("mediated with budget left", ok, true, 2, 1, true)
	conv("budget exhausted", ok, true, 1, 1, false)
	conv("thread closed by the provider", ok, false, 0, 0, false)
	conv("account needs reconnect", account("NEEDS_RECONNECT", true), true, 0, 0, false)
	conv("send permission lost", account("CONNECTED", false), true, 0, 0, false)

	adapter := mluc.NewChannelAdapter(mluc.ChannelAdapterDeps{
		Accounts: mercadolivre_repository.NewAccountRepository(w.db), Contacts: mercadolivre_repository.NewContactRepository(w.db),
		Conversations: mercadolivre_repository.NewConversationRepository(w.db),
		Services:      mluc.Services{Messages: mlMessages{}, Attachments: mlAttachments{}, Errors: mlErrors{}},
		Tokens:        &mluc.TokenSource{}, Threads: &mluc.ThreadSync{}, Refresh: &mluc.JobPublisher{}, Media: mlMedia{}, Metrics: mlMetrics{},
	})
	w.assertParity(adapter)
}

func TestWindowFilterFollowsTheOfficialLeadWindow(t *testing.T) {
	db := repotest.IsolatedDB(t, "window_parity_official")
	for _, ddl := range []string{
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, business_phone_id uuid, department_id uuid, type text, name text)`,
		`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid NOT NULL, lead_id uuid NOT NULL, conversation_status text,
			last_message_at timestamptz, awaiting_reply_since timestamptz, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE lead_message_windows (id uuid PRIMARY KEY, lead_id uuid NOT NULL, business_phone_id uuid NOT NULL, last_message_at timestamptz NOT NULL)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	workspace, phone, otherPhone := uuid.NewString(), uuid.NewString(), uuid.NewString()
	campaign := func(businessPhone *string) string {
		id := uuid.NewString()
		if err := db.Exec(`INSERT INTO whatsapp_campaigns (id, workspace_id, business_phone_id) VALUES (?, ?, ?)`, id, workspace, businessPhone).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	withPhone, withoutPhone := campaign(&phone), campaign(nil)
	want := map[string]bool{}
	names := map[string]string{}
	entry := func(name, campaignID string, windowPhone string, wroteAgo *time.Duration, open bool) {
		id, lead := uuid.NewString(), uuid.NewString()
		if err := db.Exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, last_message_at) VALUES (?, ?, ?, NOW())`, id, campaignID, lead).Error; err != nil {
			t.Fatal(err)
		}
		if wroteAgo != nil {
			if err := db.Exec(`INSERT INTO lead_message_windows (id, lead_id, business_phone_id, last_message_at) VALUES (?, ?, ?, ?)`,
				uuid.NewString(), lead, windowPhone, time.Now().UTC().Add(-*wroteAgo)).Error; err != nil {
				t.Fatal(err)
			}
		}
		want[id], names[id] = open, name
	}
	hour, day := time.Hour, 25*time.Hour
	entry("open on the campaign number", withPhone, phone, &hour, true)
	entry("expired on the campaign number", withPhone, phone, &day, false)
	entry("never wrote", withPhone, phone, nil, false)
	entry("wrote to another number", withPhone, otherPhone, &hour, false)
	entry("campaign without a number, open on any", withoutPhone, otherPhone, &hour, true)

	openIDs, err := conversation_repository.WorkspaceWindowFilterIDs(db, workspace, shared.EntryTypeWhatsApp, true)
	if err != nil {
		t.Fatal(err)
	}
	closedIDs, err := conversation_repository.WorkspaceWindowFilterIDs(db, workspace, shared.EntryTypeWhatsApp, false)
	if err != nil {
		t.Fatal(err)
	}
	inOpen, inClosed := set(openIDs), set(closedIDs)
	for id, open := range want {
		if inOpen[id] != open || inClosed[id] == open {
			t.Errorf("%s: open filter=%v, closed filter=%v, the lead window says open=%v", names[id], inOpen[id], inClosed[id], open)
		}
	}
}
