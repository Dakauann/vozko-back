package database

import (
	"strings"
	"testing"
)

func TestMercadoLivreUniquenessIsOnLiveRows(t *testing.T) {
	cases := map[string][]string{
		"ux_ml_account_live_user":         {"UNIQUE", "mercadolivre_accounts (ml_user_id)", "deleted_at IS NULL"},
		"ux_ml_contact_account_user":      {"UNIQUE", "mercadolivre_contacts (account_id, ml_user_id)", "deleted_at IS NULL"},
		"ux_ml_conversation_account_pack": {"UNIQUE", "mercadolivre_conversations (account_id, pack_key)", "deleted_at IS NULL"},
	}
	for name, fragments := range cases {
		sql, ok := PerformanceIndexSQL(name)
		if !ok {
			t.Errorf("%s is not declared", name)
			continue
		}
		flat := strings.Join(strings.Fields(sql), " ")
		for _, f := range fragments {
			if !strings.Contains(flat, f) {
				t.Errorf("%s misses %q: %s", name, f, flat)
			}
		}
	}
}

func TestEveryMercadoLivreQueryPathHasAnIndex(t *testing.T) {
	cases := map[string][]string{
		"idx_ml_acct_workspace_created":    {"mercadolivre_accounts (workspace_id, created_at DESC)", "deleted_at IS NULL"},
		"idx_ml_acct_refresh_due":          {"mercadolivre_accounts (access_expires_at, id)", "status = 'CONNECTED'", "deleted_at IS NULL"},
		"idx_ml_acct_reconcile_due":        {"mercadolivre_accounts (last_reconciled_at, id)", "status = 'CONNECTED'", "deleted_at IS NULL"},
		"idx_ml_acct_health_due":           {"mercadolivre_accounts (last_health_check_at, id)", "deleted_at IS NULL"},
		"idx_ml_acct_purge_due":            {"mercadolivre_accounts (purge_after, id)", "purge_after IS NOT NULL"},
		"idx_ml_conv_workspace_lastmsg":    {"mercadolivre_conversations (workspace_id, last_message_at DESC)", "deleted_at IS NULL"},
		"idx_ml_conv_account_lastmsg":      {"mercadolivre_conversations (account_id, last_message_at DESC)", "deleted_at IS NULL"},
		"idx_ml_conv_account_created":      {"mercadolivre_conversations (account_id, created_at DESC, id DESC)", "deleted_at IS NULL"},
		"idx_ml_conv_account_shipment":     {"mercadolivre_conversations (account_id, shipment_id)", "shipment_id <> ''", "deleted_at IS NULL"},
		"idx_ml_contact_questions_blocked": {"mercadolivre_contacts (account_id, questions_blocked_at)", "questions_blocked_at IS NOT NULL", "deleted_at IS NULL"},
	}
	for name, fragments := range cases {
		sql, ok := PerformanceIndexSQL(name)
		if !ok {
			t.Errorf("%s is not declared", name)
			continue
		}
		flat := strings.Join(strings.Fields(sql), " ")
		for _, f := range fragments {
			if !strings.Contains(flat, f) {
				t.Errorf("%s misses %q: %s", name, f, flat)
			}
		}
	}
}
