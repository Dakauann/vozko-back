package lead

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	findRelationSQL = "SELECT * FROM lead_relations WHERE workspace_id = ? AND id = ?"

	deleteRelationSQL = "DELETE FROM lead_relations WHERE workspace_id = ? AND id = ? RETURNING id"

	relativesPageSQL = "SELECT p.id, p.lead_id, p.other_lead_id, p.kind, p.created_by, p.created_at," +
		" o.id AS relative_id, o.name AS relative_name, o.number AS relative_number" +
		" FROM (%s UNION ALL %s) p JOIN leads o ON o.id = p.relative_id ORDER BY p.created_at, p.id LIMIT ?"
)

func (r *repository) AddRelation(ctx context.Context, workspaceID string, rel lead.Relation) (lead.RelationWrite, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return lead.RelationWrite{}, lead.ErrLeadWorkspaceRequired
	}
	if _, err := lead.NewRelation(rel.LeadID, rel.OtherLeadID, rel.Kind, rel.CreatedBy); err != nil {
		return lead.RelationWrite{}, err
	}
	if !rel.Kind.Canonical() {
		return lead.RelationWrite{}, lead.ErrRelationKindInvalid
	}
	now := time.Now().UTC()
	rel.ID, rel.CreatedAt = r.leadID(), now
	var tallies map[string]lead.RelationTally
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sides := []string{rel.LeadID, rel.OtherLeadID}
		if err := lockLeads(tx, workspaceID, sides, sides); err != nil {
			return err
		}
		if err := insertRelationRows(tx, workspaceID, []lead.Relation{rel}); err != nil {
			return err
		}
		var err error
		tallies, err = applyRelationChange(tx, workspaceID, relationChange{added: []lead.Relation{rel}}, "", now)
		return err
	})
	if err != nil {
		return lead.RelationWrite{}, err
	}
	r.agg.bump(workspaceID)
	return lead.RelationWrite{Relation: rel, Leads: tallies}, nil
}

func (r *repository) RemoveRelation(ctx context.Context, workspaceID, relationID, actorID string) (lead.RelationWrite, error) {
	workspaceID, relationID = strings.TrimSpace(workspaceID), strings.TrimSpace(relationID)
	if workspaceID == "" {
		return lead.RelationWrite{}, lead.ErrLeadWorkspaceRequired
	}
	if len(database.UUIDArray([]string{relationID})) == 0 {
		return lead.RelationWrite{}, lead.ErrRelationNotFound
	}
	now := time.Now().UTC()
	var write lead.RelationWrite
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored []schema.LeadRelation
		if err := tx.Raw(findRelationSQL, workspaceID, relationID).Scan(&stored).Error; err != nil {
			return err
		}
		if len(stored) == 0 {
			return lead.ErrRelationNotFound
		}
		rel := relationOf(stored[0])
		if err := lockLeads(tx, workspaceID, []string{rel.LeadID, rel.OtherLeadID}, nil); err != nil {
			return err
		}
		var deleted []string
		if err := tx.Raw(deleteRelationSQL, workspaceID, relationID).Scan(&deleted).Error; err != nil {
			return err
		}
		if len(deleted) == 0 {
			return lead.ErrRelationNotFound
		}
		tallies, err := applyRelationChange(tx, workspaceID, relationChange{removed: []lead.Relation{rel}, removedBy: actorID}, "", now)
		if err != nil {
			return err
		}
		write = lead.RelationWrite{Relation: rel, Leads: tallies}
		return nil
	})
	if err != nil {
		return lead.RelationWrite{}, err
	}
	r.agg.bump(workspaceID)
	return write, nil
}

type relativeRow struct {
	ID             string
	LeadID         string
	OtherLeadID    string
	Kind           string
	CreatedBy      schema.OptionalText
	CreatedAt      time.Time
	RelativeID     string
	RelativeName   string
	RelativeNumber schema.OptionalText
}

func relativesBranch(own, other string, q lead.RelativesQuery, cursor *lead.RelativesCursor, workspaceID string) (string, []interface{}) {
	sql := "(SELECT r.id, r.lead_id, r.other_lead_id, r.kind, r.created_by, r.created_at, r." + other + " AS relative_id" +
		" FROM lead_relations r WHERE r.workspace_id = ? AND r." + own + " = ?"
	args := []interface{}{workspaceID, q.LeadID}
	if q.Dimension != "" {
		sql += " AND r.dimension = ?"
		args = append(args, string(q.Dimension))
	}
	if cursor != nil {
		sql += " AND (r.created_at, r.id) > (?::timestamptz, ?::uuid)"
		args = append(args, cursor.CreatedAt, cursor.RelationID)
	}
	sql += " AND EXISTS (SELECT 1 FROM leads o WHERE o.id = r." + other + " AND o.deleted_at IS NULL)" +
		" ORDER BY r.created_at, r.id LIMIT ?)"
	return sql, append(args, q.Limit+1)
}

func relativesQuery(workspaceID string, q lead.RelativesQuery, cursor *lead.RelativesCursor) (string, []interface{}) {
	held, heldArgs := relativesBranch("lead_id", "other_lead_id", q, cursor, workspaceID)
	holding, holdingArgs := relativesBranch("other_lead_id", "lead_id", q, cursor, workspaceID)
	args := append(append(heldArgs, holdingArgs...), q.Limit+1)
	return fmt.Sprintf(relativesPageSQL, held, holding), args
}

func (r *repository) ListRelatives(ctx context.Context, workspaceID string, q lead.RelativesQuery) (lead.RelativesPage, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return lead.RelativesPage{}, lead.ErrLeadWorkspaceRequired
	}
	q, err := q.Normalize()
	if err != nil {
		return lead.RelativesPage{}, err
	}
	if len(database.UUIDArray([]string{q.LeadID})) == 0 {
		return lead.RelativesPage{}, lead.ErrLeadNotFound
	}
	after, paged, err := q.Cursor()
	if err != nil {
		return lead.RelativesPage{}, err
	}
	var cursor *lead.RelativesCursor
	if paged {
		cursor = &after
	}
	sql, args := relativesQuery(workspaceID, q, cursor)
	var rows []relativeRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return lead.RelativesPage{}, err
	}
	page := lead.RelativesPage{Relatives: make([]lead.Relative, 0, len(rows))}
	for i, row := range rows {
		if i == q.Limit {
			last := page.Relatives[len(page.Relatives)-1].Relation
			page.Next = lead.RelativesCursor{CreatedAt: last.CreatedAt, RelationID: last.ID}.Encode()
			break
		}
		page.Relatives = append(page.Relatives, lead.Relative{
			Relation: lead.Relation{
				ID: row.ID, LeadID: row.LeadID, OtherLeadID: row.OtherLeadID, Kind: lead.RelationKind(row.Kind),
				CreatedBy: string(row.CreatedBy), CreatedAt: row.CreatedAt,
			},
			Lead: &lead.Lead{ID: row.RelativeID, WorkspaceID: workspaceID, Name: row.RelativeName, Number: string(row.RelativeNumber)},
		})
	}
	return page, nil
}
