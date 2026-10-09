package conversation_repository

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/repositories/repotest"
)

const entryUnionTablesDDL = `
CREATE TABLE leads (id uuid PRIMARY KEY, name text, number text, deleted_at timestamptz);
CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, workspace_id uuid, business_phone_id uuid, department_id uuid, type text);
CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid, lead_id uuid, conversation_status text,
	created_at timestamptz, updated_at timestamptz, last_message_at timestamptz, deleted_at timestamptz);
CREATE TABLE instagram_accounts (id uuid PRIMARY KEY, workspace_id uuid, department_id uuid);
CREATE TABLE instagram_conversations (id uuid PRIMARY KEY, ig_account_id uuid, contact_id uuid, conversation_status text,
	created_at timestamptz, updated_at timestamptz, last_message_at timestamptz, deleted_at timestamptz);
CREATE TABLE facebook_pages (id uuid PRIMARY KEY, workspace_id uuid, department_id uuid);
CREATE TABLE facebook_conversations (id uuid PRIMARY KEY, page_id uuid, contact_id uuid, conversation_status text,
	created_at timestamptz, updated_at timestamptz, last_message_at timestamptz, deleted_at timestamptz);
CREATE TABLE telegram_accounts (id uuid PRIMARY KEY, workspace_id uuid, department_id uuid);
CREATE TABLE telegram_conversations (id uuid PRIMARY KEY, account_id uuid, contact_id uuid, conversation_status text,
	created_at timestamptz, updated_at timestamptz, last_message_at timestamptz, deleted_at timestamptz);
CREATE TABLE webchat_widgets (id uuid PRIMARY KEY, workspace_id uuid, department_id uuid);
CREATE TABLE webchat_visitors (id uuid PRIMARY KEY, lead_id uuid);
CREATE TABLE webchat_conversations (id uuid PRIMARY KEY, widget_id uuid, visitor_id uuid, conversation_status text,
	created_at timestamptz, updated_at timestamptz, last_message_at timestamptz, deleted_at timestamptz);
CREATE TABLE unofficial_whatsapp_instances (id uuid PRIMARY KEY, workspace_id uuid, department_id uuid);
CREATE TABLE unofficial_whatsapp_contacts (id uuid PRIMARY KEY, lead_id uuid);
CREATE TABLE unofficial_whatsapp_campaigns (id uuid PRIMARY KEY, deleted_at timestamptz);
CREATE TABLE unofficial_whatsapp_conversations (id uuid PRIMARY KEY, instance_id uuid, contact_id uuid, campaign_id text,
	conversation_status text, created_at timestamptz, updated_at timestamptz, last_message_at timestamptz, deleted_at timestamptz);
`

func entryUnionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "entry_sel_test")
	if err := db.Exec(entryUnionTablesDDL).Error; err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return db
}

type seededEntries struct {
	workspaceID string
	ids         []string
}

func seedEntryUnion(t *testing.T, db *gorm.DB) seededEntries {
	t.Helper()
	ws, other := uuid.NewString(), uuid.NewString()
	campaign, foreignCampaign, igAccount, tgAccount := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(sql string, args ...interface{}) {
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO whatsapp_campaigns (id, workspace_id, type) VALUES (?, ?, 'standard'), (?, ?, 'standard')`, campaign, ws, foreignCampaign, other)
	exec(`INSERT INTO instagram_accounts (id, workspace_id) VALUES (?, ?)`, igAccount, ws)
	exec(`INSERT INTO telegram_accounts (id, workspace_id) VALUES (?, ?)`, tgAccount, ws)

	seeded := seededEntries{workspaceID: ws}
	for range 3 {
		id := uuid.NewString()
		exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, created_at, updated_at, last_message_at) VALUES (?, ?, now(), now(), now())`, id, campaign)
		seeded.ids = append(seeded.ids, id)
	}
	for range 2 {
		id := uuid.NewString()
		exec(`INSERT INTO instagram_conversations (id, ig_account_id, created_at, updated_at, last_message_at) VALUES (?, ?, now(), now(), now())`, id, igAccount)
		seeded.ids = append(seeded.ids, id)
	}
	telegram := uuid.NewString()
	exec(`INSERT INTO telegram_conversations (id, account_id, created_at, updated_at, last_message_at) VALUES (?, ?, now(), now(), now())`, telegram, tgAccount)
	seeded.ids = append(seeded.ids, telegram)

	exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, created_at, updated_at, last_message_at) VALUES (?, ?, now(), now(), now())`, uuid.NewString(), foreignCampaign)
	exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, created_at, updated_at, last_message_at, deleted_at) VALUES (?, ?, now(), now(), now(), now())`, uuid.NewString(), campaign)
	return seeded
}

