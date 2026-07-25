package analytics_repository

import "strings"

func subscriptionScopeClause(column string, planID, billingCycle, status *string) (string, []interface{}) {
	if planID == nil || strings.TrimSpace(*planID) == "" {
		return "", nil
	}
	sub := "SELECT workspace_id FROM workspace_subscriptions WHERE plan_definition_id = ?"
	args := []interface{}{strings.TrimSpace(*planID)}
	if billingCycle != nil && strings.TrimSpace(*billingCycle) != "" {
		sub += " AND billing_cycle = ?"
		args = append(args, strings.TrimSpace(*billingCycle))
	}
	if status != nil && strings.TrimSpace(*status) != "" {
		sub += " AND status = ?"
		args = append(args, strings.TrimSpace(*status))
	} else {
		sub += " AND status = 'active'"
	}
	return column + " IN (" + sub + ")", args
}
