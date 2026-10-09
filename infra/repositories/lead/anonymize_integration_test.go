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

	"vozko/domain/address"
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
		&schema.Call{}, &schema.WhatsAppTemplateSend{}, &schema.CallListItem{}, &schema.GeocodeCache{}); err != nil {
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
	must(db.Create(&schema.UnofficialWhatsAppCampaignEntry{ID: uuid.NewString(), CampaignID: uuid.NewString(), WorkspaceID: ws, LeadID: w.maria.ID,
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

func TestAnonymizeErasesStoredAnswersNoOtherLiveLeadHoldsAgainstPostgres(t *testing.T) {
	w := newAnonymizationWorld(t)
	ctx := context.Background()
	shared, own := lead.Address{Postal: paulista}, lead.Address{Postal: bahia}
	carla := newLead(t, w.repo, w.ws, lead.Draft{Name: "Carla Souza", Addresses: []lead.AddressInput{
		{Label: lead.AddressHome, Primary: true, Postal: paulista}, {Label: lead.AddressWork, Postal: bahia},
	}})
	elsewhere := uuid.NewString()
	newLead(t, w.repo, elsewhere, lead.Draft{Name: "Outra Pessoa", Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: bahia}}})
	store, cached := w.storeAnswer(t), w.cachedAnswer(t)
	store(w.ws, shared.Fingerprint())
	store(w.ws, own.Fingerprint())
	store(elsewhere, own.Fingerprint())

	erasure, err := w.repo.Anonymize(ctx, w.ws, carla.ID, uuid.NewString(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if erasure.Rows[lead.ErasureGeocodeCache] != 1 || cached(w.ws, own.Fingerprint()) {
		t.Fatalf("erasure = %+v, want the answer for the address only Carla held erased", erasure.Rows)
	}
	if !cached(w.ws, shared.Fingerprint()) {
		t.Fatal("Maria still lives at the shared address, so its answer stays")
	}
	if !cached(elsewhere, own.Fingerprint()) {
		t.Fatal("another workspace's answer is never touched")
	}

	if _, err := w.repo.Anonymize(ctx, w.ws, w.maria.ID, uuid.NewString(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if cached(w.ws, shared.Fingerprint()) {
		t.Fatal("once no live lead holds the address, its stored answer is erased")
	}
}

func (w anonymizationWorld) storeAnswer(t *testing.T) func(ws, fingerprint string) {
	return func(ws, fingerprint string) {
		t.Helper()
		if err := w.db.Exec("INSERT INTO geocode_cache (workspace_id, fingerprint, outcome, provider, resolved_at) VALUES (?, ?, 'not_found', 'opencage', now())", ws, fingerprint).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func (w anonymizationWorld) cachedAnswer(t *testing.T) func(ws, fingerprint string) bool {
	return func(ws, fingerprint string) bool {
		t.Helper()
		return w.count(t, "SELECT count(*) FROM geocode_cache WHERE workspace_id = ? AND fingerprint = ?", ws, fingerprint) == 1
	}
}

func keepingAddresses(l *lead.Lead, replace map[string]address.Postal, drop map[string]bool) []lead.AddressInput {
	var inputs []lead.AddressInput
	for _, a := range l.Addresses {
		if drop[a.ID] {
			continue
		}
		postal := a.Postal
		if next, ok := replace[a.ID]; ok {
			postal = next
		}
		inputs = append(inputs, lead.AddressInput{ID: a.ID, Label: a.Label, Primary: a.Primary, Postal: postal})
	}
	return inputs
}

func saveAddresses(t *testing.T, w anonymizationWorld, id string, edit func(l *lead.Lead) []lead.AddressInput) {
	t.Helper()
	l := reload(t, w.repo, w.ws, id)
	if err := l.SetAddresses(edit(l)); err != nil {
		t.Fatal(err)
	}
	if err := w.repo.Save(context.Background(), l, l.Version, nil); err != nil {
		t.Fatalf("save addresses: %v", err)
	}
}

func TestEditingOrRemovingAnAddressReleasesItsStoredAnswerAgainstPostgres(t *testing.T) {
	w := newAnonymizationWorld(t)
	store, cached := w.storeAnswer(t), w.cachedAnswer(t)
	carla := newLead(t, w.repo, w.ws, lead.Draft{Name: "Carla Souza", Addresses: []lead.AddressInput{
		{Label: lead.AddressHome, Primary: true, Postal: paulista}, {Label: lead.AddressWork, Postal: bahia},
	}})
	shared, own := lead.Address{Postal: paulista}.Fingerprint(), lead.Address{Postal: bahia}.Fingerprint()
	elsewhere := uuid.NewString()
	store(w.ws, shared)
	store(w.ws, own)
	store(elsewhere, own)
	moved := bahia
	moved.Number = "20"

	saveAddresses(t, w, carla.ID, func(l *lead.Lead) []lead.AddressInput {
		return keepingAddresses(l, map[string]address.Postal{l.Addresses[1].ID: moved}, nil)
	})
	if cached(w.ws, own) {
		t.Fatal("once Carla's work address text changed, no lead holds the old text, so its answer must go")
	}
	if !cached(w.ws, shared) || !cached(elsewhere, own) {
		t.Fatal("an answer another lead or another workspace still holds stays")
	}

	saveAddresses(t, w, carla.ID, func(l *lead.Lead) []lead.AddressInput {
		return keepingAddresses(l, nil, map[string]bool{l.Addresses[0].ID: true})
	})
	if !cached(w.ws, shared) {
		t.Fatal("Carla removed the shared address but Maria still lives there, so its answer stays")
	}

	saveAddresses(t, w, w.maria.ID, func(l *lead.Lead) []lead.AddressInput {
		return keepingAddresses(l, map[string]address.Postal{l.Addresses[0].ID: moved}, nil)
	})
	if cached(w.ws, shared) {
		t.Fatal("once the last holder edited the text away, its answer must go")
	}
}

func TestTwoHoldersOfOneTextAnonymizedTogetherLeaveNoStoredAnswerAgainstPostgres(t *testing.T) {
	w := newAnonymizationWorld(t)
	store, cached := w.storeAnswer(t), w.cachedAnswer(t)
	flatmate := newLead(t, w.repo, w.ws, lead.Draft{Name: "Rita Lima", Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: paulista}}})
	shared := lead.Address{Postal: paulista}.Fingerprint()
	store(w.ws, shared)

	first := w.db.Begin()
	defer first.Rollback()
	run := &anonymization{tx: first, actorID: uuid.NewString(), at: time.Now().UTC(), erasure: lead.Erasure{LeadID: w.maria.ID, Rows: map[lead.ErasureTarget]int64{}}}
	if err := run.erase(w.ws, w.maria.ID); err != nil {
		t.Fatalf("first anonymization: %v", err)
	}
	if run.erasure.Rows[lead.ErasureGeocodeCache] != 0 {
		t.Fatalf("the first anonymization erased %d answers while Rita still lives there", run.erasure.Rows[lead.ErasureGeocodeCache])
	}
	done := make(chan error, 1)
	go func() {
		_, err := w.repo.Anonymize(context.Background(), w.ws, flatmate.ID, uuid.NewString(), time.Now().UTC())
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the second anonymization returned %v while the first was still open, want it to wait on the stored answer", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := first.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second anonymization: %v", err)
	}
	if cached(w.ws, shared) {
		t.Fatal("both holders were anonymized, so the stored answer must be gone")
	}
}
