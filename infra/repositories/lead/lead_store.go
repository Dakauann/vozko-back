package lead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/actor"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

const (
	eventBatchSize = 500

	recordColumnsSQL = "number = ?, name = ?, name_source = ?, nickname = ?, email = ?, birth_date = ?," +
		" owner_id = ?, owner_kind = ?, custom_fields = ?, whatsapp_opt_in_at = ?, whatsapp_opt_in_source = ?, whatsapp_opt_in_purpose = ?," +
		" opted_out_at = ?, opted_out_source = ?, blocked = ?, blocked_at = ?, blocked_by = ?, profile_picture_url = ?, age = ?"

	currentVersionSQL = "SELECT version FROM leads WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL"
)

var saveSQL = "UPDATE leads SET " + recordColumnsSQL + ", updated_at = ?, version = version + 1" +
	" WHERE id = ? AND workspace_id = ? AND version = ? AND deleted_at IS NULL" +
	" AND (leads.number IS NOT DISTINCT FROM ? OR NOT EXISTS (SELECT 1 FROM " + infracrmfilter.LeadEntriesSource() +
	" WHERE lead_entries.lead_id = leads.id)) RETURNING version"

func recordColumns(l *lead.Lead) ([]interface{}, error) {
	customFields, err := customFieldsColumn(l.CustomFields)
	if err != nil {
		return nil, err
	}
	ownerID, ownerKind := ownerColumns(l.Owner)
	optInAt, optInSource, optInPurpose := consentColumns(l.WhatsAppOptIn)
	return []interface{}{
		schema.OptionalText(l.Number), l.Name, schema.OptionalText(l.NameSource), schema.OptionalText(l.Nickname), schema.OptionalText(l.Email), birthDateColumn(l.BirthDate),
		ownerID, ownerKind, jsonArg(customFields), optInAt, optInSource, optInPurpose,
		l.OptedOutAt, optOutSourceColumn(l), l.Blocked, blockedAtColumn(l), l.BlockedBy, l.ProfilePictureURL, l.StoredAge,
	}, nil
}

func (r *repository) Insert(ctx context.Context, l *lead.Lead, events []recordevent.Event) error {
	if l == nil {
		return lead.ErrLeadRequired
	}
	if err := l.Validate(); err != nil {
		return err
	}
	if l.HasIdentity() {
		if _, err := r.FindByNumber(l.WorkspaceID, l.Number); err == nil {
			return lead.ErrLeadDuplicate
		} else if !errors.Is(err, lead.ErrLeadNotFound) {
			return err
		}
	}
	if l.ID == "" {
		l.ID = r.leadID()
	}
	l.EnsureCollections()
	now := time.Now().UTC()
	l.Version, l.CreatedAt, l.UpdatedAt = 1, now, now
	counts := lead.CountRelations(l.ID, l.Relations)
	l.RelativesCount, l.ReferredCount = counts.Relatives, counts.Referred
	added, err := r.newRelations(l, now)
	if err != nil {
		return err
	}
	row, err := toSchema(l)
	if err != nil {
		return err
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		change := relationChange{added: added}
		if counterparts := change.sidesOtherThan(l.ID); len(counterparts) > 0 {
			if err := lockLeads(tx, l.WorkspaceID, counterparts, counterparts); err != nil {
				return err
			}
		}
		res := tx.Clauses(onLiveIdentityConflictDoNothing()).Create(row)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return lead.ErrLeadDuplicate
		}
		if err := writePhones(tx, l, now, false, r.leadID); err != nil {
			return err
		}
		if err := writeAddresses(tx, l, now, false, r.leadID); err != nil {
			return err
		}
		if err := insertRelationRows(tx, l.WorkspaceID, added); err != nil {
			return err
		}
		if _, err := applyRelationChange(tx, l.WorkspaceID, change, l.ID, now); err != nil {
			return err
		}
		return insertEvents(tx, l.WorkspaceID, l.ID, events, now)
	})
	if err != nil {
		return err
	}
	r.agg.bump(l.WorkspaceID)
	return nil
}

