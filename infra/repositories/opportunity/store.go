package opportunity_repository

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/actor"
	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
)

func (r *repository) Create(o *opportunity.Opportunity, links []opportunity.ConversationLink, events []opportunity.Event) error {
	row, err := mapToSchema(o)
	if err != nil {
		return err
	}
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		for _, link := range links {
			if err := insertLink(tx, link); err != nil {
				return err
			}
		}
		return insertEvents(tx, events)
	})
	if err != nil {
		return err
	}
	o.ID = row.ID
	o.CreatedAt = row.CreatedAt
	o.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repository) Update(o *opportunity.Opportunity, events []opportunity.Event) error {
	customJSON, err := marshalCustomFields(o.CustomFields)
	if err != nil {
		return err
	}
	ownerID, ownerKind := actor.Split(o.OwnerID)
	closedByID, closedByKind := splitAuthor(o.ClosedBy)
	update := map[string]interface{}{
		"lead_id":        nullableUUID(o.LeadID),
		"pipeline_id":    o.PipelineID,
		"stage_id":       o.StageID,
		"owner_id":       nullableUUID(ownerID),
		"owner_kind":     string(ownerKind),
		"closed_by_id":   nullableUUID(closedByID),
		"closed_by_kind": closedByKind,
		"carteira_id":    nullableUUID(o.CarteiraID),
		"title":          o.Title,
		"value_cents":    o.ValueCents,
		"currency":       o.Currency,
		"status":         string(o.Status),
		"lost_reason_id": nullableUUID(o.LostReasonID),
		"source":         o.Source,
		"close_date":     o.CloseDate,
		"custom_fields":  customJSON,
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&schema.Opportunity{}).
			Where("id = ? AND workspace_id = ?", o.ID, o.WorkspaceID).
			Updates(update)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return opportunity.ErrNotFound
		}
		return insertEvents(tx, events)
	})
}

func (r *repository) Link(link opportunity.ConversationLink, events []opportunity.Event) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := insertLink(tx, link); err != nil {
			return err
		}
		return insertEvents(tx, events)
	})
}

func (r *repository) OpenForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error) {
	return r.firstEntryDeal(r.entryDeals(workspaceID, pipelineID, entryID, entryType).
		Where("o.status = ?", string(opportunity.StatusOpen)))
}

func (r *repository) CurrentForEntry(workspaceID, pipelineID, entryID, entryType string) (*opportunity.Opportunity, error) {
	return r.firstEntryDeal(r.entryDeals(workspaceID, pipelineID, entryID, entryType).
		Order(clause.Expr{SQL: "o.status = ? DESC", Vars: []interface{}{string(opportunity.StatusOpen)}}))
}

func (r *repository) entryDeals(workspaceID, pipelineID, entryID, entryType string) *gorm.DB {
	return r.db.Table("opportunities AS o").
		Select("o.*").
		Joins("JOIN opportunity_conversations oc ON oc.opportunity_id = o.id").
		Where("o.deleted_at IS NULL AND o.workspace_id = ? AND o.pipeline_id = ?", workspaceID, pipelineID).
		Where("oc.entry_id = ? AND oc.entry_type = ?", entryID, entryType)
}

func (r *repository) firstEntryDeal(query *gorm.DB) (*opportunity.Opportunity, error) {
	var row schema.Opportunity
	err := query.Order("o.created_at DESC").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, opportunity.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapToDomain(&row)
}

func (r *repository) ListEvents(workspaceID, opportunityID string) ([]opportunity.Event, error) {
	var rows []schema.OpportunityEvent
	if err := r.db.
		Where("workspace_id = ? AND opportunity_id = ?", workspaceID, opportunityID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]opportunity.Event, 0, len(rows))
	for i := range rows {
		event, err := mapEventToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (r *repository) WithEntryLock(workspaceID, entryID, entryType string, fn func(opportunity.Store) error) error {
	key := workspaceID + ":" + entryType + ":" + entryID
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}
		return fn(&repository{db: tx})
	})
}

func insertLink(tx *gorm.DB, link opportunity.ConversationLink) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "opportunity_id"}, {Name: "entry_id"}, {Name: "entry_type"}},
		DoNothing: true,
	}).Create(&schema.OpportunityConversation{
		ID:            uuid.New().String(),
		OpportunityID: link.OpportunityID,
		EntryID:       link.EntryID,
		EntryType:     link.EntryType,
	}).Error
}

func insertEvents(tx *gorm.DB, events []opportunity.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]schema.OpportunityEvent, 0, len(events))
	for _, event := range events {
		row, err := mapEventToSchema(event)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	return tx.Create(&rows).Error
}

func mapEventToSchema(e opportunity.Event) (schema.OpportunityEvent, error) {
	var details datatypes.JSON
	if len(e.Details) > 0 {
		raw, err := json.Marshal(e.Details)
		if err != nil {
			return schema.OpportunityEvent{}, err
		}
		details = raw
	}
	actorID, actorKind := actor.Split(e.ActorID)
	id := e.ID
	if id == "" {
		id = uuid.New().String()
	}
	return schema.OpportunityEvent{
		ID:            id,
		WorkspaceID:   e.WorkspaceID,
		OpportunityID: e.OpportunityID,
		Type:          string(e.Type),
		ActorID:       actorID,
		ActorKind:     string(actorKind),
		FromStageID:   e.FromStageID,
		ToStageID:     e.ToStageID,
		ValueCents:    e.ValueCents,
		Currency:      e.Currency,
		Details:       details,
		CreatedAt:     e.CreatedAt,
	}, nil
}

func mapEventToDomain(row *schema.OpportunityEvent) (opportunity.Event, error) {
	var details map[string]any
	if len(row.Details) > 0 {
		if err := json.Unmarshal(row.Details, &details); err != nil {
			return opportunity.Event{}, err
		}
	}
	return opportunity.Event{
		ID:            row.ID,
		WorkspaceID:   row.WorkspaceID,
		OpportunityID: row.OpportunityID,
		Type:          opportunity.EventType(row.Type),
		ActorID:       actor.Join(row.ActorID, actor.Kind(row.ActorKind)),
		FromStageID:   row.FromStageID,
		ToStageID:     row.ToStageID,
		ValueCents:    row.ValueCents,
		Currency:      row.Currency,
		Details:       details,
		CreatedAt:     row.CreatedAt,
	}, nil
}

func splitAuthor(id string) (string, string) {
	if strings.TrimSpace(id) == "" {
		return "", ""
	}
	stored, kind := actor.Split(id)
	return stored, string(kind)
}

func joinAuthor(id, kind string) string {
	if kind == "" {
		return ""
	}
	return actor.Join(id, actor.Kind(kind))
}
