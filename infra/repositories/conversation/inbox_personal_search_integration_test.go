package conversation_repository

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

type inboxWorld struct {
	db        *gorm.DB
	workspace string
	account   string
	ana, bia  string
	label     string
	byName    map[string]string
	names     map[string]string
}

func (w inboxWorld) ids(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = w.byName[n]
	}
	return out
}

func (w inboxWorld) namesOf(entries []conversation.EntryWithLastMessage) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = w.names[e.EntryID]
	}
	return out
}

func seedInboxWorld(t *testing.T) inboxWorld {
	t.Helper()
	db := repotest.IsolatedDB(t, "inbox_personal_test",
		&schema.TelegramAccount{},
		&schema.TelegramContact{},
		&schema.TelegramConversation{},
		&schema.ConversationMessage{},
		&schema.ConversationPersonalMark{},
		&schema.Label{},
		&schema.EntryLabel{},
		&schema.InboxAssignment{},
		&schema.ConversationDelegation{},
	)
	exec := func(sql string, args ...interface{}) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	w := inboxWorld{
		db: db, workspace: uuid.NewString(), account: uuid.NewString(),
		ana: uuid.NewString(), bia: uuid.NewString(), label: uuid.NewString(),
		byName: map[string]string{}, names: map[string]string{},
	}
	exec(`INSERT INTO telegram_accounts (id, workspace_id, bot_user_id) VALUES (?, ?, 1)`, w.account, w.workspace)

	base := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	hour := func(h float64) time.Time { return base.Add(time.Duration(h * float64(time.Hour))) }
	type conv struct {
		name     string
		last     time.Time
		customer *time.Time
		agent    *time.Time
		fromUs   bool
		waiting  *time.Time
		status   string
	}
	ptr := func(t time.Time) *time.Time { return &t }
	convs := []conv{
		{"customer-at-10", hour(10), ptr(hour(10)), ptr(hour(9)), false, ptr(hour(9.2)), "ongoing"},
		{"team-at-11", hour(11), ptr(hour(8)), ptr(hour(11)), true, nil, "ongoing"},
		{"customer-at-9.5", hour(9.5), ptr(hour(9.5)), nil, false, ptr(hour(9.5)), "ongoing"},
		{"customer-at-12", hour(12), ptr(hour(12)), nil, false, ptr(hour(12)), "ongoing"},
		{"pinned-by-ana", hour(7), ptr(hour(6)), ptr(hour(7)), true, nil, "ongoing"},
		{"finished-long-ago-waiting", hour(13), ptr(hour(13)), nil, false, ptr(hour(1)), "finished"},
		{"finished-recently", hour(14), ptr(hour(14)), ptr(hour(13.5)), false, nil, "finished"},
	}
	for i, c := range convs {
		id, contact := uuid.NewString(), uuid.NewString()
		w.byName[c.name], w.names[id] = id, c.name
		exec(`INSERT INTO telegram_contacts (id, workspace_id, account_id, tg_user_id, tg_chat_id, first_name) VALUES (?, ?, ?, ?, ?, ?)`,
			contact, w.workspace, w.account, i+1, i+1, c.name)
		exec(`INSERT INTO telegram_conversations (id, workspace_id, account_id, contact_id, tg_chat_id, conversation_status, last_message_at, last_customer_message_at, last_agent_message_at, awaiting_reply_since)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, w.workspace, w.account, contact, i+1, c.status, c.last, c.customer, c.agent, c.waiting)
		sender := "contact"
		if c.fromUs {
			sender = "human"
		}
		exec(`INSERT INTO conversation_messages (id, entry_id, entry_type, message_type, from_participant, text, sender_kind, created_at, updated_at)
			VALUES (?, ?, 'telegram', 'user_message', 'x', 'oi', ?, ?, ?)`, uuid.NewString(), id, sender, c.last, c.last)
	}

	exec(`INSERT INTO conversation_personal_marks (user_id, entry_type, entry_id, pinned_at, updated_at) VALUES (?, 'telegram', ?, NOW(), NOW())`, w.ana, w.byName["pinned-by-ana"])
	exec(`INSERT INTO conversation_personal_marks (user_id, entry_type, entry_id, pinned_at, updated_at) VALUES (?, 'telegram', ?, NOW(), NOW())`, w.bia, w.byName["team-at-11"])

	exec(`INSERT INTO labels (id, workspace_id, name) VALUES (?, ?, 'VIP')`, w.label, w.workspace)
	for _, name := range []string{"customer-at-10", "customer-at-9.5"} {
		exec(`INSERT INTO entry_labels (id, label_id, entry_id, entry_type, workspace_id) VALUES (?, ?, ?, 'telegram', ?)`, uuid.NewString(), w.label, w.byName[name], w.workspace)
	}
	removed := uuid.NewString()
	exec(`INSERT INTO entry_labels (id, label_id, entry_id, entry_type, workspace_id, deleted_at) VALUES (?, ?, ?, 'telegram', ?, NOW())`, removed, w.label, w.byName["team-at-11"], w.workspace)
	return w
}

func TestInboxPersonalViewsSortsAndLabelsAgainstPostgres(t *testing.T) {
	w := seedInboxWorld(t)
	repo := &repository{db: w.db}

	paths := map[string]conversation.SearchEntriesInput{
		"workspace": {WorkspaceID: w.workspace, EntryType: shared.EntryTypeTelegram},
		"container": {CampaignID: w.account, EntryType: shared.EntryTypeTelegram},
	}
	cases := []struct {
		name   string
		adjust func(*conversation.SearchEntriesInput)
		want   []string
	}{
		{"surfaces without personal marks keep today's order", func(in *conversation.SearchEntriesInput) {},
			[]string{"customer-at-12", "team-at-11", "customer-at-10", "customer-at-9.5", "pinned-by-ana"}},
		{"ana's inbox puts her pin first and hides nothing", func(in *conversation.SearchEntriesInput) {
			in.Personal, in.PersonalUserID = personalinbox.ViewInbox, w.ana
		}, []string{"pinned-by-ana", "customer-at-12", "team-at-11", "customer-at-10", "customer-at-9.5"}},
		{"bia's inbox is unaffected by ana's marks", func(in *conversation.SearchEntriesInput) {
			in.Personal, in.PersonalUserID = personalinbox.ViewInbox, w.bia
		}, []string{"team-at-11", "customer-at-12", "customer-at-10", "customer-at-9.5", "pinned-by-ana"}},
		{"waiting puts whoever waits since the earliest unanswered message first after the pins", func(in *conversation.SearchEntriesInput) {
			in.Personal, in.PersonalUserID, in.SortOrder = personalinbox.ViewInbox, w.ana, conversation.SortOrderWaiting
		}, []string{"pinned-by-ana", "customer-at-10", "customer-at-9.5", "customer-at-12", "team-at-11"}},
		{"a finished conversation waits for nobody, so waiting falls back to recency", func(in *conversation.SearchEntriesInput) {
			in.ConversationStatus, in.SortOrder = conversation.ConversationStatusFinished, conversation.SortOrderWaiting
		}, []string{"finished-recently", "finished-long-ago-waiting"}},
		{"pinned shows only ana's pins", func(in *conversation.SearchEntriesInput) {
			in.Personal, in.PersonalUserID = personalinbox.ViewPinned, w.ana
		}, []string{"pinned-by-ana"}},
		{"the label filter keeps only labelled conversations", func(in *conversation.SearchEntriesInput) {
			in.Personal, in.PersonalUserID, in.LabelID = personalinbox.ViewInbox, w.ana, w.label
		}, []string{"customer-at-10", "customer-at-9.5"}},
		{"marks never widen what assignment hides", func(in *conversation.SearchEntriesInput) {
			in.Personal, in.PersonalUserID, in.AssignedUserID = personalinbox.ViewInbox, w.ana, w.ana
		}, []string{"pinned-by-ana", "customer-at-12", "team-at-11", "customer-at-10"}},
	}

	if err := w.db.Exec(`INSERT INTO inbox_assignments (id, workspace_id, entry_id, entry_type, assigned_user_id) VALUES (?, ?, ?, 'telegram', ?)`,
		uuid.NewString(), w.workspace, w.byName["customer-at-9.5"], w.bia).Error; err != nil {
		t.Fatal(err)
	}

	for path, base := range paths {
		for _, c := range cases {
			t.Run(path+"/"+c.name, func(t *testing.T) {
				in := base
				in.Page, in.PageSize = 1, 20
				c.adjust(&in)
				entries, total, err := repo.SearchEntriesWithMessages(in)
				if err != nil {
					t.Fatalf("search: %v", err)
				}
				if got := w.namesOf(entries); !reflect.DeepEqual(got, c.want) {
					t.Fatalf("order = %v\nwant    %v", got, c.want)
				}
				if total != int64(len(c.want)) {
					t.Fatalf("total = %d, want %d: the count must apply the same personal view as the page", total, len(c.want))
				}
			})
		}
	}
}

func TestInboxPagesCarryTheMessageClocksAgainstPostgres(t *testing.T) {
	w := seedInboxWorld(t)
	repo := &repository{db: w.db}
	id := w.byName["customer-at-10"]

	page, _, err := repo.GetEntriesWithMessages("", []string{id}, shared.EntryTypeTelegram, 1, 1, "")
	if err != nil || len(page) != 1 {
		t.Fatalf("GetEntriesWithMessages = %+v, %v", page, err)
	}
	single, err := repo.GetEntryLastMessage(id, shared.EntryTypeTelegram)
	if err != nil || single == nil {
		t.Fatalf("GetEntryLastMessage = %+v, %v", single, err)
	}
	for name, row := range map[string]conversation.EntryWithLastMessage{"list": page[0], "single": *single} {
		if row.LastCustomerMessageAt == nil || row.LastAgentMessageAt == nil || !row.LastCustomerMessageAt.After(*row.LastAgentMessageAt) {
			t.Errorf("%s clocks = %v / %v, want the customer after the team", name, row.LastCustomerMessageAt, row.LastAgentMessageAt)
		}
		if row.AwaitingReplySince == nil || !row.AwaitingReplySince.Before(*row.LastCustomerMessageAt) {
			t.Errorf("%s waits since %v, want the first unanswered message, before the last one at %v", name, row.AwaitingReplySince, row.LastCustomerMessageAt)
		}
	}
}
