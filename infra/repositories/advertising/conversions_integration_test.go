package advertising_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

type outboxFixture struct {
	t    *testing.T
	db   *gorm.DB
	base time.Time
}

func (f outboxFixture) create(value any) {
	f.t.Helper()
	if err := f.db.Omit(clause.Associations).Create(value).Error; err != nil {
		f.t.Fatal(err)
	}
}

func (f outboxFixture) opportunity(ws, leadID string, cents int64) string {
	o := &schema.Opportunity{WorkspaceID: ws, LeadID: leadID, PipelineID: uuid.NewString(), StageID: uuid.NewString(),
		Status: "open", Currency: "BRL", ValueCents: cents}
	f.create(o)
	return o.ID
}

func (f outboxFixture) event(ws, opportunityID, kind string, offset time.Duration) {
	f.create(&schema.OpportunityEvent{ID: uuid.NewString(), WorkspaceID: ws, OpportunityID: opportunityID, Type: kind,
		ActorKind: "human", Currency: "BRL", CreatedAt: f.base.Add(offset)})
}

func (f outboxFixture) link(opportunityID, entryID, entryType string) {
	f.create(&schema.OpportunityConversation{OpportunityID: opportunityID, EntryID: entryID, EntryType: entryType})
}

func (f outboxFixture) phone(wabaID string) string {
	p := &schema.WhatsAppBusinessPhoneNumber{MetaPhoneNumberID: uuid.NewString(), WABAId: wabaID, DisplayPhoneNumber: "+55 11 4000-0000",
		OwnerWorkspaceID: uuid.NewString(), OwnerAssignedBy: uuid.NewString()}
	f.create(p)
	return p.ID
}

func (f outboxFixture) facebookConversation(ws, fbPageID, psid string) string {
	page := &schema.FacebookPage{WorkspaceID: ws, GrantID: uuid.NewString(), FBPageID: fbPageID}
	f.create(page)
	contact := &schema.FacebookContact{WorkspaceID: ws, PageID: page.ID, PSID: psid}
	f.create(contact)
	conversation := &schema.FacebookConversation{WorkspaceID: ws, PageID: page.ID, ContactID: contact.ID}
	f.create(conversation)
	return conversation.ID
}

