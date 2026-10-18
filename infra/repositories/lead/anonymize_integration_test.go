package lead

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/infra/database/schema"
)

type anonymizationWorld struct {
	db           *gorm.DB
	repo         *repository
	ws           string
	maria        *lead.Lead
	joao         *lead.Lead
	ana          *lead.Lead
	strangerCall string
	anaCall      string
	anaSend      string
}

func newAnonymizationWorld(t *testing.T) anonymizationWorld {
	t.Helper()
	db, repo := collectionsDB(t)
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.LeadMemory{}, &schema.WhatsAppCampaignEntry{}, &schema.UnofficialWhatsAppCampaignEntry{},
		&schema.Call{}, &schema.WhatsAppTemplateSend{}, &schema.CallListItem{}, &schema.CustomerPortfolioEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ws := uuid.NewString()
	w := anonymizationWorld{db: db, repo: repo, ws: ws}
	w.maria = newLead(t, repo, ws, lead.Draft{Number: "5511987654321", Name: "Maria Souza", Email: "maria@exemplo.com.br", BirthDate: "1980-05-01",
		Phones: []lead.ContactPhone{{Number: "1133334444", Label: lead.PhoneLandline}}, Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: paulista}}})
	w.joao = newLead(t, repo, ws, lead.Draft{Name: "João Souza"})
	w.ana = newLead(t, repo, ws, lead.Draft{Name: "Ana Lima", Phones: []lead.ContactPhone{{Number: "1133334444", Label: lead.PhoneLandline}}})

	relation, err := lead.RelationBetween(w.maria, w.joao, lead.KindChild, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddRelation(context.Background(), ws, relation); err != nil {
		t.Fatalf("relate: %v", err)
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Exec("UPDATE leads SET custom_fields = ?::jsonb, whatsapp_opt_in_at = now(), whatsapp_opt_in_source = 'manual' WHERE id = ?",
		`{"classificacao": "positivo"}`, w.maria.ID).Error)
	must(db.Create(&schema.LeadMemory{WorkspaceID: ws, LeadID: w.maria.ID, Category: "family", Content: "tem dois filhos", ActorKind: "human", ActorID: "u"}).Error)
	mariaEntry, anaEntry := uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: mariaEntry, CampaignID: uuid.NewString(), LeadID: w.maria.ID,
		Variables: pq.StringArray{"Maria"}, Metadata: schema.LeadMetadata{"bairro": "Bela Vista"}}).Error)
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: anaEntry, CampaignID: uuid.NewString(), LeadID: w.ana.ID}).Error)
	must(db.Create(&schema.UnofficialWhatsAppCampaignEntry{ID: uuid.NewString(), CampaignID: uuid.NewString(), WorkspaceID: ws, LeadID: &w.maria.ID,
		Number: "5511987654321", Name: "Maria", Variables: pq.StringArray{"Maria"}, Metadata: schema.LeadMetadata{"cpf": "x"}}).Error)
	call := func(id string, leadID *string, from, to string) {
		must(db.Create(&schema.Call{ID: id, CallID: uuid.NewString(), WorkspaceID: ws, Type: "voice", Direction: "inbound", Source: "sip",
			Status: "completed", LeadID: leadID, PhoneFrom: from, PhoneTo: to, StartedAt: time.Now()}).Error)
	}
	call(uuid.NewString(), nil, "+5511987654321", "551133334444")
	call(uuid.NewString(), &w.maria.ID, "551199990000", "551133334444")
	w.anaCall = uuid.NewString()
	call(w.anaCall, &w.ana.ID, "551199990000", "551133334444")
	w.strangerCall = uuid.NewString()
	must(db.Create(&schema.Call{ID: w.strangerCall, CallID: uuid.NewString(), WorkspaceID: uuid.NewString(), Type: "voice", Direction: "inbound", Source: "sip",
		Status: "completed", PhoneFrom: "+5511987654321", PhoneTo: "551100000000", StartedAt: time.Now()}).Error)
	send := func(id string, entryID *string, to string) {
		must(db.Create(&schema.WhatsAppTemplateSend{ID: id, WorkspaceID: ws, UserID: uuid.NewString(), IdempotencyKey: uuid.NewString(), BusinessPhoneID: uuid.NewString(),
			TemplateID: uuid.NewString(), ToNumber: to, EntryID: entryID, ChargedMicros: 1200}).Error)
	}
	send(uuid.NewString(), nil, "551187654321")
	send(uuid.NewString(), &mariaEntry, "551133334444")
	w.anaSend = uuid.NewString()
	send(w.anaSend, &anaEntry, "551133334444")
	must(db.Exec("INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, note, created_at, updated_at)"+
		" VALUES (?, ?, ?, ?, '5511987654321', 1, 'closed', 'mora com a filha', now(), now())", uuid.NewString(), ws, uuid.NewString(), w.maria.ID).Error)
	must(db.Exec("INSERT INTO customer_portfolio_entries (id, workspace_id, holder_id, lead_id, created_at, updated_at) VALUES (?, ?, ?, ?, now(), now())",
		uuid.NewString(), ws, uuid.NewString(), w.maria.ID).Error)
	return w
}

