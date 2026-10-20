package lead

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func migrateChannelLineTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.ConversationMessage{}, &schema.WhatsAppCampaign{}, &schema.WhatsAppBusinessPhoneNumber{},
		&schema.UnofficialWhatsAppInstance{}, &schema.UnofficialWhatsAppCampaign{}, &schema.InstagramAccount{}, &schema.FacebookPage{},
		&schema.TelegramAccount{}, &schema.WebchatWidget{}); err != nil {
		t.Fatalf("migrate channel lines: %v", err)
	}
}

func TestEveryConversationOfTheLeadNamesTheLineItRunsOnAgainstPostgres(t *testing.T) {
	db, repo := collectionsDB(t)
	migrateChannelLineTables(t, db)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.AutoMigrate(&schema.UnofficialWhatsAppCampaignEntry{}, &schema.LeadMemory{}))
	ws := uuid.NewString()
	maria := newLead(t, repo, ws, lead.Draft{Number: "5511987654321", Name: "Maria Souza"}).ID
	started := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	want := map[string]string{}
	opened := func(entryID string, entryType shared.EntryType, line string) {
		started = started.Add(time.Minute)
		must(db.Create(&schema.ConversationMessage{ID: uuid.NewString(), EntryID: entryID, EntryType: string(entryType), MessageType: "text", CreatedAt: started}).Error)
		want["conversation:"+string(entryType)+":"+entryID] = line
	}
	phone := func(display, verified string) string {
		id := uuid.NewString()
		must(db.Create(&schema.WhatsAppBusinessPhoneNumber{ID: id, MetaPhoneNumberID: id, WABAId: "waba-1", OwnerWorkspaceID: ws,
			OwnerAssignedBy: uuid.NewString(), DisplayPhoneNumber: display, VerifiedName: verified}).Error)
		return id
	}

	campaignPhone, receivingPhone, unnamedPhone := phone("+55 11 4000-1000", "Escola Aurora"), phone("+55 11 4000-2000", "Escola Aurora"), phone("", "Escola Aurora")
	campaign, receptive, unnamedCampaign := uuid.NewString(), uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.WhatsAppCampaign{ID: campaign, WorkspaceID: ws, Name: "Volta às aulas", BusinessPhoneID: &campaignPhone}).Error)
	must(db.Create(&schema.WhatsAppCampaign{ID: receptive, WorkspaceID: ws, Name: "Receptivo", BusinessPhoneID: &campaignPhone}).Error)
	must(db.Create(&schema.WhatsAppCampaign{ID: unnamedCampaign, WorkspaceID: ws, Name: "Rematrícula", BusinessPhoneID: &unnamedPhone}).Error)
	sent, received, unnamed := uuid.NewString(), uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: sent, CampaignID: campaign, LeadID: maria, Status: "READ"}).Error)
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: received, CampaignID: receptive, LeadID: maria, Status: "READ", ReceivedBusinessPhoneID: &receivingPhone}).Error)
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: unnamed, CampaignID: unnamedCampaign, LeadID: maria, Status: "READ"}).Error)
	opened(sent, shared.EntryTypeWhatsApp, "+55 11 4000-1000")
	opened(received, shared.EntryTypeWhatsApp, "+55 11 4000-2000")
	opened(unnamed, shared.EntryTypeWhatsApp, "Escola Aurora")

	instance := func(display, number string) string {
		id := uuid.NewString()
		must(db.Create(&schema.UnofficialWhatsAppInstance{ID: id, WorkspaceID: ws, ServerID: uuid.NewString(), ProviderInstanceID: id,
			DeliveryTokenHash: id, DisplayName: display, PhoneNumber: number}).Error)
		return id
	}
	unofficialChat := func(instanceID, line string) {
		contact, conversation := uuid.NewString(), uuid.NewString()
		must(db.Create(&schema.UnofficialWhatsAppContact{ID: contact, WorkspaceID: ws, InstanceID: instanceID, JID: contact + "@s.whatsapp.net", LeadID: &maria}).Error)
		must(db.Create(&schema.UnofficialWhatsAppConversation{ID: conversation, WorkspaceID: ws, InstanceID: instanceID, ContactID: contact, ChatID: contact + "@s.whatsapp.net"}).Error)
		opened(conversation, shared.EntryTypeUnofficialWhatsApp, line)
	}
	unofficialChat(instance("Comercial", "5511955550000"), "Comercial")
	unofficialChat(instance("", "5511955551111"), "5511955551111")

	igAccount, igContact, igConversation := uuid.NewString(), uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.InstagramAccount{ID: igAccount, WorkspaceID: ws, IGUserID: igAccount, Username: "escola.aurora"}).Error)
	must(db.Create(&schema.InstagramContact{ID: igContact, WorkspaceID: ws, IGAccountID: igAccount, IGSID: igContact, LeadID: &maria}).Error)
	must(db.Create(&schema.InstagramConversation{ID: igConversation, WorkspaceID: ws, IGAccountID: igAccount, ContactID: igContact}).Error)
	opened(igConversation, shared.EntryTypeInstagram, "escola.aurora")

	page, fbContact, fbConversation := uuid.NewString(), uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.FacebookPage{ID: page, WorkspaceID: ws, GrantID: uuid.NewString(), FBPageID: page, Name: "Escola Aurora Oficial"}).Error)
	must(db.Create(&schema.FacebookContact{ID: fbContact, WorkspaceID: ws, PageID: page, PSID: fbContact, LeadID: &maria}).Error)
	must(db.Create(&schema.FacebookConversation{ID: fbConversation, WorkspaceID: ws, PageID: page, ContactID: fbContact}).Error)
	opened(fbConversation, shared.EntryTypeFacebook, "Escola Aurora Oficial")

	bot, tgContact, tgConversation := uuid.NewString(), uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.TelegramAccount{ID: bot, WorkspaceID: ws, BotUserID: 42, BotUsername: "aurora_bot"}).Error)
	must(db.Create(&schema.TelegramContact{ID: tgContact, WorkspaceID: ws, AccountID: bot, TGUserID: 7, TGChatID: 7, LeadID: &maria}).Error)
	must(db.Create(&schema.TelegramConversation{ID: tgConversation, WorkspaceID: ws, AccountID: bot, ContactID: tgContact, TGChatID: 7}).Error)
	opened(tgConversation, shared.EntryTypeTelegram, "aurora_bot")

	widget, visitor, chat := uuid.NewString(), uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.WebchatWidget{ID: widget, WorkspaceID: ws, Name: "Site da escola", PublicKey: widget}).Error)
	must(db.Create(&schema.WebchatVisitor{ID: visitor, WorkspaceID: ws, WidgetID: widget, LeadID: &maria}).Error)
	must(db.Create(&schema.WebchatConversation{ID: chat, WorkspaceID: ws, WidgetID: widget, VisitorID: visitor}).Error)
	opened(chat, shared.EntryTypeWebchat, "Site da escola")

	w := timelineWorld{repo: repo, ws: ws, maria: &lead.Lead{ID: maria}}
	got := map[string]string{}
	for _, item := range w.scan(t, lead.TimelineScan{Limit: 50}) {
		if item.Kind == lead.TimelineConversation {
			got[item.ID] = item.Summary.Line
		}
	}
	for id, line := range want {
		if got[id] != line {
			t.Errorf("%s runs on %q, want %q", id[:strings.LastIndex(id, ":")], got[id], line)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%d conversations, want %d: %v", len(got), len(want), got)
	}
}