func TestConversionOutboxBuildsSignalsPerChannelAndHonoursRecordsAgainstPostgres(t *testing.T) {
	withPII(t)
	db := repotest.IsolatedDB(t, "ads_outbox_test")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Opportunity{}, &schema.OpportunityEvent{}, &schema.OpportunityConversation{},
		&schema.ConversationAdOrigin{}, &schema.Lead{}, &schema.WhatsAppBusinessPhoneNumber{}, &schema.WhatsAppCampaign{},
		&schema.WhatsAppCampaignEntry{}, &schema.FacebookPage{}, &schema.FacebookContact{}, &schema.FacebookConversation{},
		&schema.InstagramAccount{}, &schema.InstagramContact{}, &schema.InstagramConversation{}, &schema.AdConversionRecord{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	f := outboxFixture{t: t, db: db, base: base}
	wsA, wsB := uuid.NewString(), uuid.NewString()
	outbox := NewConversionOutbox(db)

	lead := &schema.Lead{WorkspaceID: wsA, Number: "5511999990000"}
	f.create(lead)
	campaignPhone, receivedPhone := f.phone("waba-campaign"), f.phone("waba-received")
	campaign := &schema.WhatsAppCampaign{WorkspaceID: wsA, BusinessPhoneID: &campaignPhone, Name: "Anuncio"}
	f.create(campaign)
	entry := &schema.WhatsAppCampaignEntry{CampaignID: campaign.ID, LeadID: lead.ID, ReceivedBusinessPhoneID: &receivedPhone}
	f.create(entry)
	f.create(&schema.ConversationAdOrigin{EntryID: entry.ID, EntryType: "whatsapp", AdID: "ad-1", ClickID: "clid-1", ArrivedAt: base})
	whatsapp := f.opportunity(wsA, lead.ID, 1500)
	f.link(whatsapp, entry.ID, "whatsapp")
	f.event(wsA, whatsapp, "created", 30*time.Minute)
	f.event(wsA, whatsapp, "won", 2*time.Hour)
	f.event(wsA, whatsapp, "won", 5*time.Hour)

	messenger := f.opportunity(wsA, "", 0)
	f.link(messenger, f.facebookConversation(wsA, "fbpage-1", "psid-1"), "facebook")
	f.event(wsA, messenger, "created", time.Hour)
	f.event(wsA, messenger, "won", 90*time.Minute)

	account := &schema.InstagramAccount{WorkspaceID: wsA, IGUserID: "ig-1"}
	f.create(account)
	igContact := &schema.InstagramContact{WorkspaceID: wsA, IGAccountID: account.ID, IGSID: "igsid-1"}
	f.create(igContact)
	igConversation := &schema.InstagramConversation{WorkspaceID: wsA, IGAccountID: account.ID, ContactID: igContact.ID}
	f.create(igConversation)
	instagram := f.opportunity(wsA, "", 0)
	f.link(instagram, igConversation.ID, "instagram")
	f.event(wsA, instagram, "created", 40*time.Minute)
	f.event(wsA, instagram, "won", 3*time.Hour)

	telegram := f.opportunity(wsA, "", 0)
	f.link(telegram, uuid.NewString(), "telegram")
	f.event(wsA, telegram, "created", 4*time.Hour)

	foreignLink := f.opportunity(wsA, "", 0)
	f.link(foreignLink, f.facebookConversation(wsB, "fbpage-b", "psid-b"), "facebook")
	f.event(wsA, foreignLink, "created", 6*time.Hour)

	deleted := f.opportunity(wsA, "", 0)
	f.event(wsA, deleted, "created", time.Hour)
	if err := db.Delete(&schema.Opportunity{}, "id = ?", deleted).Error; err != nil {
		t.Fatal(err)
	}
	old := f.opportunity(wsA, "", 0)
	f.event(wsA, old, "created", -time.Hour)
	other := f.opportunity(wsB, "", 0)
	f.event(wsB, other, "created", time.Hour)

	record := func(ws, opportunityID, name string, status advertising.ConversionStatus) {
		t.Helper()
		if err := outbox.Record(ctx, advertising.ConversionRecord{OpportunityID: opportunityID, EventName: name, WorkspaceID: ws, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	record(wsA, whatsapp, advertising.EventNameLead, advertising.ConversionSent)
	record(wsA, messenger, advertising.EventNamePurchase, advertising.ConversionSkipped)
	record(wsA, messenger, advertising.EventNameLead, advertising.ConversionFailed)
	record(wsA, messenger, advertising.EventNameLead, advertising.ConversionFailed)
	for range 5 {
		record(wsA, instagram, advertising.EventNameLead, advertising.ConversionFailed)
	}
	err := outbox.Record(ctx, advertising.ConversionRecord{OpportunityID: whatsapp, EventName: advertising.EventNameLead, WorkspaceID: wsB, Status: advertising.ConversionFailed})
	if !errors.Is(err, errConversionRecordElsewhere) {
		t.Fatalf("record of another workspace: %v", err)
	}

	pending, err := outbox.Pending(ctx, advertising.PendingQuery{WorkspaceID: wsA, Events: []advertising.DealEvent{advertising.DealCreated, advertising.DealWon}, Since: base, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	signal := func(id string, event advertising.DealEvent, offset time.Duration, cents int64, identity advertising.MessagingIdentity, phone string) advertising.PendingSignal {
		return advertising.PendingSignal{WorkspaceID: wsA, Signal: advertising.DealSignal{
			OpportunityID: id, Event: event, At: base.Add(offset), ValueCents: cents, Currency: "BRL", Identity: identity, Phone: phone,
		}}
	}
	want := []advertising.PendingSignal{
		signal(messenger, advertising.DealCreated, time.Hour, 0,
			advertising.MessagingIdentity{Channel: advertising.ChannelMessenger, PageID: "fbpage-1", PageScopedUserID: "psid-1"}, ""),
		signal(whatsapp, advertising.DealWon, 2*time.Hour, 1500,
			advertising.MessagingIdentity{Channel: advertising.ChannelWhatsApp, ClickID: "clid-1", WABAID: "waba-received"}, "5511999990000"),
		signal(instagram, advertising.DealWon, 3*time.Hour, 0,
			advertising.MessagingIdentity{Channel: advertising.ChannelInstagram, InstagramUserID: "ig-1", InstagramScoped: "igsid-1"}, ""),
		signal(telegram, advertising.DealCreated, 4*time.Hour, 0, advertising.MessagingIdentity{}, ""),
		signal(foreignLink, advertising.DealCreated, 6*time.Hour, 0, advertising.MessagingIdentity{}, ""),
	}
	if len(pending) != len(want) {
		t.Fatalf("got %d signals: %+v", len(pending), pending)
	}
	for i := range want {
		got := pending[i]
		got.Signal.At = got.Signal.At.UTC()
		if got != want[i] {
			t.Errorf("signal %d:\n got %+v\nwant %+v", i, got, want[i])
		}
	}
	if !want[1].Signal.Identity.Complete() || !want[0].Signal.Identity.Complete() || !want[2].Signal.Identity.Complete() {
		t.Fatal("resolved identities must be complete")
	}

	firstTwo, err := outbox.Pending(ctx, advertising.PendingQuery{WorkspaceID: wsA, Events: []advertising.DealEvent{advertising.DealCreated, advertising.DealWon}, Since: base, Limit: 2})
	if err != nil || len(firstTwo) != 2 || firstTwo[1].Signal.OpportunityID != whatsapp {
		t.Fatalf("limit: %+v, %v", firstTwo, err)
	}

	recent, err := outbox.Recent(ctx, wsA, 10)
	if err != nil || len(recent) != 4 {
		t.Fatalf("recent: %+v, %v", recent, err)
	}
	attempts := map[string]int{}
	for _, r := range recent {
		attempts[r.OpportunityID+":"+r.EventName] = r.Attempts
	}
	if attempts[instagram+":"+advertising.EventNameLead] != 5 || attempts[messenger+":"+advertising.EventNameLead] != 2 ||
		attempts[whatsapp+":"+advertising.EventNameLead] != 0 {
		t.Fatalf("attempts: %v", attempts)
	}
	if none, err := outbox.Recent(ctx, wsB, 10); err != nil || len(none) != 0 {
		t.Fatalf("records leaked across workspaces: %v, %v", none, err)
	}

	pendingOf := func() map[string]bool {
		t.Helper()
		signals, err := outbox.Pending(ctx, advertising.PendingQuery{WorkspaceID: wsA, Events: []advertising.DealEvent{advertising.DealCreated, advertising.DealWon}, Since: base, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, s := range signals {
			ids[s.Signal.OpportunityID] = true
		}
		return ids
	}
	record(wsA, telegram, advertising.EventNameLead, advertising.ConversionSending)
	if ids := pendingOf(); ids[telegram] || len(ids) != 4 {
		t.Fatalf("an event being sent is still pending: %v", ids)
	}
	record(wsA, telegram, advertising.EventNameLead, advertising.ConversionFailed)
	if ids := pendingOf(); !ids[telegram] {
		t.Fatalf("a failed send must be retried: %v", ids)
	}
	record(wsA, telegram, advertising.EventNameLead, advertising.ConversionSending)
	record(wsA, telegram, advertising.EventNameLead, advertising.ConversionSent)
	if ids := pendingOf(); ids[telegram] {
		t.Fatalf("a sent event is still pending: %v", ids)
	}
	recent, err = outbox.Recent(ctx, wsA, 1)
	if err != nil || len(recent) != 1 || recent[0].OpportunityID != telegram || recent[0].Status != advertising.ConversionSent || recent[0].Attempts != 1 {
		t.Fatalf("telegram record: %+v, %v", recent, err)
	}
}

func TestAudiencesFormsLeadsAndSettingsRoundTripAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_store_test", &schema.AdAccount{}, &schema.AdSavedAudience{}, &schema.AdLeadForm{},
		&schema.AdFormLead{}, &schema.AdConversionSettings{})
	ctx := context.Background()
	wsA, wsB := uuid.NewString(), uuid.NewString()

	accounts := NewAccountRepository(db)
	account := connectedAccount()
	account.WorkspaceID, account.GrantID = wsA, uuid.NewString()
	if err := accounts.Upsert(ctx, account); err != nil {
		t.Fatal(err)
	}
	found, err := accounts.FindByMetaAccountID(ctx, "act_123")
	if err != nil || found.BusinessID != "bm-1" || found.WorkspaceID != wsA {
		t.Fatalf("account: %+v, %v", found, err)
	}
	if err := accounts.SetConnection(ctx, account.ID, advertising.ConnectionDisconnected); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FindByMetaAccountID(ctx, "123"); !errors.Is(err, advertising.ErrAccountNotFound) {
		t.Fatalf("disconnected account found: %v", err)
	}

	audiences := NewSavedAudienceRepository(db)
	saved := savedAudience()
	saved.WorkspaceID, saved.CreatedBy = wsA, uuid.NewString()
	saved.Placements = advertising.Placements{Platforms: []string{"instagram"}, Positions: map[string][]string{"instagram": {"stream"}}}
	if err := audiences.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	saved.Name = "Renomeada"
	if err := audiences.Update(ctx, saved); err != nil {
		t.Fatal(err)
	}
	back, err := audiences.Find(ctx, wsA, saved.ID)
	if err != nil || back.Name != "Renomeada" || back.Targeting.AgeMax != 40 || back.Placements.Positions["instagram"][0] != "stream" {
		t.Fatalf("audience: %+v, %v", back, err)
	}
	if _, err := audiences.Find(ctx, wsB, saved.ID); !errors.Is(err, advertising.ErrSavedAudienceNotFound) {
		t.Fatalf("audience leaked: %v", err)
	}
	if err := audiences.Delete(ctx, wsB, saved.ID); !errors.Is(err, advertising.ErrSavedAudienceNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if err := audiences.Delete(ctx, wsA, saved.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := audiences.List(ctx, wsA); err != nil || len(list) != 0 {
		t.Fatalf("after delete: %v, %v", list, err)
	}

	forms := NewLeadFormRepository(db)
	for _, id := range []string{"f-1", "f-2"} {
		if err := forms.Track(ctx, &advertising.TrackedForm{MetaID: id, WorkspaceID: wsA, AdAccountID: account.ID, PageID: "p-1", Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := forms.Track(ctx, &advertising.TrackedForm{MetaID: "f-1", WorkspaceID: wsB, AdAccountID: uuid.NewString(), PageID: "p-9"}); !errors.Is(err, errFormTrackedElsewhere) {
		t.Fatalf("stolen form: %v", err)
	}
	if err := forms.MarkPolled(ctx, "f-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	all, err := forms.ListAll(ctx, 10, 0)
	if err != nil || len(all) != 2 || all[0].MetaID != "f-2" || all[1].LastPolledAt == nil {
		t.Fatalf("list all: %+v, %v", all, err)
	}
	if f, err := forms.FindByMetaID(ctx, "f-1"); err != nil || f.WorkspaceID != wsA || f.PageID != "p-1" {
		t.Fatalf("form: %+v, %v", f, err)
	}

	leads := NewFormLeadRepository(db)
	first := formLead()
	first.WorkspaceID = wsA
	second := formLead()
	second.MetaID, second.WorkspaceID, second.FormMetaID, second.CreatedTime = "l-2", wsA, "f-2", first.CreatedTime.Add(time.Hour)
	for _, l := range []*advertising.FormLead{first, second} {
		if inserted, err := leads.Save(ctx, l); err != nil || !inserted {
			t.Fatalf("save %s: %v, %v", l.MetaID, inserted, err)
		}
	}
	if inserted, err := leads.Save(ctx, first); err != nil || inserted {
		t.Fatalf("duplicate: %v, %v", inserted, err)
	}
	leadID := uuid.NewString()
	if err := leads.LinkLead(ctx, "l-1", leadID); err != nil {
		t.Fatal(err)
	}
	page, total, err := leads.List(ctx, advertising.FormLeadQuery{WorkspaceID: wsA, Limit: 10})
	if err != nil || total != 2 || len(page) != 2 || page[0].MetaID != "l-2" || page[1].LeadID != leadID || page[1].Answers["email"] != "a@b.com" {
		t.Fatalf("leads: %+v, %d, %v", page, total, err)
	}
	if page, total, err := leads.List(ctx, advertising.FormLeadQuery{WorkspaceID: wsA, FormMetaID: "f-1", Limit: 10}); err != nil || total != 1 || len(page) != 1 {
		t.Fatalf("by form: %+v, %d, %v", page, total, err)
	}
	if _, total, err := leads.List(ctx, advertising.FormLeadQuery{WorkspaceID: wsB, Limit: 10}); err != nil || total != 0 {
		t.Fatalf("leads leaked: %d, %v", total, err)
	}

	settings := NewConversionSettingsRepository(db)
	if _, err := settings.Get(ctx, wsA); !errors.Is(err, advertising.ErrSettingsNotFound) {
		t.Fatalf("absent settings: %v", err)
	}
	s := &advertising.ConversionSettings{WorkspaceID: wsA, AdAccountID: account.ID, DatasetID: "ds-1", SendLeads: true, Enabled: true}
	if err := settings.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.PixelID, s.SendPurchases = "px-1", true
	if err := settings.Save(ctx, s); err != nil || s.UpdatedAt.IsZero() {
		t.Fatalf("resave: %v", err)
	}
	if err := settings.Save(ctx, &advertising.ConversionSettings{WorkspaceID: wsB, AdAccountID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Get(ctx, wsA)
	if err != nil || got.PixelID != "px-1" || !got.SendPurchases || !got.Enabled || got.DatasetID != "ds-1" {
		t.Fatalf("settings: %+v, %v", got, err)
	}
	enabled, err := settings.ListEnabled(ctx)
	if err != nil || len(enabled) != 1 || enabled[0].WorkspaceID != wsA {
		t.Fatalf("enabled: %+v, %v", enabled, err)
	}
}