func pageThrough(t *testing.T, repo *repository, input conversation.SearchByFilterInput, limit int) []shared.EntryRef {
	t.Helper()
	var all []shared.EntryRef
	after := ""
	for range 20 {
		page, err := repo.ResolveEntryRefsByFilter(input, after, limit)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page...)
		if len(page) < limit {
			return all
		}
		after = page[len(page)-1].EntryID
	}
	t.Fatal("paging never ended")
	return nil
}

func TestResolveEntryRefsByFilter_PagesTheWholeUnionOnceAgainstPostgres(t *testing.T) {
	db := entryUnionDB(t)
	seeded := seedEntryUnion(t, db)
	repo := &repository{db: db}
	input := conversation.SearchByFilterInput{WorkspaceID: seeded.workspaceID}

	refs := pageThrough(t, repo, input, 2)

	var dbOrder []string
	if err := db.Raw(`SELECT x FROM unnest(?::text[]) AS x ORDER BY x`, pq.Array(seeded.ids)).Scan(&dbOrder).Error; err != nil {
		t.Fatal(err)
	}
	if got := refIDs(refs); fmt.Sprint(got) != fmt.Sprint(dbOrder) {
		t.Fatalf("each entry must be visited exactly once in the database order:\n got %v\nwant %v", got, dbOrder)
	}
	types := map[shared.EntryType]int{}
	for _, r := range refs {
		types[r.EntryType]++
	}
	if types[shared.EntryTypeWhatsApp] != 3 || types[shared.EntryTypeInstagram] != 2 || types[shared.EntryTypeTelegram] != 1 {
		t.Fatalf("every source must be paged, got %v", types)
	}

	total, err := repo.CountEntriesByFilter(input)
	if err != nil || total != int64(len(seeded.ids)) {
		t.Fatalf("count = %d, %v; want %d", total, err, len(seeded.ids))
	}
}

func TestResolveEntryRefsByFilter_SkipsExclusionsAgainstPostgres(t *testing.T) {
	db := entryUnionDB(t)
	seeded := seedEntryUnion(t, db)
	repo := &repository{db: db}
	excluded := []string{seeded.ids[0], seeded.ids[4]}
	input := conversation.SearchByFilterInput{WorkspaceID: seeded.workspaceID, ExcludeEntryIDs: excluded}

	refs := pageThrough(t, repo, input, 2)

	got := refIDs(refs)
	if len(got) != len(seeded.ids)-len(excluded) {
		t.Fatalf("got %d entries, want %d", len(got), len(seeded.ids)-len(excluded))
	}
	for _, id := range got {
		if id == excluded[0] || id == excluded[1] {
			t.Fatalf("excluded entry %s was resolved", id)
		}
	}

	before, err := repo.CountEntriesByFilter(conversation.SearchByFilterInput{WorkspaceID: seeded.workspaceID})
	if err != nil || before != int64(len(seeded.ids)) {
		t.Fatalf("the confirming count is taken before exclusions, got %d, %v", before, err)
	}
}

func refIDs(refs []shared.EntryRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.EntryID
	}
	return out
}
