package entrypurge_repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	domain "vozko/domain/entrypurge"
	"vozko/domain/shared"
)

type keyedTable struct {
	table      string
	idColumn   string
	typeColumn string
	dependents []string
}

func (k keyedTable) statements() []string {
	out := append(make([]string, 0, len(k.dependents)+1), k.dependents...)
	return append(out, fmt.Sprintf("DELETE FROM %s WHERE %s = ? AND %s IN ?", k.table, k.typeColumn, k.idColumn))
}

func entry(table string) keyedTable {
	return keyedTable{table: table, idColumn: "entry_id", typeColumn: "entry_type"}
}

var entryTables = []keyedTable{
	entry("conversation_messages"),
	entry("conversation_media"),
	entry("conversation_events"),
	entry("conversation_notes"),
	entry("conversation_personal_marks"),
	entry("conversation_delegations"),
	entry("conversation_ad_origins"),
	entry("conversation_live_reads"),
	entry("live_decision_records"),
	entry("ai_attendance_sessions"),
	entry("ai_tool_calls"),
	entry("workflow_webhook_deliveries"),
	entry("assignment_history"),
	entry("inbox_assignments"),
	entry("entry_labels"),
	entry("entry_stages"),
	entry("opportunity_conversations"),
	entry("scheduled_messages"),
	entry("lead_exit_runs"),
	entry("whatsapp_form_sends"),
	entry("whatsapp_form_responses"),
	entry("google_sheet_rows"),
	{table: "lead_memories", idColumn: "source_entry_id", typeColumn: "source_entry_type"},
	{
		table: "workflow_runs", idColumn: "entry_id", typeColumn: "entry_type",
		dependents: []string{"DELETE FROM workflow_run_logs WHERE run_id IN (SELECT id FROM workflow_runs WHERE entry_type = ? AND entry_id IN ?)"},
	},
}

var contactTables = []keyedTable{
	{table: "customer_portfolio_entries", idColumn: "contact_id", typeColumn: "contact_type"},
}

const mediaURLsSQL = "SELECT url FROM conversation_media WHERE entry_type = ? AND entry_id IN ?"

type store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) domain.Store {
	if db == nil {
		panic("entry purge store: the database is required")
	}
	return &store{db: db}
}

func (s *store) MediaURLs(ctx context.Context, entryType shared.EntryType, entryIDs []string) ([]string, error) {
	if len(entryIDs) == 0 {
		return nil, nil
	}
	var urls []string
	if err := s.db.WithContext(ctx).Raw(mediaURLsSQL, string(entryType), entryIDs).Scan(&urls).Error; err != nil {
		return nil, err
	}
	return urls, nil
}

func (s *store) DeleteEntries(ctx context.Context, entryType shared.EntryType, entryIDs []string) error {
	return s.deleteFrom(ctx, entryTables, entryType, entryIDs)
}

func (s *store) DeleteContacts(ctx context.Context, contactType shared.EntryType, contactIDs []string) error {
	return s.deleteFrom(ctx, contactTables, contactType, contactIDs)
}

func (s *store) deleteFrom(ctx context.Context, tables []keyedTable, kind shared.EntryType, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > domain.MaxBatch {
		return domain.ErrBatchTooLarge
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, tbl := range tables {
			for _, stmt := range tbl.statements() {
				if err := tx.Exec(stmt, string(kind), ids).Error; err != nil {
					return fmt.Errorf("entry purge: %s: %w", tbl.table, err)
				}
			}
		}
		return nil
	})
}