func (w anonymizationWorld) count(t *testing.T, sql string, args ...interface{}) int64 {
	t.Helper()
	var n int64
	if err := w.db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func (w anonymizationWorld) text(t *testing.T, sql string, args ...interface{}) string {
	t.Helper()
	var s string
	if err := w.db.Raw(sql, args...).Scan(&s).Error; err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAnonymizeErasesThePersonEverywhereAgainstPostgres(t *testing.T) {
	w := newAnonymizationWorld(t)
	actor := uuid.NewString()
	erasure, err := w.repo.Anonymize(context.Background(), w.ws, w.maria.ID, actor, time.Now().UTC())
	if err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	if tally, ok := erasure.Counterparts[w.joao.ID]; len(erasure.Counterparts) != 1 || !ok || tally.Counts.Relatives != 0 {
		t.Fatalf("counterparts = %v", erasure.Counterparts)
	}

	var row struct {
		Number       *string
		Name         string
		Email        *string
		BirthDate    *string
		CustomFields *string
		OptIn        *time.Time `gorm:"column:whatsapp_opt_in_at"`
		Deleted      *time.Time `gorm:"column:deleted_at"`
		Version      int64
	}
	if err := w.db.Raw("SELECT number, name, email, birth_date::text AS birth_date, custom_fields::text AS custom_fields, whatsapp_opt_in_at, deleted_at, version FROM leads WHERE id = ?", w.maria.ID).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Number != nil || row.Name != "" || row.Email != nil || row.BirthDate != nil || row.CustomFields != nil || row.OptIn != nil || row.Deleted == nil {
		t.Fatalf("lead row = %+v", row)
	}
	if _, err := w.repo.FindByID(w.ws, w.maria.ID); err == nil {
		t.Fatal("an anonymized lead must be gone from every read")
	}

	for table, sql := range map[string]string{
		"lead_phones":    "SELECT count(*) FROM lead_phones WHERE lead_id = ?",
		"lead_addresses": "SELECT count(*) FROM lead_addresses WHERE lead_id = ?",
		"lead_relations": "SELECT count(*) FROM lead_relations WHERE lead_id = ? OR other_lead_id = ?",
		"lead_memories":  "SELECT count(*) FROM lead_memories WHERE lead_id = ?",
	} {
		args := []interface{}{w.maria.ID}
		if strings.Contains(sql, "other_lead_id") {
			args = append(args, w.maria.ID)
		}
		if n := w.count(t, sql, args...); n != 0 {
			t.Errorf("%s still holds %d rows", table, n)
		}
	}
	if n := w.count(t, "SELECT relatives_count FROM leads WHERE id = ?", w.joao.ID); n != 0 {
		t.Errorf("the relative still counts the anonymized lead: %d", n)
	}

	events := w.text(t, "SELECT string_agg(changes::text, '|') FROM lead_events WHERE lead_id = ?", w.maria.ID)
	for _, leaked := range []string{"Maria", "5511987654321", "maria@exemplo", "1980"} {
		if strings.Contains(events, leaked) {
			t.Fatalf("lead events still hold %q: %s", leaked, events)
		}
	}
	var stored []string
	if err := w.db.Raw("SELECT changes::text FROM lead_events WHERE lead_id = ? AND kind <> 'anonymized'", w.maria.ID).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	for _, raw := range stored {
		var changes []recordevent.Change
		if err := json.Unmarshal([]byte(raw), &changes); err != nil {
			t.Fatalf("event %s: %v", raw, err)
		}
		for _, c := range changes {
			if c.Field == "" || !c.Redacted || c.Before != nil || c.After != nil {
				t.Fatalf("event change = %+v", c)
			}
		}
	}
	if n := w.count(t, "SELECT count(*) FROM lead_events WHERE lead_id = ? AND kind = 'anonymized' AND actor_id = ?", w.maria.ID, actor); n != 1 {
		t.Fatalf("the anonymization is recorded %d times", n)
	}

	if s := w.text(t, "SELECT coalesce(array_to_string(variables, ','), '') || metadata::text FROM whatsapp_campaign_entries WHERE lead_id = ?", w.maria.ID); s != "{}" {
		t.Errorf("official entry = %q", s)
	}
	if s := w.text(t, "SELECT number || '/' || name || '/' || coalesce(array_to_string(variables, ','), '') || metadata::text FROM unofficial_whatsapp_campaign_entries WHERE lead_id = ?", w.maria.ID); s != "••••4321//{}" {
		t.Errorf("unofficial entry = %q", s)
	}
	if n := w.count(t, "SELECT count(*) FROM calls WHERE workspace_id = ? AND lead_id = ? AND phone_from = '551199990000' AND phone_to = '••••4444'", w.ws, w.maria.ID); n != 1 {
		t.Errorf("a call of the person must have every number of the person masked, found %d", n)
	}
	if n := w.count(t, "SELECT count(*) FROM calls WHERE workspace_id = ? AND lead_id IS NULL AND phone_from = '••••4321' AND phone_to = '551133334444'", w.ws); n != 1 {
		t.Errorf("a call of nobody masks the WhatsApp number and keeps the landline another lead still holds, found %d", n)
	}
	if s := w.text(t, "SELECT phone_to FROM calls WHERE id = ?", w.anaCall); s != "551133334444" {
		t.Errorf("the call of another lead on the shared landline was masked: %q", s)
	}
	if s := w.text(t, "SELECT phone_from FROM calls WHERE id = ?", w.strangerCall); s != "+5511987654321" {
		t.Errorf("another workspace's call was touched: %q", s)
	}
	if n := w.count(t, "SELECT count(*) FROM whatsapp_template_sends WHERE workspace_id = ? AND charged_micros = 1200 AND to_number IN ('••••4321', '••••4444') AND id <> ?", w.ws, w.anaSend); n != 2 {
		t.Errorf("the person's sends keep their billing with the number masked, found %d", n)
	}
	if s := w.text(t, "SELECT to_number FROM whatsapp_template_sends WHERE id = ?", w.anaSend); s != "551133334444" {
		t.Errorf("the billed send of another lead on the shared landline was masked: %q", s)
	}
	if n := w.count(t, "SELECT count(*) FROM call_list_items WHERE lead_id = ? AND phone = '' AND note IS NULL AND state = 'closed'", w.maria.ID); n != 1 {
		t.Errorf("the call list item of the person keeps its number or note, found %d erased", n)
	}
	if n := w.count(t, "SELECT count(*) FROM customer_portfolio_entries WHERE lead_id = ?", w.maria.ID); n != 0 {
		t.Errorf("an anonymized person must leave every carteira, found %d entries", n)
	}
}

func TestAnonymizeRemovesARelationLinkedAfterItsFirstReadAgainstPostgres(t *testing.T) {
	w := newAnonymizationWorld(t)
	late := newLead(t, w.repo, w.ws, lead.Draft{Name: "Bia Souza"})
	run := &anonymization{actorID: uuid.NewString(), at: time.Now().UTC(), erasure: lead.Erasure{LeadID: w.maria.ID, Rows: map[lead.ErasureTarget]int64{}}}
	err := w.db.Transaction(func(tx *gorm.DB) error {
		run.tx = tx
		counterparts, err := run.counterparts(w.ws, w.maria.ID)
		if err != nil {
			return err
		}
		relation, err := lead.RelationBetween(w.maria, late, lead.KindSibling, uuid.NewString())
		if err != nil {
			return err
		}
		if _, err := w.repo.AddRelation(context.Background(), w.ws, relation); err != nil {
			return err
		}
		if err := run.lock(w.ws, w.maria.ID, counterparts); err != nil {
			return err
		}
		return run.unrelate()
	})
	if err != nil {
		t.Fatalf("anonymization steps: %v", err)
	}
	if n := w.count(t, "SELECT count(*) FROM lead_relations WHERE lead_id = ? OR other_lead_id = ?", w.maria.ID, w.maria.ID); n != 0 {
		t.Fatalf("%d relations escaped the erasure", n)
	}
	if n := w.count(t, "SELECT relatives_count FROM leads WHERE id = ?", late.ID); n != 0 {
		t.Fatalf("the lead linked in the meantime still counts the erased person: %d", n)
	}
	if _, ok := run.erasure.Counterparts[late.ID]; !ok || run.erasure.Rows[lead.ErasureRelations] != 2 {
		t.Fatalf("erasure = %+v", run.erasure)
	}
}
