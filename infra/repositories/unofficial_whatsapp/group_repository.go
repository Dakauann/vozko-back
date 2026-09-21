package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

type groupRepository struct {
	db *gorm.DB
}

func NewGroupRepository(db *gorm.DB) uw.GroupRepository {
	return &groupRepository{db: db}
}

func (r *groupRepository) Upsert(ctx context.Context, g *uw.Group) error {
	if g == nil {
		return uw.ErrGroupNotFound
	}
	g.Normalize()
	if err := g.Validate(); err != nil {
		return err
	}

	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record := &schema.UnofficialWhatsAppGroup{
			WorkspaceID:      g.WorkspaceID,
			InstanceID:       g.InstanceID,
			JID:              g.JID,
			Subject:          truncate(g.Subject, 255),
			Description:      truncate(g.Description, 1024),
			OwnerJID:         truncate(g.OwnerJID, 64),
			Announce:         g.Announce,
			Locked:           g.Locked,
			JoinApproval:     g.JoinApproval,
			Ephemeral:        g.Ephemeral,
			DisappearingSecs: g.DisappearingSecs,
			Community:        g.Community,
			WeAreAdmin:       g.WeAreAdmin,
			WeCanSend:        g.WeCanSend,
			ParticipantCount: g.ParticipantCount,
			GroupCreatedAt:   g.GroupCreatedAt,
			SyncedAt:         &now,
		}

		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}, {Name: "jid"}},
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"subject", "description", "owner_jid",
				"announce", "locked", "join_approval", "ephemeral",
				"disappearing_secs", "community",
				"we_are_admin", "we_can_send",
				"participant_count", "group_created_at",
				"synced_at", "updated_at",
			}),
		}).Create(record).Error
		if err != nil {
			return err
		}

		var stored schema.UnofficialWhatsAppGroup
		if err := tx.First(&stored, "instance_id = ? AND jid = ?", g.InstanceID, g.JID).Error; err != nil {
			return err
		}
		g.ID = stored.ID
		g.SyncedAt = stored.SyncedAt

		if len(g.Participants) == 0 {
			return nil
		}

		if err := tx.Where("group_id = ?", stored.ID).
			Delete(&schema.UnofficialWhatsAppGroupParticipant{}).Error; err != nil {
			return err
		}

		rows := make([]schema.UnofficialWhatsAppGroupParticipant, 0, len(g.Participants))
		for _, p := range g.Participants {
			if strings.TrimSpace(p.JID) == "" {
				continue
			}
			rows = append(rows, schema.UnofficialWhatsAppGroupParticipant{
				GroupID:     stored.ID,
				JID:         truncate(p.JID, 64),
				LID:         truncate(p.LID, 64),
				PhoneNumber: truncate(p.PhoneNumber, 32),
				DisplayName: truncate(p.DisplayName, 255),
				Role:        string(p.Role),
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "group_id"}, {Name: "jid"}},
			DoNothing: true,
		}).Create(&rows).Error
	})
}

func (r *groupRepository) FindByJID(ctx context.Context, instanceID, jid string) (*uw.Group, error) {
	var record schema.UnofficialWhatsAppGroup
	err := r.db.WithContext(ctx).
		First(&record, "instance_id = ? AND jid = ?", instanceID, jid).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrGroupNotFound
		}
		return nil, err
	}
	return r.hydrate(ctx, &record)
}

func (r *groupRepository) FindByID(ctx context.Context, id string) (*uw.Group, error) {
	var record schema.UnofficialWhatsAppGroup
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uw.ErrGroupNotFound
		}
		return nil, err
	}
	return r.hydrate(ctx, &record)
}

func (r *groupRepository) hydrate(ctx context.Context, record *schema.UnofficialWhatsAppGroup) (*uw.Group, error) {
	participants, err := r.participantRows(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	return toGroupDomain(record, participants), nil
}

func (r *groupRepository) ListByInstance(ctx context.Context, instanceID string) ([]*uw.Group, error) {
	var records []schema.UnofficialWhatsAppGroup
	err := r.db.WithContext(ctx).
		Where("instance_id = ?", instanceID).
		Order("subject ASC").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	out := make([]*uw.Group, 0, len(records))
	for i := range records {
		out = append(out, toGroupDomain(&records[i], nil))
	}
	return out, nil
}

func (r *groupRepository) Participants(ctx context.Context, groupID string) ([]uw.GroupParticipant, error) {
	rows, err := r.participantRows(ctx, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]uw.GroupParticipant, 0, len(rows))
	for i := range rows {
		out = append(out, toGroupParticipantDomain(&rows[i]))
	}
	return out, nil
}

func (r *groupRepository) participantRows(ctx context.Context, groupID string) ([]schema.UnofficialWhatsAppGroupParticipant, error) {
	var rows []schema.UnofficialWhatsAppGroupParticipant
	err := r.db.WithContext(ctx).
		Where("group_id = ?", groupID).
		Order(`CASE role WHEN 'superadmin' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, display_name ASC, phone_number ASC`).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *groupRepository) MarkStale(ctx context.Context, instanceID, jid string, at time.Time) error {
	if strings.TrimSpace(jid) == "" {
		return nil
	}
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroup{}).
		Where("instance_id = ? AND jid = ?", instanceID, jid).
		Update("stale_at", at).Error
}

func (r *groupRepository) LinkParticipantContacts(ctx context.Context, groupID, instanceID string) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE unofficial_whatsapp_group_participants AS p
		SET contact_id = c.id
		FROM unofficial_whatsapp_contacts AS c
		WHERE p.group_id = ?
		  AND c.instance_id = ?
		  AND c.is_group = false
		  AND c.deleted_at IS NULL
		  AND c.phone_number <> ''
		  AND c.phone_number = p.phone_number
		  AND (p.contact_id IS NULL OR p.contact_id <> c.id)
	`, groupID, instanceID).Error
}

func (r *groupRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).
			Delete(&schema.UnofficialWhatsAppGroupParticipant{}).Error; err != nil {
			return err
		}
		return tx.Delete(&schema.UnofficialWhatsAppGroup{}, "id = ?", id).Error
	})
}