func (r *repository) Save(ctx context.Context, l *lead.Lead, expectedVersion int64, events []recordevent.Event) error {
	if l == nil {
		return lead.ErrLeadRequired
	}
	if err := shared.RequireVersion(expectedVersion); err != nil {
		return err
	}
	if !l.IsAggregate() {
		return lead.ErrAggregateNotLoaded
	}
	if err := l.ValidateRecord(); err != nil {
		return err
	}
	columns, err := recordColumns(l)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	args := append(columns, now, l.ID, l.WorkspaceID, expectedVersion, schema.OptionalText(l.Number))

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var versions []int64
		if err := tx.Raw(saveSQL, args...).Scan(&versions).Error; err != nil {
			if database.IsUniqueViolation(err) {
				return lead.ErrLeadDuplicate
			}
			return err
		}
		if len(versions) == 0 {
			return whyNotSaved(tx, l, expectedVersion)
		}
		l.Version, l.UpdatedAt = versions[0], now
		if err := writePhones(tx, l, now, true, r.leadID); err != nil {
			return err
		}
		if err := writeAddresses(tx, l, now, true, r.leadID); err != nil {
			return err
		}
		return insertEvents(tx, l.WorkspaceID, l.ID, events, now)
	})
	if err != nil {
		return err
	}
	r.agg.bump(l.WorkspaceID)
	return nil
}

func whyNotSaved(tx *gorm.DB, l *lead.Lead, expectedVersion int64) error {
	var current []int64
	if err := tx.Raw(currentVersionSQL, l.ID, l.WorkspaceID).Scan(&current).Error; err != nil {
		return err
	}
	switch {
	case len(current) == 0:
		return lead.ErrLeadNotFound
	case current[0] != expectedVersion:
		return shared.ErrVersionConflict
	}
	return lead.ErrIdentityInUse
}

func eventRows(workspaceID, leadID string, events []recordevent.Event, at time.Time) ([]schema.LeadEvent, error) {
	rows := make([]schema.LeadEvent, 0, len(events))
	for _, e := range events {
		if e.Empty() {
			continue
		}
		changes, err := json.Marshal(e.Changes)
		if err != nil {
			return nil, fmt.Errorf("lead event changes: %w", err)
		}
		actorID, kind := actor.Split(e.Actor)
		rows = append(rows, schema.LeadEvent{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			LeadID:      leadID,
			ActorID:     schema.OptionalText(actorID),
			ActorKind:   string(kind),
			Kind:        string(e.Kind),
			Changes:     datatypes.JSON(changes),
			CreatedAt:   at,
		})
	}
	return rows, nil
}

func insertEventRows(tx *gorm.DB, rows []schema.LeadEvent) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(&rows, eventBatchSize).Error
}

func insertEvents(tx *gorm.DB, workspaceID, leadID string, events []recordevent.Event, at time.Time) error {
	rows, err := eventRows(workspaceID, leadID, events, at)
	if err != nil {
		return err
	}
	return insertEventRows(tx, rows)
}

type entryRefRow struct {
	EntryID   string
	EntryType string
}

func (r *repository) EntryRefs(ctx context.Context, workspaceID, leadID string) ([]shared.EntryRef, error) {
	workspaceID, leadID = strings.TrimSpace(workspaceID), strings.TrimSpace(leadID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if leadID == "" {
		return nil, lead.ErrLeadRequired
	}
	sql := "SELECT lead_entries.entry_id::text AS entry_id, lead_entries.entry_type AS entry_type FROM " +
		infracrmfilter.LeadEntriesSource() +
		" WHERE lead_entries.lead_id = ? AND EXISTS (SELECT 1 FROM leads WHERE leads.id = ? AND leads.workspace_id = ?)"
	var rows []entryRefRow
	if err := r.db.WithContext(ctx).Raw(sql, leadID, leadID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	refs := make([]shared.EntryRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, shared.EntryRef{EntryID: row.EntryID, EntryType: shared.EntryType(row.EntryType)})
	}
	return refs, nil
}

func jsonArg(raw datatypes.JSON) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

var leadOfEntrySQL = "SELECT lead_entries.lead_id::text FROM " + infracrmfilter.LeadEntriesSource() +
	" WHERE lead_entries.entry_id = ? AND lead_entries.entry_type = ?" +
	" AND EXISTS (SELECT 1 FROM leads WHERE leads.id = lead_entries.lead_id AND leads.workspace_id = ? AND leads.deleted_at IS NULL) LIMIT 1"

func (r *repository) LeadOfEntry(ctx context.Context, workspaceID string, ref shared.EntryRef) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", lead.ErrLeadWorkspaceRequired
	}
	if !ref.EntryType.Valid() || len(database.UUIDArray([]string{ref.EntryID})) == 0 {
		return "", lead.ErrLeadNotFound
	}
	var ids []string
	if err := r.db.WithContext(ctx).Raw(leadOfEntrySQL, strings.TrimSpace(ref.EntryID), string(ref.EntryType), workspaceID).Scan(&ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", lead.ErrLeadNotFound
	}
	return ids[0], nil
}
