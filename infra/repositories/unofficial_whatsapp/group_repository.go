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

// NewGroupRepository builds the group read-model repository.
func NewGroupRepository(db *gorm.DB) uw.GroupRepository {
	return &groupRepository{db: db}
}

// Upsert writes one synced group and replaces its roster.
//
// Both halves happen in one transaction, and the roster is REPLACED rather than
// merged. A merge would need a local diff to know who left, and there is no
// such thing: absence from the provider's list is the only evidence a member is
// gone. A roster that can only grow is worse than none, because it reads as
// authoritative while quietly listing people who were removed months ago.
//
// SyncedAt is stamped here and nowhere else, so "we have confirmed this with the
// provider" is written by exactly the code path that did.
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
			// Partial unique index: the predicate must be repeated or Postgres
			// refuses to use it as the conflict arbiter (42P10).
			TargetWhere: clause.Where{
				Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}},
			},
			// StaleAt is deliberately absent from the update list: a webhook that
			// fires WHILE a sync is in flight must not have its invalidation
			// erased by that sync's result, or the change it announced is lost
			// until the TTL expires. Leaving it alone makes NeedsSync compare it
			// against the new SyncedAt, which is exactly the right question.
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

		// A read that returned no roster is not a claim that the group is
		// empty: ListGroups is called with participants omitted precisely
		// because a workspace in 200 groups does not need 200 rosters. Wiping
		// what an earlier full read stored would be inventing a fact.
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

// hydrate loads a group with its roster. The roster is a second query rather
// than a join: a group row is one row and its roster is hundreds, and joining
// them multiplies every group column by the member count for no benefit.
func (r *groupRepository) hydrate(ctx context.Context, record *schema.UnofficialWhatsAppGroup) (*uw.Group, error) {
	participants, err := r.participantRows(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	return toGroupDomain(record, participants), nil
}

// ListByInstance returns the groups a number belongs to. Rosters are NOT loaded:
// a list view shows names, and loading every roster to render them would be one
// query per group on a page that displays none of it.
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

// participantRows orders admins first, then by label, which is how a roster is
// read: an operator scanning a group wants to know who can act, not who joined
// in which order.
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

// MarkStale records that something about a group changed, without saying what.
//
// A no-op for a group we do not track: the provider notifies about every group
// the connected number is in, including ones nobody in the CRM has ever opened,
// and creating a row for each would sync rosters nobody asked for.
func (r *groupRepository) MarkStale(ctx context.Context, instanceID, jid string, at time.Time) error {
	if strings.TrimSpace(jid) == "" {
		return nil
	}
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroup{}).
		Where("instance_id = ? AND jid = ?", instanceID, jid).
		Update("stale_at", at).Error
}

// LinkParticipantContacts attaches contact rows to roster entries for members we
// already know from a direct chat.
//
// Matched on phone number, and only for members who have one: a LID-only member
// is anonymous by WhatsApp's design, and guessing at an identity for them would
// attach a stranger's transcript to the wrong person in the roster.
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

// Delete removes a group we have left. The roster goes with it: it describes a
// membership that no longer exists, and keeping it would leave the panel
// rendering a group the number is not in.
func (r *groupRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).
			Delete(&schema.UnofficialWhatsAppGroupParticipant{}).Error; err != nil {
			return err
		}
		return tx.Delete(&schema.UnofficialWhatsAppGroup{}, "id = ?", id).Error
	})
}
