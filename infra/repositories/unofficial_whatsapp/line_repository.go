package unofficial_whatsapp_repository

import (
	"context"
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	lineTransferBatch   = 500
	lineTransferMaxRuns = 10_000
)

type lineRepository struct {
	db    *gorm.DB
	batch int
}

func NewLineRepository(db *gorm.DB) uw.LineRepository {
	return &lineRepository{db: db, batch: lineTransferBatch}
}

func (r *lineRepository) ListSameNumber(ctx context.Context, instance *uw.Instance) ([]*uw.Instance, error) {
	if instance == nil || strings.TrimSpace(instance.PhoneNumber) == "" {
		return nil, nil
	}
	var rows []schema.UnofficialWhatsAppInstance
	err := r.db.WithContext(ctx).Unscoped().
		Where("workspace_id = ? AND phone_number = ? AND id <> ?",
			instance.WorkspaceID, instance.PhoneNumber, instance.ID).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*uw.Instance, 0, len(rows))
	for i := range rows {
		out = append(out, toInstanceDomain(&rows[i]))
	}
	return out, nil
}

type rowMove struct {
	table    string
	eligible string
}

var (
	contactMove = rowMove{
		table: "unofficial_whatsapp_contacts",
		eligible: `NOT EXISTS (
			SELECT 1 FROM unofficial_whatsapp_contacts b
			 WHERE b.instance_id = @to AND b.deleted_at IS NULL
			   AND (b.jid = a.jid OR (a.lid <> '' AND b.lid = a.lid)))`,
	}
	conversationMove = rowMove{
		table: "unofficial_whatsapp_conversations",
		eligible: `EXISTS (
			SELECT 1 FROM unofficial_whatsapp_contacts k
			 WHERE k.id = a.contact_id AND k.instance_id = @to)
		AND NOT EXISTS (
			SELECT 1 FROM unofficial_whatsapp_conversations b
			 WHERE b.instance_id = @to AND b.deleted_at IS NULL
			   AND b.campaign_id = a.campaign_id
			   AND (b.contact_id = a.contact_id OR (a.chat_id <> '' AND b.chat_id = a.chat_id)))`,
	}
	groupMove = rowMove{
		table: "unofficial_whatsapp_groups",
		eligible: `NOT EXISTS (
			SELECT 1 FROM unofficial_whatsapp_groups b
			 WHERE b.instance_id = @to AND b.deleted_at IS NULL AND b.jid = a.jid)`,
	}
)

func (r *lineRepository) Transfer(ctx context.Context, from, to string) (uw.LineTransfer, error) {
	var out uw.LineTransfer
	if from == "" || to == "" || from == to {
		return out, fmt.Errorf("unofficial whatsapp: invalid line transfer %q -> %q", from, to)
	}

	merged, err := r.repointToTwins(ctx, from, to)
	if err != nil {
		return out, err
	}
	out.ContactsMerged = merged

	if out.Contacts, err = r.moveInBatches(ctx, contactMove, from, to); err != nil {
		return out, err
	}
	if merged, err = r.repointToTwins(ctx, from, to); err != nil {
		return out, err
	}
	out.ContactsMerged += merged
	if out.Conversations, err = r.moveInBatches(ctx, conversationMove, from, to); err != nil {
		return out, err
	}
	if out.Groups, err = r.moveInBatches(ctx, groupMove, from, to); err != nil {
		return out, err
	}

	var kept int64
	if err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppConversation{}).
		Where("instance_id = ?", from).Count(&kept).Error; err != nil {
		return out, err
	}
	out.ConversationsKept = int(kept)
	return out, nil
}

const twinContactSQL = `
	FROM unofficial_whatsapp_contacts a
	JOIN LATERAL (
		SELECT b.id FROM unofficial_whatsapp_contacts b
		 WHERE b.instance_id = @to AND b.deleted_at IS NULL
		   AND (b.jid = a.jid OR (a.lid <> '' AND b.lid = a.lid))
		 ORDER BY (b.jid = a.jid) DESC
		 LIMIT 1
	) twin ON TRUE
	WHERE a.instance_id = @from AND a.deleted_at IS NULL`

