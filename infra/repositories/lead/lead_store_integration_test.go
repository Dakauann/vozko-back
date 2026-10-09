package lead

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func leadStoreDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "lead_store")
	migrateLeadTables(t, db)
	return db
}

func migrateLeadTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadEvent{}, &schema.LeadPhone{}, &schema.LeadAddress{}, &schema.LeadRelation{},
		&schema.WhatsAppCampaignEntry{}, &schema.UnofficialWhatsAppContact{}, &schema.UnofficialWhatsAppConversation{},
		&schema.TelegramContact{}, &schema.TelegramConversation{}, &schema.InstagramContact{}, &schema.InstagramConversation{},
		&schema.FacebookContact{}, &schema.FacebookConversation{}, &schema.WebchatVisitor{}, &schema.WebchatConversation{}, &schema.CallList{}, &schema.CallListItem{}, &schema.GeocodeCache{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.CreateLeadCollectionConstraints(db); err != nil {
		t.Fatalf("constraints: %v", err)
	}
	if err := database.CreateSearchFold(db); err != nil {
		t.Fatalf("search fold: %v", err)
	}
}

func TestLeadsWithoutIdentityCoexistAndAnIdentityStaysUniqueAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()

	for _, name := range []string{"Maria", "José"} {
		l := &lead.Lead{WorkspaceID: ws, Name: name, NameSource: lead.SourceManual, Source: lead.SourceManual}
		if err := repo.Insert(ctx, l, nil); err != nil {
			t.Fatalf("insert %s without identity: %v", name, err)
		}
		stored, err := repo.FindByID(ws, l.ID)
		if err != nil || stored.Number != "" || stored.Version != 1 {
			t.Fatalf("stored = %+v, %v", stored, err)
		}
	}
	var nulls int64
	db.Raw("SELECT COUNT(*) FROM leads WHERE number IS NULL").Scan(&nulls)
	if nulls != 2 {
		t.Fatalf("leads without identity must store NULL, found %d", nulls)
	}

	first := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Source: lead.SourceManual}
	if err := repo.Insert(ctx, first, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	again := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Source: lead.SourceManual}
	if err := repo.Insert(ctx, again, nil); !errors.Is(err, lead.ErrLeadDuplicate) {
		t.Fatalf("a taken identity must be refused, got %v", err)
	}
}

func TestConcurrentFindOrCreateOnOneNumberMakesOneLeadAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ws := uuid.NewString()

	const callers = 8
	ids := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, _, err := repo.FindOrCreate(ws, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, Name: "Ana"})
			errs[i] = err
			if l != nil {
				ids[i] = l.ID
			}
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("callers got different leads: %v", ids)
		}
	}
	var count int64
	db.Raw("SELECT COUNT(*) FROM leads WHERE workspace_id = ?", ws).Scan(&count)
	if count != 1 {
		t.Fatalf("leads = %d, want 1", count)
	}
}

