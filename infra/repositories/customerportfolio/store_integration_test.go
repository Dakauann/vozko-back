package customerportfolio_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var ctx = context.Background()

type world struct {
	t     *testing.T
	db    *gorm.DB
	store *Store
	ws    string
	other string
	at    time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := repotest.IsolatedDB(t, "customer_portfolio")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := database.CreateSearchFold(db); err != nil {
		t.Fatalf("search fold: %v", err)
	}
	if err := db.AutoMigrate(
		&schema.Lead{}, &schema.CustomerPortfolioEntry{}, &schema.WhatsAppCampaignEntry{},
		&schema.UnofficialWhatsAppContact{}, &schema.UnofficialWhatsAppConversation{},
		&schema.TelegramContact{}, &schema.TelegramConversation{},
		&schema.InstagramContact{}, &schema.InstagramConversation{},
		&schema.FacebookContact{}, &schema.FacebookConversation{},
		&schema.WebchatVisitor{}, &schema.WebchatConversation{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &world{t: t, db: db, store: New(db), ws: uuid.NewString(), other: uuid.NewString(), at: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (w *world) create(row interface{}) {
	w.t.Helper()
	if err := w.db.Create(row).Error; err != nil {
		w.t.Fatalf("create %T: %v", row, err)
	}
}

func (w *world) lead(workspaceID, name string) string {
	id := uuid.NewString()
	w.create(&schema.Lead{ID: id, WorkspaceID: workspaceID, Name: name, Number: schema.OptionalText("55119" + id[:8])})
	return id
}

func (w *world) officialEntry(leadID string) string {
	id := uuid.NewString()
	w.create(&schema.WhatsAppCampaignEntry{ID: id, CampaignID: uuid.NewString(), LeadID: leadID})
	return id
}

func (w *world) instagram(workspaceID, username string, leadID *string) (contactID, conversationID string) {
	contactID, conversationID, account := uuid.NewString(), uuid.NewString(), uuid.NewString()
	w.create(&schema.InstagramContact{ID: contactID, WorkspaceID: workspaceID, IGAccountID: account, IGSID: contactID[:8], Username: username, Name: "Perfil " + username, LeadID: leadID})
	w.create(&schema.InstagramConversation{ID: conversationID, WorkspaceID: workspaceID, IGAccountID: account, ContactID: contactID})
	return contactID, conversationID
}

func (w *world) telegram(workspaceID string, leadID *string) (contactID, conversationID string) {
	contactID, conversationID, account := uuid.NewString(), uuid.NewString(), uuid.NewString()
	w.create(&schema.TelegramContact{ID: contactID, WorkspaceID: workspaceID, AccountID: account, TGUserID: time.Now().UnixNano(), TGChatID: time.Now().UnixNano(), Username: "tg_user", FirstName: "Tele", LastName: "Gram", LeadID: leadID})
	w.create(&schema.TelegramConversation{ID: conversationID, WorkspaceID: workspaceID, AccountID: account, ContactID: contactID, TGChatID: time.Now().UnixNano()})
	return contactID, conversationID
}

func (w *world) put(holder string, s cp.Subject) cp.Entry {
	w.t.Helper()
	e, err := cp.New(w.ws, holder, holder, s, w.at)
	if err != nil {
		w.t.Fatalf("new entry: %v", err)
	}
	e.ID = uuid.NewString()
	stored, inserted, err := w.store.Insert(ctx, e)
	if err != nil || !inserted {
		w.t.Fatalf("insert = %v inserted=%v", err, inserted)
	}
	return stored
}

func TestStoreKeepsOneHolderPerLead(t *testing.T) {
	w := newWorld(t)
	ana, bob := uuid.NewString(), uuid.NewString()
	lead := w.lead(w.ws, "Maria")
	first := w.put(ana, cp.LeadSubject(lead))

	second, _ := cp.New(w.ws, bob, bob, cp.LeadSubject(lead), w.at)
	second.ID = uuid.NewString()
	stored, inserted, err := w.store.Insert(ctx, second)

	if err != nil || inserted {
		t.Fatalf("second insert = %v inserted=%v, want the existing row", err, inserted)
	}
	if stored.ID != first.ID || stored.HolderID != ana {
		t.Fatalf("stored = %+v, want ana's entry", stored)
	}
}

func TestStoreRefusesSubjectsOfAnotherWorkspace(t *testing.T) {
	w := newWorld(t)
	holder := uuid.NewString()
	foreignLead := w.lead(w.other, "Outra")
	foreignContact, _ := w.instagram(w.other, "outra", nil)

	for name, subject := range map[string]cp.Subject{
		"lead":    cp.LeadSubject(foreignLead),
		"contact": cp.ContactSubject(shared.EntryTypeInstagram, foreignContact),
		"unknown": cp.LeadSubject(uuid.NewString()),
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := cp.New(w.ws, holder, holder, subject, w.at)
			e.ID = uuid.NewString()
			if _, _, err := w.store.Insert(ctx, e); !errors.Is(err, cp.ErrSubjectNotFound) {
				t.Fatalf("insert = %v, want ErrSubjectNotFound", err)
			}
		})
	}
}

func TestStoreMovesFindsAndDeletesEntries(t *testing.T) {
	w := newWorld(t)
	ana, bob := uuid.NewString(), uuid.NewString()
	contact, _ := w.instagram(w.ws, "maria.ig", nil)
	entry := w.put(ana, cp.ContactSubject(shared.EntryTypeInstagram, contact))

	found, err := w.store.FindBySubject(ctx, w.ws, cp.ContactSubject(shared.EntryTypeInstagram, contact))
	if err != nil || found == nil || found.ID != entry.ID {
		t.Fatalf("find = %+v %v", found, err)
	}
	if missing, err := w.store.FindBySubject(ctx, w.other, cp.ContactSubject(shared.EntryTypeInstagram, contact)); err != nil || missing != nil {
		t.Fatalf("find in another workspace = %+v %v", missing, err)
	}

	moved, _, _ := entry.MoveTo(bob, w.at.Add(time.Hour))
	if err := w.store.SaveHolder(ctx, moved); err != nil {
		t.Fatalf("save holder = %v", err)
	}
	got, err := w.store.Get(ctx, w.ws, entry.ID)
	if err != nil || got.HolderID != bob || !got.UpdatedAt.Equal(moved.UpdatedAt) {
		t.Fatalf("get after move = %+v %v", got, err)
	}

	if err := w.store.Delete(ctx, w.other, entry.ID); !errors.Is(err, cp.ErrEntryNotFound) {
		t.Fatalf("delete from another workspace = %v", err)
	}
	if err := w.store.Delete(ctx, w.ws, entry.ID); err != nil {
		t.Fatalf("delete = %v", err)
	}
	if _, err := w.store.Get(ctx, w.ws, entry.ID); !errors.Is(err, cp.ErrEntryNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
	if err := w.store.SaveHolder(ctx, moved); !errors.Is(err, cp.ErrEntryNotFound) {
		t.Fatalf("move a deleted entry = %v", err)
	}
}

func TestHolderOfConversationMatchesTheLeadFirstThenTheContact(t *testing.T) {
	w := newWorld(t)
	ana, bob := uuid.NewString(), uuid.NewString()
	lead := w.lead(w.ws, "Maria")
	w.put(ana, cp.LeadSubject(lead))

	official := w.officialEntry(lead)
	_, telegramWithLead := w.telegram(w.ws, &lead)
	igContact, igConversation := w.instagram(w.ws, "maria.ig", nil)
	w.put(bob, cp.ContactSubject(shared.EntryTypeInstagram, igContact))
	tgContact, tgBoth := w.telegram(w.ws, &lead)
	w.put(bob, cp.ContactSubject(shared.EntryTypeTelegram, tgContact))
	_, strangerConversation := w.instagram(w.ws, "stranger", nil)

	cases := []struct {
		name      string
		entryID   string
		entryType shared.EntryType
		workspace string
		want      string
	}{
		{"official whatsapp reaches the lead's holder", official, shared.EntryTypeWhatsApp, w.ws, ana},
		{"telegram with a shared phone reaches the lead's holder", telegramWithLead, shared.EntryTypeTelegram, w.ws, ana},
		{"instagram is matched by the contact (the @)", igConversation, shared.EntryTypeInstagram, w.ws, bob},
		{"the lead wins over the contact", tgBoth, shared.EntryTypeTelegram, w.ws, ana},
		{"a customer in nobody's carteira", strangerConversation, shared.EntryTypeInstagram, w.ws, ""},
		{"another workspace never sees the holder", igConversation, shared.EntryTypeInstagram, w.other, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := w.store.HolderOfConversation(c.workspace, c.entryID, string(c.entryType))
			if err != nil || got != c.want {
				t.Fatalf("holder = %q %v, want %q", got, err, c.want)
			}
		})
	}
}

func TestHolderOfConversationIgnoresAnAnonymizedLead(t *testing.T) {
	w := newWorld(t)
	lead := w.lead(w.ws, "Maria")
	w.put(uuid.NewString(), cp.LeadSubject(lead))
	official := w.officialEntry(lead)
	if err := w.db.Exec("UPDATE leads SET deleted_at = now() WHERE id = ?", lead).Error; err != nil {
		t.Fatal(err)
	}

	if got, err := w.store.HolderOfConversation(w.ws, official, string(shared.EntryTypeWhatsApp)); err != nil || got != "" {
		t.Fatalf("holder = %q %v, want none", got, err)
	}
}

func TestContactOfConversation(t *testing.T) {
	w := newWorld(t)
	igContact, igConversation := w.instagram(w.ws, "maria.ig", nil)
	lead := w.lead(w.ws, "Maria")
	official := w.officialEntry(lead)

	got, err := w.store.ContactOfConversation(ctx, w.ws, shared.EntryRef{EntryID: igConversation, EntryType: shared.EntryTypeInstagram})
	if err != nil || got != cp.ContactSubject(shared.EntryTypeInstagram, igContact) {
		t.Fatalf("instagram contact = %+v %v", got, err)
	}
	if _, err := w.store.ContactOfConversation(ctx, w.other, shared.EntryRef{EntryID: igConversation, EntryType: shared.EntryTypeInstagram}); !errors.Is(err, cp.ErrSubjectNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
	if _, err := w.store.ContactOfConversation(ctx, w.ws, shared.EntryRef{EntryID: official, EntryType: shared.EntryTypeWhatsApp}); !errors.Is(err, cp.ErrSubjectNotFound) {
		t.Fatalf("official whatsapp has no contact = %v", err)
	}
}

func TestListShowsWhoIsInACarteiraWithSearchAndPages(t *testing.T) {
	w := newWorld(t)
	ana, bob := uuid.NewString(), uuid.NewString()
	lead := w.lead(w.ws, "Maria Souza")
	w.put(ana, cp.LeadSubject(lead))
	igContact, _ := w.instagram(w.ws, "joana.ig", nil)
	w.put(ana, cp.ContactSubject(shared.EntryTypeInstagram, igContact))
	tgContact, _ := w.telegram(w.ws, nil)
	w.put(bob, cp.ContactSubject(shared.EntryTypeTelegram, tgContact))

	anas, err := w.store.List(ctx, w.ws, cp.ListQuery{HolderID: ana, PageSize: 10})
	if err != nil || anas.TotalItems != 2 || len(anas.Items) != 2 {
		t.Fatalf("ana's carteira = %+v %v", anas, err)
	}
	byChannel := map[shared.EntryType]cp.Display{}
	for _, row := range anas.Items {
		byChannel[row.Display.Channel] = row.Display
	}
	if d := byChannel[""]; d.Name != "Maria Souza" || d.Number == "" {
		t.Fatalf("lead display = %+v", d)
	}
	if d := byChannel[shared.EntryTypeInstagram]; d.Handle != "joana.ig" || d.Name != "Perfil joana.ig" {
		t.Fatalf("instagram display = %+v", d)
	}

	everyone, err := w.store.List(ctx, w.ws, cp.ListQuery{PageSize: 2})
	if err != nil || everyone.TotalItems != 3 || len(everyone.Items) != 2 {
		t.Fatalf("everyone page 1 = %+v %v", everyone, err)
	}

	found, err := w.store.List(ctx, w.ws, cp.ListQuery{Search: "JOANA"})
	if err != nil || found.TotalItems != 1 || found.Items[0].Display.Handle != "joana.ig" {
		t.Fatalf("search by handle = %+v %v", found, err)
	}
	telegram, err := w.store.List(ctx, w.ws, cp.ListQuery{Search: "tele gram"})
	if err != nil || telegram.TotalItems != 1 || telegram.Items[0].HolderID != bob {
		t.Fatalf("search by telegram name = %+v %v", telegram, err)
	}

	if none, err := w.store.List(ctx, w.other, cp.ListQuery{}); err != nil || none.TotalItems != 0 {
		t.Fatalf("another workspace = %+v %v", none, err)
	}
}
