package opportunity_repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
	crmfiltersql "vozko/infra/repositories/crmfilter"
)

var ErrLeadDealsQueryInvalid = errors.New("opportunity: the deals of a lead need a workspace, a lead, a page size and a cursor this server gave")

func NewLeadDealReader(db *gorm.DB) opportunity.LeadDealReader {
	return &repository{db: db}
}

func DealScopeCondition(alias string, scope opportunity.DealScope) (string, []interface{}) {
	if !scope.Restrict {
		return "", nil
	}
	var conds []string
	var args []interface{}
	if scope.AssigneeOverride != "" {
		conds = append(conds, alias+".owner_id = ?")
		args = append(args, scope.AssigneeOverride)
	}
	if len(scope.DepartmentIDs) > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM workspace_department_members wdm "+
			"JOIN workspace_members wm ON wm.id = wdm.member_id "+
			"WHERE wm.user_id = "+alias+".owner_id AND wdm.department_id = ANY(?::uuid[]))")
		args = append(args, pq.Array(scope.DepartmentIDs))
	}
	if len(conds) == 0 {
		return "1 = 0", nil
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

func leadDealsMembership(alias, workspaceID, leadID string) (string, []interface{}) {
	return alias + ".id IN (SELECT od.id FROM opportunities od WHERE od.workspace_id = ? AND od.lead_id = ? AND od.deleted_at IS NULL" +
			" UNION SELECT oc.opportunity_id FROM opportunity_conversations oc JOIN " + crmfiltersql.LeadEntriesSource() +
			" ON lead_entries.entry_id = oc.entry_id AND lead_entries.entry_type = oc.entry_type WHERE lead_entries.lead_id = ?)",
		[]interface{}{workspaceID, leadID, leadID}
}

func LeadDealsCondition(alias, workspaceID, leadID string, scope opportunity.DealScope) (string, []interface{}) {
	membership, membershipArgs := leadDealsMembership(alias, workspaceID, leadID)
	where := alias + ".workspace_id = ? AND " + alias + ".deleted_at IS NULL AND " + membership
	args := append([]interface{}{workspaceID}, membershipArgs...)
	if cond, condArgs := DealScopeCondition(alias, scope); cond != "" {
		where += " AND " + cond
		args = append(args, condArgs...)
	}
	return where, args
}

func JoinAuthor(id, kind string) string {
	return joinAuthor(id, kind)
}

func dealsOfLeadSQL(condition string, paged bool) string {
	sql := "SELECT " + oppAlias + ".* FROM opportunities " + oppAlias + " WHERE " + condition
	if paged {
		sql += " AND (" + oppAlias + ".created_at, " + oppAlias + ".id) < (?, ?::uuid)"
	}
	return sql + " ORDER BY " + oppAlias + ".created_at DESC, " + oppAlias + ".id DESC LIMIT ?"
}

func NewLeadDealCounter(db *gorm.DB) opportunity.LeadDealCounter {
	return &repository{db: db}
}

func countDealsOfLeadSQL(condition string) string {
	return "SELECT count(*) FROM opportunities " + oppAlias + " WHERE " + condition
}

func (r *repository) CountDealsOfLead(ctx context.Context, workspaceID, leadID string, scope opportunity.DealScope) (int, error) {
	workspaceID, leadID = strings.TrimSpace(workspaceID), strings.TrimSpace(leadID)
	if workspaceID == "" || leadID == "" {
		return 0, ErrLeadDealsQueryInvalid
	}
	condition, args := LeadDealsCondition(oppAlias, workspaceID, leadID, scope)
	var count int
	if err := r.db.WithContext(ctx).Raw(countDealsOfLeadSQL(condition), args...).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *repository) DealsOfLead(ctx context.Context, q opportunity.LeadDealsQuery) ([]*opportunity.Opportunity, error) {
	workspaceID, leadID := strings.TrimSpace(q.WorkspaceID), strings.TrimSpace(q.LeadID)
	if workspaceID == "" || leadID == "" || q.Limit <= 0 {
		return nil, ErrLeadDealsQueryInvalid
	}
	if q.Before != nil {
		if _, err := uuid.Parse(q.Before.ID); err != nil {
			return nil, ErrLeadDealsQueryInvalid
		}
	}
	condition, args := LeadDealsCondition(oppAlias, workspaceID, leadID, q.Scope)
	if q.Before != nil {
		args = append(args, q.Before.At, q.Before.ID)
	}
	args = append(args, q.Limit)
	var rows []schema.Opportunity
	if err := r.db.WithContext(ctx).Raw(dealsOfLeadSQL(condition, q.Before != nil), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*opportunity.Opportunity, 0, len(rows))
	for i := range rows {
		o, err := mapToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}
