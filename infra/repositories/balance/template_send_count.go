package balance_repository

import "vozko/domain/balance"

const netTemplateSendExpr = `CASE WHEN bt.type = 'debit' AND bt.is_refund = false THEN 1 WHEN bt.type = 'credit' AND bt.is_refund = true THEN -1 ELSE 0 END`

const chargeOwnerKey = `CASE WHEN regexp_replace(bt.reference_id, '^refund:', '') LIKE 'waba:%' THEN regexp_replace(bt.reference_id, '^refund:', '') ELSE split_part(regexp_replace(bt.reference_id, '^refund:', ''), ':', 1) END`

const templateSendRowsFilter = `((bt.type = 'debit' AND bt.is_refund = false) OR (bt.type = 'credit' AND bt.is_refund = true))`

func netTemplateSendsSinceSQL(workspaceColumn string) string {
	return `SELECT COALESCE(SUM(` + netTemplateSendExpr + `), 0)::bigint FROM balance_transactions bt WHERE bt.workspace_id = ` + workspaceColumn +
		` AND bt.service_type = '` + string(balance.ServiceWhatsAppCampaign) + `' AND bt.created_at >= ? AND ` + templateSendRowsFilter
}
