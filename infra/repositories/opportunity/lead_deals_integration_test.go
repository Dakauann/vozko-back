package opportunity_repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/opportunity"
)

func leadEntriesTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	ddl := []string{`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, lead_id uuid, deleted_at timestamptz)`}
	for _, channel := range []struct{ conversations, contacts, contactColumn string }{
		{"unofficial_whatsapp_conversations", "unofficial_whatsapp_contacts", "contact_id"},
		{"telegram_conversations", "telegram_contacts", "contact_id"},
		{"instagram_conversations", "instagram_contacts", "contact_id"},
		{"facebook_conversations", "facebook_contacts", "contact_id"},
		{"webchat_conversations", "webchat_visitors", "visitor_id"},
	} {
		ddl = append(ddl,
			`CREATE TABLE `+channel.contacts+` (id uuid PRIMARY KEY, lead_id uuid, deleted_at timestamptz)`,
			`CREATE TABLE `+channel.conversations+` (id uuid PRIMARY KEY, `+channel.contactColumn+` uuid NOT NULL, deleted_at timestamptz)`)
	}
	for _, statement := range ddl {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
}

func TestIntegrationTheDealCountOfALeadCountsItsOwnAndLinkedDealsInsideTheScope(t *testing.T) {
	db := integrationDB(t)
	leadEntriesTables(t, db)
	repo := NewRepository(db)
	ws, leadID, viewer, colleague := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	entry, contact, conversation := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, seed := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO whatsapp_campaign_entries (id, lead_id) VALUES (?, ?)`, []any{entry, leadID}},
		{`INSERT INTO telegram_contacts (id, lead_id) VALUES (?, ?)`, []any{contact, leadID}},
		{`INSERT INTO telegram_conversations (id, contact_id) VALUES (?, ?)`, []any{conversation, contact}},
	} {
		if err := db.Exec(seed.sql, seed.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	deal := func(owner, lead string, links ...opportunity.ConversationLink) string {
		d := newDeal(ws, uuid.NewString(), owner)
		d.LeadID = lead
		for i := range links {
			links[i].OpportunityID = d.ID
		}
		if err := repo.Create(d, links, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return d.ID
	}
	deal(viewer, leadID, opportunity.ConversationLink{EntryID: entry, EntryType: "whatsapp"})
	deal(viewer, "", opportunity.ConversationLink{EntryID: entry, EntryType: "whatsapp"})
	deal(viewer, "", opportunity.ConversationLink{EntryID: conversation, EntryType: "telegram"})
	deleted := deal(viewer, leadID)
	deal(colleague, leadID)
	deal(viewer, uuid.NewString())
	if err := db.Exec(`UPDATE opportunities SET deleted_at = now() WHERE id = ?`, deleted).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	other := newDeal(uuid.NewString(), uuid.NewString(), viewer)
	other.LeadID = leadID
	if err := repo.Create(other, nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	counter := NewLeadDealCounter(db)
	cases := []struct {
		name  string
		scope opportunity.DealScope
		want  int
	}{
		{"inside the viewer's own deals", opportunity.DealScope{Restrict: true, AssigneeOverride: viewer}, 3},
		{"every deal of the lead", opportunity.DealScope{}, 4},
		{"a scope that sees nothing", opportunity.DealScope{Restrict: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := counter.CountDealsOfLead(context.Background(), ws, leadID, tc.scope)
			if err != nil || got != tc.want {
				t.Fatalf("count = %d, %v, want %d", got, err, tc.want)
			}
		})
	}
}