func (r *lineRepository) repointToTwins(ctx context.Context, from, to string) (int, error) {
	args := map[string]any{"from": from, "to": to}

	conversations := r.db.WithContext(ctx).Exec(`
		UPDATE unofficial_whatsapp_conversations c
		   SET contact_id = twin.id, updated_at = NOW()`+twinContactSQL+`
		   AND c.contact_id = a.id AND c.deleted_at IS NULL`, args)
	if conversations.Error != nil {
		if database.IsUniqueViolation(conversations.Error) {
			log.Printf("[unofficial-whatsapp][line] %s -> %s: two old contacts resolve to one new contact in the same campaign; "+
				"those conversations stay on the old number: %v", from, to, conversations.Error)
			return 0, nil
		}
		return 0, conversations.Error
	}

	participants := r.db.WithContext(ctx).Exec(`
		UPDATE unofficial_whatsapp_group_participants c
		   SET contact_id = twin.id, updated_at = NOW()`+twinContactSQL+`
		   AND c.contact_id = a.id`, args)
	if participants.Error != nil {
		return 0, participants.Error
	}
	return int(conversations.RowsAffected), nil
}

func (r *lineRepository) moveInBatches(ctx context.Context, move rowMove, from, to string) (int, error) {
	moved := 0
	var excluded []string
	for run := 0; run < lineTransferMaxRuns; run++ {
		if err := ctx.Err(); err != nil {
			return moved, err
		}
		ids, err := r.nextBatch(ctx, move, from, to, excluded)
		if err != nil {
			return moved, err
		}
		if len(ids) == 0 {
			return moved, nil
		}

		n, err := r.moveRows(ctx, move, from, to, ids)
		if database.IsUniqueViolation(err) {
			n, excluded, err = r.moveOneByOne(ctx, move, from, to, ids, excluded)
		}
		if err != nil {
			return moved, err
		}
		moved += n
	}
	return moved, fmt.Errorf("unofficial whatsapp: %s transfer %s -> %s did not settle", move.table, from, to)
}

func (r *lineRepository) nextBatch(ctx context.Context, move rowMove, from, to string, excluded []string) ([]string, error) {
	query := `SELECT a.id FROM ` + move.table + ` a
		WHERE a.instance_id = @from AND a.deleted_at IS NULL AND ` + move.eligible
	args := map[string]any{"from": from, "to": to, "limit": r.batch}
	if len(excluded) > 0 {
		query += ` AND a.id NOT IN @excluded`
		args["excluded"] = excluded
	}
	query += ` ORDER BY a.id LIMIT @limit`

	var ids []string
	if err := r.db.WithContext(ctx).Raw(query, args).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *lineRepository) moveRows(ctx context.Context, move rowMove, from, to string, ids []string) (int, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE `+move.table+` a SET instance_id = @to, updated_at = NOW()
		 WHERE a.id IN @ids AND a.instance_id = @from AND a.deleted_at IS NULL AND `+move.eligible,
		map[string]any{"from": from, "to": to, "ids": ids})
	return int(result.RowsAffected), result.Error
}

func (r *lineRepository) moveOneByOne(
	ctx context.Context,
	move rowMove,
	from, to string,
	ids, excluded []string,
) (int, []string, error) {
	moved := 0
	for _, id := range ids {
		n, err := r.moveRows(ctx, move, from, to, []string{id})
		switch {
		case database.IsUniqueViolation(err):
			log.Printf("[unofficial-whatsapp][line] %s -> %s: %s row %s collides with one the new number already has; it stays",
				from, to, move.table, id)
			excluded = append(excluded, id)
		case err != nil:
			return moved, excluded, err
		default:
			moved += n
		}
	}
	return moved, excluded, nil
}