func TestSaveBumpsTheVersionAndTellsConflictFromMissingAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()

	l := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Name: "Ana", Source: lead.SourceManual}
	if err := repo.Insert(ctx, l, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	stale := *l
	l.Nickname = "Aninha"
	event := recordevent.Event{Actor: uuid.NewString(), Kind: lead.EventUpdated, Changes: []recordevent.Change{{Field: "nickname", After: "Aninha"}}}
	if err := repo.Save(ctx, l, 1, []recordevent.Event{event}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if l.Version != 2 {
		t.Fatalf("version = %d, want 2", l.Version)
	}

	stale.Nickname = "Ana Clara"
	if err := repo.Save(ctx, &stale, 1, nil); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("a stale save must conflict, got %v", err)
	}
	missing := &lead.Lead{ID: uuid.NewString(), WorkspaceID: ws, Number: "5511912345678"}
	missing.EnsureCollections()
	if err := repo.Save(ctx, missing, 1, nil); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("a missing lead must be not found, got %v", err)
	}
	foreign := *l
	foreign.WorkspaceID = uuid.NewString()
	if err := repo.Save(ctx, &foreign, l.Version, nil); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("another workspace must not reach the lead, got %v", err)
	}

	var events int64
	db.Raw("SELECT COUNT(*) FROM lead_events WHERE lead_id = ? AND kind = 'updated'", l.ID).Scan(&events)
	if events != 1 {
		t.Fatalf("events = %d, want 1", events)
	}

	stored, err := repo.FindByID(ws, l.ID)
	if err != nil || stored.Nickname != "Aninha" || stored.Version != 2 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestEveryIncomingWriteBumpsTheVersionAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ws := uuid.NewString()

	created, _, err := repo.FindOrCreate(ws, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.FindOrCreateMany(ws, []lead.BulkLeadInput{{Source: lead.SourceImport, Number: "5511987654321", Name: "Ana Souza"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := repo.FindOrCreateMany(ws, []lead.BulkLeadInput{{Source: lead.SourceChannel, Number: "5511987654321", Name: "Aninha"}}); err != nil {
		t.Fatalf("many: %v", err)
	}
	if _, _, err := repo.FindOrCreate(ws, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, ProfilePictureURL: "https://x/p.jpg"}); err != nil {
		t.Fatalf("picture: %v", err)
	}
	stored, err := repo.FindByID(ws, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Ana Souza" || stored.NameSource != lead.SourceImport || stored.ProfilePictureURL != "https://x/p.jpg" ||
		stored.StoredAge != nil || stored.Version != 3 {
		t.Fatalf("stored = %+v, want the imported name kept over the channel one and the picture, no age, at version 3", stored)
	}
}

func TestAManualRenameBetweenTheReadAndTheIncomingWriteSurvivesAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()

	created, _, err := repo.FindOrCreate(ws, "5511987654321", lead.LeadUpdate{Source: lead.SourceChannel, Name: "Aninha"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	staleRead, err := repo.FindByID(ws, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	aggregate, err := repo.Load(ctx, ws, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	renamed := *aggregate
	renamed.Name, renamed.NameSource = "Ana Maria", lead.SourceManual
	if err := repo.Save(ctx, &renamed, staleRead.Version, nil); err != nil {
		t.Fatalf("rename: %v", err)
	}

	wrote, err := repo.mergeIncoming(ws, []incomingMerge{{lead: staleRead, apply: mergeOf(lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana Souza"})}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	stored, err := repo.FindByID(ws, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if wrote || stored.Name != "Ana Maria" || stored.NameSource != lead.SourceManual || stored.Version != renamed.Version {
		t.Fatalf("the manual rename must survive the import, got %+v (wrote %v)", stored, wrote)
	}
}

func TestABatchMergeWritesEveryChangedLeadAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ws := uuid.NewString()

	const total = 1100
	unnamed := make([]lead.BulkLeadInput, total)
	named := make([]lead.BulkLeadInput, total)
	for i := 0; i < total; i++ {
		number := fmt.Sprintf("551198%07d", i)
		unnamed[i] = lead.BulkLeadInput{Source: lead.SourceChannel, Number: number}
		named[i] = lead.BulkLeadInput{Source: lead.SourceImport, Number: number, Name: fmt.Sprintf("Pessoa %d", i)}
	}
	if _, err := repo.FindOrCreateMany(ws, unnamed); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.FindOrCreateMany(ws, named)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(got) != total || got[named[total-1].Number].Version != 2 {
		t.Fatalf("got %d leads, last %+v", len(got), got[named[total-1].Number])
	}
	var renamed int64
	db.Raw("SELECT COUNT(*) FROM leads WHERE workspace_id = ? AND name LIKE 'Pessoa %' AND name_source = 'import' AND version = 2 AND age IS NULL", ws).Scan(&renamed)
	if renamed != total {
		t.Fatalf("renamed = %d, want %d", renamed, total)
	}
}

func TestSaveRoundTripsEveryRecordColumnInAnotherTimeZoneAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDBWith(t, "lead_store_tz", repotest.Options{TimeZone: "America/Sao_Paulo", PrepareStmt: true})
	migrateLeadTables(t, db)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()

	l := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Name: "Ana", Source: lead.SourceManual}
	if err := repo.Insert(ctx, l, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	birth := shared.Date{Year: 1990, Month: time.April, Day: 21}
	granted := time.Date(2026, time.October, 1, 23, 30, 0, 0, time.UTC)
	optedOut := time.Date(2026, time.October, 2, 1, 15, 0, 0, time.UTC)
	blockedAt := time.Date(2026, time.October, 3, 2, 45, 0, 0, time.UTC)
	blockedBy := uuid.NewString()
	ownerID := uuid.NewString()
	age := 36
	l.Name, l.NameSource = "Ana Souza", lead.SourceManual
	l.Nickname, l.Email, l.BirthDate = "Aninha", "ana@exemplo.com.br", &birth
	l.Owner = "ai:" + ownerID
	l.CustomFields = map[string]any{"cor": "azul"}
	l.WhatsAppOptIn = &lead.Consent{GrantedAt: granted, Source: lead.ConsentForm}
	l.OptedOutAt, l.OptOutSource = &optedOut, lead.OptOutOperator
	l.Blocked, l.BlockedAt, l.BlockedBy = true, blockedAt, &blockedBy
	l.ProfilePictureURL, l.StoredAge = "https://x/p.jpg", &age
	if err := repo.Save(ctx, l, 1, nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	stored, err := repo.FindByID(ws, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.BirthDate == nil || *stored.BirthDate != birth {
		t.Fatalf("birth date = %v, want %v", stored.BirthDate, birth)
	}
	if stored.Name != "Ana Souza" || stored.NameSource != lead.SourceManual || stored.Nickname != "Aninha" || stored.Email != "ana@exemplo.com.br" ||
		stored.Owner != l.Owner || stored.CustomFields["cor"] != "azul" || stored.ProfilePictureURL != "https://x/p.jpg" ||
		stored.StoredAge == nil || *stored.StoredAge != 36 || stored.Version != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.WhatsAppOptIn == nil || !stored.WhatsAppOptIn.GrantedAt.Equal(granted) || stored.WhatsAppOptIn.Source != lead.ConsentForm {
		t.Fatalf("consent = %+v", stored.WhatsAppOptIn)
	}
	if stored.OptedOutAt == nil || !stored.OptedOutAt.Equal(optedOut) || stored.OptOutSource != lead.OptOutOperator {
		t.Fatalf("opted out = %v from %q", stored.OptedOutAt, stored.OptOutSource)
	}
	if !stored.Blocked || !stored.BlockedAt.Equal(blockedAt) || stored.BlockedBy == nil || *stored.BlockedBy != blockedBy {
		t.Fatalf("block = %v %v %v", stored.Blocked, stored.BlockedAt, stored.BlockedBy)
	}
	var day string
	db.Raw("SELECT birth_date::text FROM leads WHERE id = ?", l.ID).Scan(&day)
	if day != "1990-04-21" {
		t.Fatalf("stored day = %q", day)
	}
}

func TestASystemMergeEventIsStoredWithoutAPersonAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()

	l := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Source: lead.SourceChannel}
	if err := repo.Insert(ctx, l, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	merged := *l
	merged.MergeIncoming(lead.LeadUpdate{Source: lead.SourceImport, Name: "Ana"})
	event := lead.Changes(lead.EventMerged, "system", l, &merged, nil)
	if err := repo.Save(ctx, &merged, l.Version, []recordevent.Event{event}); err != nil {
		t.Fatalf("save: %v", err)
	}
	var kinds []string
	db.Raw("SELECT actor_kind FROM lead_events WHERE lead_id = ? AND actor_id IS NULL", l.ID).Scan(&kinds)
	if len(kinds) != 1 || kinds[0] != "system" {
		t.Fatalf("system events = %v", kinds)
	}
}

func TestTheIdentityCannotChangeOnceAConversationExistsEvenAfterAStaleCheckAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()
	l, err := lead.New(ws, lead.Draft{Number: "5511987654321", Name: "Maria"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, l, nil); err != nil {
		t.Fatal(err)
	}
	checked, err := repo.Load(ctx, ws, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checked.ChangeIdentity("5521998765432", false); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schema.WhatsAppCampaignEntry{ID: uuid.NewString(), CampaignID: uuid.NewString(), LeadID: l.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, checked, 1, nil); !errors.Is(err, lead.ErrIdentityInUse) {
		t.Fatalf("a number change after a conversation started = %v, want %v", err, lead.ErrIdentityInUse)
	}
	same, err := repo.Load(ctx, ws, l.ID)
	if err != nil || same.Number != "5511987654321" {
		t.Fatalf("stored = %+v, %v", same, err)
	}
	same.Nickname = "Mari"
	if err := repo.Save(ctx, same, same.Version, nil); err != nil {
		t.Fatalf("other edits of a lead with conversations still save: %v", err)
	}
}
